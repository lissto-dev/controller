/*
Copyright 2025 Lissto.

Licensed under the Sustainable Use License, Version 1.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://github.com/lissto-dev/controller/blob/main/LICENSE.md

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	stackconfig "github.com/lissto-dev/controller/internal/stack/config"
	"github.com/lissto-dev/controller/internal/stack/image"
	"github.com/lissto-dev/controller/internal/stack/manifest"
	"github.com/lissto-dev/controller/internal/stack/resource"
	"github.com/lissto-dev/controller/internal/stack/restore"
	"github.com/lissto-dev/controller/internal/stack/status"
	"github.com/lissto-dev/controller/internal/stack/suspension"
	"github.com/lissto-dev/controller/pkg/config"
	"github.com/lissto-dev/controller/pkg/util"
)

const stackFinalizerName = "stack.lissto.dev/config-cleanup"

// StackReconciler reconciles a Stack object
type StackReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Config   *config.Config
	Recorder record.EventRecorder

	// Domain services
	manifestFetcher  *manifest.Fetcher
	imageInjector    *image.Injector
	resourceApplier  *resource.Applier
	resourceCleaner  *resource.Cleaner
	configDiscoverer *stackconfig.Discoverer
	configResolver   *stackconfig.Resolver
	configInjector   *stackconfig.Injector
	secretCopier     *stackconfig.SecretCopier
	statusUpdater    *status.Updater
	restoreHandler   *restore.Handler
}

// InitServices initializes the domain services (call after Client is set)
func (r *StackReconciler) InitServices() {
	r.manifestFetcher = manifest.NewFetcher(r.Client)
	r.imageInjector = image.NewInjector()
	r.resourceApplier = resource.NewApplier(r.Client, r.Scheme)
	r.resourceCleaner = resource.NewCleaner(r.Client)
	r.configDiscoverer = stackconfig.NewDiscoverer(r.Client, r.Config.Namespaces.Global)
	r.configResolver = stackconfig.NewResolver()
	r.configInjector = stackconfig.NewInjector()
	r.secretCopier = stackconfig.NewSecretCopier(r.Client, r.Scheme)
	r.statusUpdater = status.NewUpdater(r.Client, r.Recorder)
	r.restoreHandler = restore.NewHandler(r.Client)
}

// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks/finalizers,verbs=update
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lisstovariables,verbs=get;list;watch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=lisstosecrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=blueprints,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop
func (r *StackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	stack := &envv1alpha1.Stack{}
	if err := r.Get(ctx, req.NamespacedName, stack); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling Stack", "name", stack.Name, "namespace", stack.Namespace,
		"suspension", stack.Spec.Suspension, "phase", stack.Status.Phase)

	// Handle finalizer
	if shouldReturn, err := r.handleFinalizer(ctx, stack); shouldReturn {
		return ctrl.Result{}, err
	}

	// Fetch and parse manifests
	objects, err := r.fetchAndParseManifests(ctx, stack)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Validate resources
	if err := suspension.ValidateResourceAnnotations(objects); err != nil {
		log.Error(err, "Resource validation failed")
		r.Recorder.Eventf(stack, corev1.EventTypeWarning, "ValidationFailed",
			"Resource validation failed: %v", err)
		r.statusUpdater.UpdateWithError(ctx, stack, err)
		return ctrl.Result{}, err
	}

	// Inject config
	configResult := r.injectConfig(ctx, stack, objects)

	// Inject images
	imageWarnings := r.imageInjector.Inject(ctx, objects, stack.Spec.Images)

	// Categorize and apply resources
	stateResources, workloadsToApply, workloadsToDelete := suspension.CategorizeResources(stack, objects)

	log.Info("Resource categorization",
		"stateResources", len(stateResources),
		"workloadsToApply", len(workloadsToApply),
		"workloadsToDelete", len(workloadsToDelete))

	// Apply state resources
	stateResults := r.resourceApplier.Apply(ctx, stateResources, stack.Namespace)
	r.resourceApplier.SetOwnerReferences(ctx, stack, stateResources, stateResults)

	// Handle restore if needed
	if result, done := r.handleRestore(ctx, stack, stateResources); done {
		return result, nil
	}

	// Delete suspended workloads
	for _, obj := range workloadsToDelete {
		if err := r.resourceApplier.Delete(ctx, obj, stack.Namespace); err != nil {
			log.Error(err, "Failed to delete workload resource",
				"kind", obj.GetKind(), "name", obj.GetName())
		}
	}

	// Apply running workloads
	workloadResults := r.resourceApplier.Apply(ctx, workloadsToApply, stack.Namespace)
	r.resourceApplier.SetOwnerReferences(ctx, stack, workloadsToApply, workloadResults)

	// Check workload termination
	allWorkloadsTerminated := true
	if len(workloadsToDelete) > 0 {
		terminated, err := r.waitForWorkloadsTermination(ctx, stack)
		if err != nil {
			log.Error(err, "Error checking workload termination")
		}
		allWorkloadsTerminated = terminated
	}

	// Update phase
	r.updatePhase(ctx, stack, stateResources, workloadsToApply, workloadsToDelete, allWorkloadsTerminated)

	// Update service statuses
	r.statusUpdater.UpdateServiceStatuses(ctx, stack, objects)

	// Update status
	results := append(stateResults, workloadResults...)
	r.statusUpdater.Update(ctx, stack, status.UpdateInput{
		Results:       results,
		ImageWarnings: imageWarnings,
		ConfigResult:  configResult,
	})

	// Requeue if suspending
	if stack.Status.Phase == envv1alpha1.StackPhaseSuspending {
		log.Info("Stack suspending, requeuing to check termination")
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	log.Info("Stack reconciliation complete", "name", stack.Name, "phase", stack.Status.Phase)
	return ctrl.Result{}, nil
}

// handleFinalizer manages the stack finalizer.
// Returns (shouldReturn, err) where shouldReturn indicates if the caller should return early.
func (r *StackReconciler) handleFinalizer(ctx context.Context, stack *envv1alpha1.Stack) (bool, error) {
	log := logf.FromContext(ctx)

	if stack.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(stack, stackFinalizerName) {
			controllerutil.AddFinalizer(stack, stackFinalizerName)
			if err := r.Update(ctx, stack); err != nil {
				log.Error(err, "Failed to add finalizer")
				return true, err
			}
			log.Info("Added finalizer to Stack")
		}
		return false, nil
	}

	// Stack is being deleted
	if controllerutil.ContainsFinalizer(stack, stackFinalizerName) {
		if err := r.resourceCleaner.CleanupStackResources(ctx, stack.Name, stack.Namespace, stack.UID); err != nil {
			log.Error(err, "Failed to cleanup stack resources")
			return true, err
		}

		controllerutil.RemoveFinalizer(stack, stackFinalizerName)
		if err := r.Update(ctx, stack); err != nil {
			log.Error(err, "Failed to remove finalizer")
			return true, err
		}
		log.Info("Removed finalizer from Stack")
	}

	return true, nil
}

// fetchAndParseManifests fetches and parses manifests from ConfigMap
func (r *StackReconciler) fetchAndParseManifests(ctx context.Context, stack *envv1alpha1.Stack) ([]*unstructured.Unstructured, error) {
	log := logf.FromContext(ctx)

	configMapName := stack.Spec.ManifestsConfigMapRef
	if configMapName == "" {
		log.Error(nil, "Stack has no manifests ConfigMap reference")
		return nil, fmt.Errorf("stack has no manifests ConfigMap reference")
	}

	objects, err := r.manifestFetcher.FetchAndParse(ctx, stack.Namespace, configMapName)
	if err != nil {
		log.Error(err, "Failed to fetch/parse manifests from ConfigMap")
		r.statusUpdater.UpdateWithError(ctx, stack, fmt.Errorf("failed to fetch manifests: %w", err))
		return nil, err
	}

	log.Info("Parsed manifests", "count", len(objects))
	return objects, nil
}

// injectConfig discovers and injects config into workloads
func (r *StackReconciler) injectConfig(ctx context.Context, stack *envv1alpha1.Stack, objects []*unstructured.Unstructured) *status.ConfigResult {
	log := logf.FromContext(ctx)

	blueprint, err := r.getBlueprint(ctx, stack)
	if err != nil {
		log.Error(err, "Failed to fetch blueprint for config discovery")
		return nil
	}

	repository := ""
	if blueprint != nil && blueprint.Annotations != nil {
		repository = blueprint.Annotations["lissto.dev/repository"]
	}

	variables, _ := r.configDiscoverer.DiscoverVariables(ctx, stack, repository)
	secrets, _ := r.configDiscoverer.DiscoverSecrets(ctx, stack, repository)

	mergedVars := r.configResolver.ResolveVariables(variables)
	resolvedKeys := r.configResolver.ResolveSecretKeys(secrets)

	var stackSecretName string
	var missingSecretKeys map[string][]string

	if len(resolvedKeys) > 0 {
		stackSecret, missing, err := r.secretCopier.CopyToStackNamespace(ctx, stack, resolvedKeys)
		missingSecretKeys = missing
		if err != nil {
			log.Error(err, "Failed to copy secrets to stack namespace")
		} else if stackSecret != nil {
			stackSecretName = stackSecret.Name
		}

		for secretRef, keys := range missingSecretKeys {
			r.Recorder.Eventf(stack, corev1.EventTypeWarning, "MissingSecretKeys",
				"Secret %s is missing keys: %v", secretRef, keys)
		}
	}

	metadata := stackconfig.ExtractStackMetadata(stack)
	injectionResult := r.configInjector.Inject(ctx, objects, mergedVars, resolvedKeys, stackSecretName, metadata)

	if injectionResult != nil {
		log.Info("Config injection complete",
			"variables", injectionResult.VariablesInjected,
			"secrets", injectionResult.SecretsInjected,
			"metadata", injectionResult.MetadataInjected,
			"missingKeys", len(missingSecretKeys))

		for _, warning := range injectionResult.Warnings {
			r.Recorder.Eventf(stack, corev1.EventTypeWarning, "ConfigInjectionWarning", warning)
		}

		return &status.ConfigResult{
			VariablesInjected: injectionResult.VariablesInjected,
			SecretsInjected:   injectionResult.SecretsInjected,
			MetadataInjected:  injectionResult.MetadataInjected,
			MissingSecretKeys: missingSecretKeys,
			Warnings:          injectionResult.Warnings,
		}
	}

	return nil
}

// handleRestore handles restoration from StackSnapshot
func (r *StackReconciler) handleRestore(ctx context.Context, stack *envv1alpha1.Stack, stateResources []*unstructured.Unstructured) (ctrl.Result, bool) {
	log := logf.FromContext(ctx)

	var pvcObjects []*unstructured.Unstructured
	for _, obj := range stateResources {
		if obj.GetKind() == "PersistentVolumeClaim" {
			pvcObjects = append(pvcObjects, obj)
		}
	}

	if r.restoreHandler.NeedsRestore(stack) && len(pvcObjects) > 0 {
		restoreComplete, restoreResults, err := r.restoreHandler.Restore(ctx, stack, pvcObjects)
		if err != nil {
			log.Error(err, "Failed to restore from snapshot")
			r.Recorder.Eventf(stack, corev1.EventTypeWarning, "RestoreFailed",
				"Failed to restore from snapshot: %v", err)
		}

		for _, result := range restoreResults {
			if result.Error != nil {
				log.Error(result.Error, "Resource restore failed",
					"service", result.Service, "snapshot", result.SnapshotName)
			}
		}

		if !restoreComplete {
			log.Info("Restore in progress, requeuing", "results", len(restoreResults))
			return ctrl.Result{RequeueAfter: 5 * time.Second}, true
		}

		log.Info("Restore complete", "results", len(restoreResults))
	}

	return ctrl.Result{}, false
}

// updatePhase determines and updates the stack phase
func (r *StackReconciler) updatePhase(ctx context.Context, stack *envv1alpha1.Stack,
	stateResources, workloadsToApply, workloadsToDelete []*unstructured.Unstructured,
	allWorkloadsTerminated bool) {

	newPhase := suspension.DetermineStackPhase(stack, allWorkloadsTerminated)
	oldPhase := stack.Status.Phase

	if oldPhase == newPhase {
		return
	}

	var reason, message string
	switch newPhase {
	case envv1alpha1.StackPhaseSuspending:
		reason = "Suspending"
		message = fmt.Sprintf("Suspending stack, deleting %d workload resources", len(workloadsToDelete))
	case envv1alpha1.StackPhaseSuspended:
		reason = "Suspended"
		message = fmt.Sprintf("Stack suspended, %d state resources preserved", len(stateResources))
	case envv1alpha1.StackPhaseResuming:
		reason = "Resuming"
		message = fmt.Sprintf("Resuming stack, recreating %d workload resources", len(workloadsToApply))
	case envv1alpha1.StackPhaseRunning:
		if oldPhase == "" {
			reason = "StackCreated"
			message = "Stack created and all resources applied"
		} else {
			reason = "Running"
			message = fmt.Sprintf("Stack running, %d resources applied", len(stateResources)+len(workloadsToApply))
		}
	}

	r.statusUpdater.TransitionPhase(ctx, stack, newPhase, reason, message)
}

// waitForWorkloadsTermination checks if all suspended workloads have terminated
func (r *StackReconciler) waitForWorkloadsTermination(ctx context.Context, stack *envv1alpha1.Stack) (bool, error) {
	var podList corev1.PodList
	if err := r.List(ctx, &podList, client.InNamespace(stack.Namespace)); err != nil {
		return false, err
	}

	for _, pod := range podList.Items {
		svc := pod.Labels["io.kompose.service"]
		if svc != "" && suspension.ShouldSuspendService(stack, svc) {
			if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodPending {
				return false, nil
			}
		}
	}
	return true, nil
}

// getBlueprint fetches the blueprint referenced by the stack
func (r *StackReconciler) getBlueprint(ctx context.Context, stack *envv1alpha1.Stack) (*envv1alpha1.Blueprint, error) {
	namespace, name := util.ParseBlueprintReference(
		stack.Spec.BlueprintReference,
		stack.Namespace,
		r.Config.Namespaces.Global,
	)

	blueprint := &envv1alpha1.Blueprint{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, blueprint); err != nil {
		return nil, err
	}

	return blueprint, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StackReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.InitServices()
	return ctrl.NewControllerManagedBy(mgr).
		For(&envv1alpha1.Stack{}).
		WithEventFilter(predicate.GenerationChangedPredicate{}).
		Named("stack").
		Complete(r)
}
