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

package restore

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// Handler handles restoration from StackSnapshots
type Handler struct {
	client client.Client
}

// NewHandler creates a new restore handler
func NewHandler(c client.Client) *Handler {
	return &Handler{client: c}
}

// NeedsRestore checks if the stack has a restore spec
func (h *Handler) NeedsRestore(stack *envv1alpha1.Stack) bool {
	return stack.Spec.Restore != nil && stack.Spec.Restore.SnapshotRef != ""
}

// IsComplete checks if restore has already completed
func (h *Handler) IsComplete(stack *envv1alpha1.Stack) bool {
	return stack.Status.RestoreStatus != nil &&
		stack.Status.RestoreStatus.Phase == envv1alpha1.RestorePhaseCompleted
}

// Restore handles restoration from a StackSnapshot
// Returns true if restore is complete, false if still in progress
// TODO: Implement in Phase 5 - Stack Restore
func (h *Handler) Restore(ctx context.Context, stack *envv1alpha1.Stack, pvcObjects []*unstructured.Unstructured) (bool, []Result, error) {
	log := logf.FromContext(ctx)

	if !h.NeedsRestore(stack) || h.IsComplete(stack) {
		return true, nil, nil
	}

	log.Info("Restore from StackSnapshot not yet implemented",
		"snapshotRef", stack.Spec.Restore.SnapshotRef)

	// TODO: Phase 5 implementation:
	// 1. Fetch StackSnapshot by spec.restore.snapshotRef
	// 2. Filter resources by include/exclude
	// 3. For each resource:
	//    - Download from S3 → Decrypt → Decompress
	//    - ConfigMaps/Secrets: Apply directly with owner reference
	//    - PVCs: Create PVC, run restore Job to populate data
	// 4. Update status.restoreStatus

	return true, nil, nil
}

// FindPVCForRestore finds the PVC name that matches the service and mount path
func (h *Handler) FindPVCForRestore(pvcObjects []*unstructured.Unstructured, serviceName, mountPath string) string {
	for _, obj := range pvcObjects {
		if obj.GetKind() != "PersistentVolumeClaim" {
			continue
		}

		labels := obj.GetLabels()
		annotations := obj.GetAnnotations()

		pvcService := labels["io.kompose.service"]
		if pvcService == "" {
			pvcService = labels["app.kubernetes.io/component"]
		}
		if pvcService == "" {
			pvcService = labels["app"]
		}

		if pvcService != serviceName {
			continue
		}

		pvcMountPath := annotations["env.lissto.dev/mount-path"]
		if mountPath != "" && pvcMountPath != "" && pvcMountPath != mountPath {
			continue
		}

		return obj.GetName()
	}

	return ""
}
