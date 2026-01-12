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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// Cleaner handles cleanup of stack resources
type Cleaner struct {
	client client.Client
}

// NewCleaner creates a new resource cleaner
func NewCleaner(c client.Client) *Cleaner {
	return &Cleaner{client: c}
}

// resourceType defines a resource type for cleanup
type resourceType struct {
	name string
	list client.ObjectList
}

// CleanupStackResources performs comprehensive cleanup of all Stack resources
// This acts as a safety net in case owner references didn't work properly
func (c *Cleaner) CleanupStackResources(ctx context.Context, stackName, namespace string, stackUID types.UID) error {
	log := logf.FromContext(ctx)

	resourceTypes := []resourceType{
		{"Pods", &corev1.PodList{}},
		{"Deployments", &appsv1.DeploymentList{}},
		{"Ingresses", &networkingv1.IngressList{}},
		{"Services", &corev1.ServiceList{}},
		{"PersistentVolumeClaims", &corev1.PersistentVolumeClaimList{}},
		{"Secrets", &corev1.SecretList{}},
	}

	errorCount := 0
	deletedCount := 0

	for _, rt := range resourceTypes {
		listOpts := []client.ListOption{client.InNamespace(namespace)}

		if err := c.client.List(ctx, rt.list, listOpts...); err != nil {
			log.Error(err, "Failed to list resources during cleanup", "type", rt.name)
			errorCount++
			continue
		}

		items, err := meta.ExtractList(rt.list)
		if err != nil {
			log.Error(err, "Failed to extract list items", "type", rt.name)
			errorCount++
			continue
		}

		for _, item := range items {
			obj, ok := item.(client.Object)
			if !ok {
				continue
			}

			if !c.isOwnedByStack(obj, stackName, stackUID) {
				continue
			}

			if err := c.client.Delete(ctx, obj); err != nil {
				if !apierrors.IsNotFound(err) {
					log.Error(err, "Failed to delete resource during cleanup",
						"type", rt.name, "name", obj.GetName())
					errorCount++
				}
			} else {
				log.Info("Deleted resource during cleanup", "type", rt.name, "name", obj.GetName())
				deletedCount++
			}
		}
	}

	if deletedCount > 0 {
		log.Info("Cleaned up Stack resources via finalizer",
			"deleted", deletedCount, "errors", errorCount,
			"note", "Most resources should be auto-deleted by owner references")
	}

	if errorCount > 0 {
		log.Error(nil, "Some resources failed to delete, but allowing Stack deletion to proceed",
			"errorCount", errorCount)
	}

	return nil
}

// isOwnedByStack checks if a resource is owned by the stack
func (c *Cleaner) isOwnedByStack(obj client.Object, stackName string, stackUID types.UID) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == stackUID {
			return true
		}
	}

	labels := obj.GetLabels()
	if labels != nil && labels["lissto.dev/stack"] == stackName {
		return true
	}

	return false
}

// CleanupConfigSecrets cleans up orphaned config secrets
func (c *Cleaner) CleanupConfigSecrets(ctx context.Context, stackName, namespace string) error {
	log := logf.FromContext(ctx)

	secretList := &corev1.SecretList{}
	if err := c.client.List(ctx, secretList,
		client.InNamespace(namespace),
		client.MatchingLabels{
			"lissto.dev/managed-by": "stack-controller",
			"lissto.dev/stack":      stackName,
		}); err != nil {
		return err
	}

	for i := range secretList.Items {
		secret := &secretList.Items[i]
		if err := c.client.Delete(ctx, secret); err != nil && !apierrors.IsNotFound(err) {
			log.Error(err, "Failed to delete config secret", "secret", secret.Name)
		} else {
			log.Info("Deleted config secret", "secret", secret.Name)
		}
	}

	log.Info("Cleaned up config secrets", "count", len(secretList.Items))
	return nil
}
