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

package config

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// SecretCopier handles copying secrets to stack namespace
type SecretCopier struct {
	client client.Client
	scheme *runtime.Scheme
}

// NewSecretCopier creates a new secret copier
func NewSecretCopier(c client.Client, s *runtime.Scheme) *SecretCopier {
	return &SecretCopier{client: c, scheme: s}
}

// secretSource groups keys by their source secret
type secretSource struct {
	namespace string
	secretRef string
	keys      []string
}

// CopyToStackNamespace copies secret values from source secrets to a merged secret in stack namespace
// Returns: created/updated secret, map of missing keys per secretRef, error
func (sc *SecretCopier) CopyToStackNamespace(ctx context.Context, stack *envv1alpha1.Stack, resolvedKeys map[string]SecretKeySource) (*corev1.Secret, map[string][]string, error) {
	log := logf.FromContext(ctx)

	if len(resolvedKeys) == 0 {
		return nil, nil, nil
	}

	// Group keys by source secret to minimize API calls
	sourceMap := make(map[string]*secretSource)

	for keyName, source := range resolvedKeys {
		secretRef := source.LisstoSecret.GetSecretRef()
		mapKey := fmt.Sprintf("%s/%s", source.LisstoSecret.Namespace, secretRef)

		if _, exists := sourceMap[mapKey]; !exists {
			sourceMap[mapKey] = &secretSource{
				namespace: source.LisstoSecret.Namespace,
				secretRef: secretRef,
				keys:      []string{},
			}
		}
		sourceMap[mapKey].keys = append(sourceMap[mapKey].keys, keyName)
	}

	// Read each unique source secret once and extract all needed keys
	mergedData := make(map[string][]byte)
	missingKeys := make(map[string][]string)

	for _, source := range sourceMap {
		k8sSecret := &corev1.Secret{}
		if err := sc.client.Get(ctx, client.ObjectKey{
			Namespace: source.namespace,
			Name:      source.secretRef,
		}, k8sSecret); err != nil {
			log.Error(err, "Failed to read source secret",
				"namespace", source.namespace, "name", source.secretRef)
			missingKeys[source.secretRef] = source.keys
			continue
		}

		missing := []string{}
		for _, keyName := range source.keys {
			if value, exists := k8sSecret.Data[keyName]; exists {
				mergedData[keyName] = value
			} else {
				missing = append(missing, keyName)
			}
		}

		if len(missing) > 0 {
			missingKeys[source.secretRef] = missing
			log.Error(nil, "Secret keys declared but not found in K8s Secret",
				"namespace", source.namespace, "secret", source.secretRef, "missingKeys", missing)
		}
	}

	log.V(1).Info("Read source secrets",
		"uniqueSources", len(sourceMap),
		"totalKeys", len(resolvedKeys),
		"missingKeys", len(missingKeys))

	if len(mergedData) == 0 {
		return nil, missingKeys, nil
	}

	// Create or update the stack's merged secret
	stackSecretName := stack.Name + "-config-secrets"
	stackSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      stackSecretName,
			Namespace: stack.Namespace,
			Labels: map[string]string{
				"lissto.dev/managed-by": "stack-controller",
				"lissto.dev/stack":      stack.Name,
			},
		},
		Type: corev1.SecretTypeOpaque,
		Data: mergedData,
	}

	if err := controllerutil.SetControllerReference(stack, stackSecret, sc.scheme); err != nil {
		log.Error(err, "Failed to set owner reference, will rely on finalizer for cleanup",
			"secret", stackSecretName)
	}

	existing := &corev1.Secret{}
	err := sc.client.Get(ctx, client.ObjectKey{Namespace: stack.Namespace, Name: stackSecretName}, existing)
	if err != nil {
		if err := sc.client.Create(ctx, stackSecret); err != nil {
			return nil, missingKeys, fmt.Errorf("failed to create stack secret: %w", err)
		}
		log.Info("Created stack config secret", "name", stackSecretName, "keys", len(mergedData))
	} else {
		if !reflect.DeepEqual(existing.Data, mergedData) {
			existing.Data = mergedData
			if err := sc.client.Update(ctx, existing); err != nil {
				return nil, missingKeys, fmt.Errorf("failed to update stack secret: %w", err)
			}
			log.Info("Updated stack config secret", "name", stackSecretName, "keys", len(mergedData))
		} else {
			log.V(1).Info("Stack config secret unchanged, skipping update", "name", stackSecretName)
		}
		stackSecret = existing
	}

	return stackSecret, missingKeys, nil
}
