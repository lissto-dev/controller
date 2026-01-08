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

package resource

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

const FieldOwner = "stack-controller"

// Applier handles applying Kubernetes resources to the cluster
type Applier struct {
	client client.Client
	scheme *runtime.Scheme
}

// NewApplier creates a new resource applier
func NewApplier(c client.Client, s *runtime.Scheme) *Applier {
	return &Applier{client: c, scheme: s}
}

// Apply applies all resources to cluster using server-side apply
func (a *Applier) Apply(ctx context.Context, objects []*unstructured.Unstructured, namespace string) []Result {
	log := logf.FromContext(ctx)
	results := make([]Result, 0, len(objects))

	for _, obj := range objects {
		result := Result{
			Kind: obj.GetKind(),
			Name: obj.GetName(),
		}

		obj.SetNamespace(namespace)

		if obj.GetKind() == "PersistentVolumeClaim" {
			result = a.applyPVC(ctx, obj)
		} else {
			result = a.applyWithServerSideApply(ctx, obj)
		}

		if result.Error != nil {
			log.Error(result.Error, "Failed to apply resource",
				"kind", obj.GetKind(), "name", obj.GetName())
		} else {
			log.V(1).Info("Applied resource",
				"kind", obj.GetKind(), "name", obj.GetName())
		}

		results = append(results, result)
	}

	return results
}

// applyPVC handles PVC creation (PVCs are immutable after creation)
func (a *Applier) applyPVC(ctx context.Context, obj *unstructured.Unstructured) Result {
	log := logf.FromContext(ctx)
	result := Result{Kind: obj.GetKind(), Name: obj.GetName()}

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	err := a.client.Get(ctx, client.ObjectKeyFromObject(obj), existing)

	if err != nil {
		if err := a.client.Create(ctx, obj); err != nil {
			result.Error = err
			return result
		}
		log.Info("Created PVC", "name", obj.GetName())
		result.Applied = true
		return result
	}

	log.Info("PVC already exists, skipping update (immutable)", "name", obj.GetName())
	result.Applied = true
	return result
}

// applyWithServerSideApply uses server-side apply for non-PVC resources
func (a *Applier) applyWithServerSideApply(ctx context.Context, obj *unstructured.Unstructured) Result {
	result := Result{Kind: obj.GetKind(), Name: obj.GetName()}

	if err := a.client.Patch(ctx, obj, client.Apply, client.ForceOwnership, client.FieldOwner(FieldOwner)); err != nil {
		result.Error = err
		return result
	}

	result.Applied = true
	return result
}

// SetOwnerReferences sets Stack as owner of all successfully applied resources
func (a *Applier) SetOwnerReferences(ctx context.Context, stack *envv1alpha1.Stack,
	objects []*unstructured.Unstructured, results []Result) {
	log := logf.FromContext(ctx)

	for i, result := range results {
		if !result.Applied {
			continue
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(objects[i].GroupVersionKind())
		obj.SetName(objects[i].GetName())
		obj.SetNamespace(objects[i].GetNamespace())

		if err := a.client.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			log.Error(err, "Failed to fetch resource for owner reference",
				"kind", obj.GetKind(), "name", obj.GetName())
			continue
		}

		if a.hasOwnerReference(obj, stack.UID) {
			continue
		}

		if err := controllerutil.SetOwnerReference(stack, obj, a.scheme); err != nil {
			log.Error(err, "Failed to set owner reference",
				"kind", obj.GetKind(), "name", obj.GetName())
			continue
		}

		if err := a.client.Update(ctx, obj); err != nil {
			if apierrors.IsConflict(err) {
				log.V(1).Info("Conflict updating resource with owner reference, will retry",
					"kind", obj.GetKind(), "name", obj.GetName())
			} else {
				log.Error(err, "Failed to update resource with owner reference",
					"kind", obj.GetKind(), "name", obj.GetName())
			}
		} else {
			log.Info("Set owner reference", "kind", obj.GetKind(), "name", obj.GetName())
		}
	}
}

// hasOwnerReference checks if the object already has an owner reference to the given UID
func (a *Applier) hasOwnerReference(obj *unstructured.Unstructured, uid interface{}) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == uid {
			return true
		}
	}
	return false
}

// Delete removes a resource from the cluster
func (a *Applier) Delete(ctx context.Context, obj *unstructured.Unstructured, namespace string) error {
	log := logf.FromContext(ctx)
	obj.SetNamespace(namespace)

	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(obj.GroupVersionKind())
	if err := a.client.Get(ctx, client.ObjectKeyFromObject(obj), existing); err != nil {
		return client.IgnoreNotFound(err)
	}

	if err := a.client.Delete(ctx, existing); err != nil && !apierrors.IsNotFound(err) {
		return err
	}

	log.Info("Deleted workload", "kind", obj.GetKind(), "name", obj.GetName())
	return nil
}
