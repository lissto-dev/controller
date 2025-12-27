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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/controller/snapshot"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VolumeSnapshotReconciler reconciles a VolumeSnapshot object
type VolumeSnapshotReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=env.lissto.dev,resources=volumesnapshots,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=volumesnapshots/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=env.lissto.dev,resources=volumesnapshots/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile handles VolumeSnapshot object changes
func (r *VolumeSnapshotReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// Fetch the VolumeSnapshot instance
	var volumeSnapshot envv1alpha1.VolumeSnapshot
	if err := r.Get(ctx, req.NamespacedName, &volumeSnapshot); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Skip if already completed or failed
	if volumeSnapshot.Status.Phase == envv1alpha1.VolumeSnapshotPhaseCompleted ||
		volumeSnapshot.Status.Phase == envv1alpha1.VolumeSnapshotPhaseFailed {
		return ctrl.Result{}, nil
	}

	// Initialize status if pending
	if volumeSnapshot.Status.Phase == "" {
		volumeSnapshot.Status.Phase = envv1alpha1.VolumeSnapshotPhasePending
		now := metav1.Now()
		volumeSnapshot.Status.StartTime = &now
		if err := r.Status().Update(ctx, &volumeSnapshot); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if snapshot job exists
	jobName := snapshot.GenerateSnapshotJobName(volumeSnapshot.Name)
	var job batchv1.Job
	err := r.Get(ctx, client.ObjectKey{Name: jobName, Namespace: volumeSnapshot.Namespace}, &job)

	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if err != nil {
		// Job doesn't exist - this is expected, job is created by lifecycle controller
		// Just update status to InProgress if we have a job name set
		if volumeSnapshot.Status.JobName != "" && volumeSnapshot.Status.Phase == envv1alpha1.VolumeSnapshotPhasePending {
			volumeSnapshot.Status.Phase = envv1alpha1.VolumeSnapshotPhaseInProgress
			if err := r.Status().Update(ctx, &volumeSnapshot); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Job exists, check its status
	if volumeSnapshot.Status.Phase == envv1alpha1.VolumeSnapshotPhasePending {
		volumeSnapshot.Status.Phase = envv1alpha1.VolumeSnapshotPhaseInProgress
		volumeSnapshot.Status.JobName = jobName
	}

	// Check job completion
	if job.Status.Succeeded > 0 {
		log.Info("Snapshot job completed successfully", "volumeSnapshot", volumeSnapshot.Name)
		volumeSnapshot.Status.Phase = envv1alpha1.VolumeSnapshotPhaseCompleted
		now := metav1.Now()
		volumeSnapshot.Status.CompletionTime = &now
		volumeSnapshot.Status.Message = "Snapshot completed successfully"

		r.Recorder.Event(&volumeSnapshot, corev1.EventTypeNormal, "SnapshotCompleted",
			fmt.Sprintf("Volume snapshot %s completed successfully", volumeSnapshot.Name))
	} else if job.Status.Failed > 0 {
		log.Info("Snapshot job failed", "volumeSnapshot", volumeSnapshot.Name)
		volumeSnapshot.Status.Phase = envv1alpha1.VolumeSnapshotPhaseFailed
		now := metav1.Now()
		volumeSnapshot.Status.CompletionTime = &now
		volumeSnapshot.Status.Message = "Snapshot job failed"

		r.Recorder.Event(&volumeSnapshot, corev1.EventTypeWarning, "SnapshotFailed",
			fmt.Sprintf("Volume snapshot %s failed", volumeSnapshot.Name))
	}

	if err := r.Status().Update(ctx, &volumeSnapshot); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VolumeSnapshotReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&envv1alpha1.VolumeSnapshot{}).
		Owns(&batchv1.Job{}).
		Named("volumesnapshot").
		Complete(r)
}
