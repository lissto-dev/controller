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
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/pkg/config"
	"github.com/lissto-dev/controller/pkg/util"
)

const (
	blueprintFinalizerName = "blueprint.lissto.dev/stack-protection"
)

// BlueprintReconciler reconciles a Blueprint object
type BlueprintReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Config   *config.Config
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=env.lissto.dev,resources=blueprints,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=blueprints/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=blueprints/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=stacks,verbs=list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *BlueprintReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the Blueprint instance
	blueprint := &envv1alpha1.Blueprint{}
	if err := r.Get(ctx, req.NamespacedName, blueprint); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	log.Info("Reconciling Blueprint", "name", blueprint.Name, "namespace", blueprint.Namespace)

	// Handle finalizer for deletion protection
	if blueprint.DeletionTimestamp.IsZero() {
		// Blueprint is not being deleted, ensure finalizer is present
		if !controllerutil.ContainsFinalizer(blueprint, blueprintFinalizerName) {
			controllerutil.AddFinalizer(blueprint, blueprintFinalizerName)
			if err := r.Update(ctx, blueprint); err != nil {
				log.Error(err, "Failed to add finalizer")
				return ctrl.Result{}, err
			}
			log.Info("Added finalizer to Blueprint")
		}
	} else {
		// Blueprint is being deleted, check for referencing Stacks
		if controllerutil.ContainsFinalizer(blueprint, blueprintFinalizerName) {
			// Check if any Stacks reference this Blueprint
			referencingStacks, err := r.findReferencingStacks(ctx, blueprint)
			if err != nil {
				log.Error(err, "Failed to check for referencing Stacks")
				return ctrl.Result{}, err
			}

			if len(referencingStacks) > 0 {
				// Block deletion - Stacks still reference this Blueprint
				stackNames := make([]string, len(referencingStacks))
				for i, stack := range referencingStacks {
					stackNames[i] = fmt.Sprintf("%s/%s", stack.Namespace, stack.Name)
				}
				log.Info("Cannot delete Blueprint: referenced by Stacks",
					"blueprint", blueprint.Name,
					"stacks", stackNames)

				// Emit event with stack info
				if r.Recorder != nil {
					r.Recorder.Eventf(blueprint, corev1.EventTypeWarning, "DeletionBlocked",
						"Cannot delete Blueprint: referenced by %d Stack(s): %s",
						len(referencingStacks), strings.Join(stackNames, ", "))
				}

				// Deletion blocked - lifecycles will handle Stack cleanup
				return ctrl.Result{}, nil
			}

			// No referencing Stacks, safe to delete - remove finalizer
			controllerutil.RemoveFinalizer(blueprint, blueprintFinalizerName)
			if err := r.Update(ctx, blueprint); err != nil {
				log.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
			log.Info("Removed finalizer from Blueprint, deletion can proceed")
		}
		// Stop reconciliation as the Blueprint is being deleted
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// findReferencingStacks finds all Stacks that reference the given Blueprint.
// For Blueprints in the global namespace: searches ALL namespaces.
// For Blueprints in developer namespaces (by prefix): searches only the same namespace.
func (r *BlueprintReconciler) findReferencingStacks(ctx context.Context, blueprint *envv1alpha1.Blueprint) ([]envv1alpha1.Stack, error) {
	log := logf.FromContext(ctx)

	var referencingStacks []envv1alpha1.Stack

	if r.Config == nil {
		return nil, fmt.Errorf("config is not set")
	}

	isGlobalBlueprint := r.Config.Namespaces.IsGlobalNamespace(blueprint.Namespace)

	if isGlobalBlueprint {
		// Global Blueprint: search ALL namespaces
		log.V(1).Info("Searching all namespaces for Stacks referencing global Blueprint",
			"blueprint", blueprint.Name)

		stackList := &envv1alpha1.StackList{}
		if err := r.List(ctx, stackList); err != nil {
			return nil, fmt.Errorf("failed to list Stacks across all namespaces: %w", err)
		}

		for _, stack := range stackList.Items {
			if r.stackReferencesBlueprint(&stack, blueprint) {
				referencingStacks = append(referencingStacks, stack)
			}
		}
	} else {
		// Developer Blueprint: search only the same namespace
		log.V(1).Info("Searching same namespace for Stacks referencing developer Blueprint",
			"blueprint", blueprint.Name,
			"namespace", blueprint.Namespace)

		stackList := &envv1alpha1.StackList{}
		if err := r.List(ctx, stackList, client.InNamespace(blueprint.Namespace)); err != nil {
			return nil, fmt.Errorf("failed to list Stacks in namespace %s: %w", blueprint.Namespace, err)
		}

		for _, stack := range stackList.Items {
			if r.stackReferencesBlueprint(&stack, blueprint) {
				referencingStacks = append(referencingStacks, stack)
			}
		}
	}

	return referencingStacks, nil
}

// stackReferencesBlueprint checks if a Stack references the given Blueprint
func (r *BlueprintReconciler) stackReferencesBlueprint(stack *envv1alpha1.Stack, blueprint *envv1alpha1.Blueprint) bool {
	if r.Config == nil {
		return false
	}

	// Parse the Stack's blueprint reference
	refNamespace, refName := util.ParseBlueprintReference(
		stack.Spec.BlueprintReference,
		stack.Namespace,
		r.Config.Namespaces.Global,
	)

	// Check if the reference matches the Blueprint
	return refNamespace == blueprint.Namespace && refName == blueprint.Name
}

// SetupWithManager sets up the controller with the Manager.
func (r *BlueprintReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&envv1alpha1.Blueprint{}).
		Named("blueprint").
		Complete(r)
}
