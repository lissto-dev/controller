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
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/pkg/util"
)

// ConfigInjectionResult tracks the result of config injection
type ConfigInjectionResult struct {
	VariablesInjected int
	SecretsInjected   int
	MissingSecretKeys map[string][]string // secretRef -> missing keys
	Warnings          []string
}

// scopePriority returns the priority of a scope (lower = higher priority)
func scopePriority(scope string) int {
	switch scope {
	case "env":
		return 1
	case "repo":
		return 2
	case "global":
		return 3
	default:
		return 99
	}
}

// discoverVariables finds all LisstoVariable resources that match the stack
// Uses 2 cached List calls (user ns + global ns) with in-memory filtering
func (r *StackReconciler) discoverVariables(ctx context.Context, stack *envv1alpha1.Stack, blueprint *envv1alpha1.Blueprint) ([]envv1alpha1.LisstoVariable, error) {
	log := logf.FromContext(ctx)

	globalNS := r.Config.Namespaces.Global
	userNS := stack.Namespace
	repository := ""
	if blueprint != nil && blueprint.Annotations != nil {
		repository = blueprint.Annotations["lissto.dev/repository"]
	}

	// Single List call for user namespace (uses controller cache)
	userVars := &envv1alpha1.LisstoVariableList{}
	if err := r.List(ctx, userVars, client.InNamespace(userNS)); err != nil {
		log.Error(err, "Failed to list variables in user namespace")
		return nil, err
	}

	// Single List call for global namespace (uses controller cache)
	var globalVars *envv1alpha1.LisstoVariableList
	if globalNS != userNS {
		globalVars = &envv1alpha1.LisstoVariableList{}
		if err := r.List(ctx, globalVars, client.InNamespace(globalNS)); err != nil {
			log.Error(err, "Failed to list variables in global namespace")
			// Continue without global variables
			globalVars = nil
		}
	}

	// Filter in-memory
	var matched []envv1alpha1.LisstoVariable

	// Filter user namespace variables
	for _, v := range userVars.Items {
		if matchesStack(&v, stack, repository) {
			matched = append(matched, v)
		}
	}

	// Filter global namespace variables
	if globalVars != nil {
		for _, v := range globalVars.Items {
			if matchesStack(&v, stack, repository) {
				matched = append(matched, v)
			}
		}
	}

	log.V(1).Info("Discovered variables",
		"total", len(matched),
		"stack", stack.Name,
		"env", stack.Spec.Env,
		"userNsCount", len(userVars.Items),
		"globalNsCount", lenOrZero(globalVars))

	return matched, nil
}

// discoverSecrets finds all LisstoSecret resources that match the stack
// Uses 2 cached List calls (user ns + global ns) with in-memory filtering
func (r *StackReconciler) discoverSecrets(ctx context.Context, stack *envv1alpha1.Stack, blueprint *envv1alpha1.Blueprint) ([]envv1alpha1.LisstoSecret, error) {
	log := logf.FromContext(ctx)

	globalNS := r.Config.Namespaces.Global
	userNS := stack.Namespace
	repository := ""
	if blueprint != nil && blueprint.Annotations != nil {
		repository = blueprint.Annotations["lissto.dev/repository"]
	}

	// Single List call for user namespace (uses controller cache)
	userSecrets := &envv1alpha1.LisstoSecretList{}
	if err := r.List(ctx, userSecrets, client.InNamespace(userNS)); err != nil {
		log.Error(err, "Failed to list secrets in user namespace")
		return nil, err
	}

	// Single List call for global namespace (uses controller cache)
	var globalSecrets *envv1alpha1.LisstoSecretList
	if globalNS != userNS {
		globalSecrets = &envv1alpha1.LisstoSecretList{}
		if err := r.List(ctx, globalSecrets, client.InNamespace(globalNS)); err != nil {
			log.Error(err, "Failed to list secrets in global namespace")
			// Continue without global secrets
			globalSecrets = nil
		}
	}

	// Filter in-memory
	var matched []envv1alpha1.LisstoSecret

	// Filter user namespace secrets
	for _, s := range userSecrets.Items {
		if matchesStackSecret(&s, stack, repository) {
			matched = append(matched, s)
		}
	}

	// Filter global namespace secrets
	if globalSecrets != nil {
		for _, s := range globalSecrets.Items {
			if matchesStackSecret(&s, stack, repository) {
				matched = append(matched, s)
			}
		}
	}

	log.V(1).Info("Discovered secrets",
		"total", len(matched),
		"stack", stack.Name,
		"env", stack.Spec.Env,
		"userNsCount", len(userSecrets.Items),
		"globalNsCount", lenOrZeroSecrets(globalSecrets))

	return matched, nil
}

// matchesStack checks if a LisstoVariable matches the stack's scope criteria
func matchesStack(v *envv1alpha1.LisstoVariable, stack *envv1alpha1.Stack, repository string) bool {
	scope := v.GetScope()

	switch scope {
	case "global":
		// Global scope matches all stacks
		return true
	case "repo":
		// Repo scope matches if both repository and spec.Repository are non-empty and equal
		return repository != "" && v.Spec.Repository != "" && v.Spec.Repository == repository
	case "env":
		// Env scope matches if both env fields are non-empty and equal
		return stack.Spec.Env != "" && v.Spec.Env != "" && v.Spec.Env == stack.Spec.Env
	default:
		return false
	}
}

// matchesStackSecret checks if a LisstoSecret matches the stack's scope criteria
func matchesStackSecret(s *envv1alpha1.LisstoSecret, stack *envv1alpha1.Stack, repository string) bool {
	scope := s.GetScope()

	switch scope {
	case "global":
		return true
	case "repo":
		// Repo scope matches if both repository and spec.Repository are non-empty and equal
		return repository != "" && s.Spec.Repository != "" && s.Spec.Repository == repository
	case "env":
		// Env scope matches if both env fields are non-empty and equal
		return stack.Spec.Env != "" && s.Spec.Env != "" && s.Spec.Env == stack.Spec.Env
	default:
		return false
	}
}

// Helper to get length of list or 0 if nil
func lenOrZero(list *envv1alpha1.LisstoVariableList) int {
	if list == nil {
		return 0
	}
	return len(list.Items)
}

func lenOrZeroSecrets(list *envv1alpha1.LisstoSecretList) int {
	if list == nil {
		return 0
	}
	return len(list.Items)
}

// resolveVariables merges variables with hierarchy (env > repo > global)
func (r *StackReconciler) resolveVariables(variables []envv1alpha1.LisstoVariable) map[string]string {
	// Sort by priority (higher priority = lower number)
	sort.Slice(variables, func(i, j int) bool {
		return scopePriority(variables[i].GetScope()) > scopePriority(variables[j].GetScope())
	})

	// Merge data (lower priority first, so higher priority overwrites)
	merged := make(map[string]string)
	for _, v := range variables {
		for key, value := range v.Spec.Data {
			merged[key] = value
		}
	}

	return merged
}

// secretKeySource tracks which LisstoSecret provides each key
type secretKeySource struct {
	LisstoSecret *envv1alpha1.LisstoSecret
	Key          string
	Priority     int
}

// resolveSecretKeys resolves which secret provides each key (key-level resolution)
func (r *StackReconciler) resolveSecretKeys(secrets []envv1alpha1.LisstoSecret) map[string]secretKeySource {
	resolved := make(map[string]secretKeySource)

	for i := range secrets {
		secret := &secrets[i]
		priority := scopePriority(secret.GetScope())

		for _, key := range secret.Spec.Keys {
			existing, found := resolved[key]
			if !found || priority < existing.Priority {
				// Higher priority (lower number) wins
				resolved[key] = secretKeySource{
					LisstoSecret: secret,
					Key:          key,
					Priority:     priority,
				}
			}
		}
	}

	return resolved
}

// copySecretsToStackNamespace copies secret values from source secrets to a merged secret in stack namespace
// Optimized: batches K8s Secret reads by grouping keys by source secret
// Returns: created/updated secret, map of missing keys per secretRef, error
func (r *StackReconciler) copySecretsToStackNamespace(ctx context.Context, stack *envv1alpha1.Stack, resolvedKeys map[string]secretKeySource) (*corev1.Secret, map[string][]string, error) {
	log := logf.FromContext(ctx)

	if len(resolvedKeys) == 0 {
		return nil, nil, nil
	}

	// Group keys by source secret to minimize API calls
	type secretSource struct {
		namespace string
		secretRef string
		keys      []string
	}
	sourceMap := make(map[string]*secretSource) // key: "namespace/secretRef"

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
	missingKeys := make(map[string][]string) // secretRef -> missing keys

	for _, source := range sourceMap {
		k8sSecret := &corev1.Secret{}
		if err := r.Get(ctx, client.ObjectKey{
			Namespace: source.namespace,
			Name:      source.secretRef,
		}, k8sSecret); err != nil {
			log.Error(err, "Failed to read source secret",
				"namespace", source.namespace,
				"name", source.secretRef)
			// Track all keys as missing for this secret
			missingKeys[source.secretRef] = source.keys
			continue
		}

		// Copy all needed keys from this secret
		missing := []string{}
		for _, keyName := range source.keys {
			if value, exists := k8sSecret.Data[keyName]; exists {
				mergedData[keyName] = value
			} else {
				missing = append(missing, keyName)
			}
		}

		// Track missing keys
		if len(missing) > 0 {
			missingKeys[source.secretRef] = missing
			log.Error(nil, "Secret keys declared but not found in K8s Secret",
				"namespace", source.namespace,
				"secret", source.secretRef,
				"missingKeys", missing)
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

	// Set owner reference for automatic cleanup (best effort)
	// If this fails, the Stack finalizer will handle cleanup
	if err := controllerutil.SetControllerReference(stack, stackSecret, r.Scheme); err != nil {
		log.Error(err, "Failed to set owner reference, will rely on finalizer for cleanup",
			"secret", stackSecretName)
	}

	// Create or update
	existing := &corev1.Secret{}
	err := r.Get(ctx, client.ObjectKey{Namespace: stack.Namespace, Name: stackSecretName}, existing)
	if err != nil {
		// Create new
		if err := r.Create(ctx, stackSecret); err != nil {
			return nil, missingKeys, fmt.Errorf("failed to create stack secret: %w", err)
		}
		log.Info("Created stack config secret", "name", stackSecretName, "keys", len(mergedData))
	} else {
		// Update existing
		existing.Data = mergedData
		if err := r.Update(ctx, existing); err != nil {
			return nil, missingKeys, fmt.Errorf("failed to update stack secret: %w", err)
		}
		log.Info("Updated stack config secret", "name", stackSecretName, "keys", len(mergedData))
		stackSecret = existing
	}

	return stackSecret, missingKeys, nil
}

// injectConfigIntoDeployments injects environment variables and secret refs into deployments
func (r *StackReconciler) injectConfigIntoWorkloads(ctx context.Context, objects []*unstructured.Unstructured,
	mergedVars map[string]string, resolvedKeys map[string]secretKeySource, stackSecretName string) *ConfigInjectionResult {
	log := logf.FromContext(ctx)
	result := &ConfigInjectionResult{}

	if len(mergedVars) == 0 && len(resolvedKeys) == 0 {
		return result
	}

	for _, obj := range objects {
		// Support both Deployment and Pod
		if obj.GetKind() != "Deployment" && obj.GetKind() != "Pod" {
			continue
		}

		resourceKind := obj.GetKind()
		resourceName := obj.GetName()

		// Get containers path based on resource type
		var containersPath []string
		if resourceKind == "Deployment" {
			containersPath = []string{"spec", "template", "spec", "containers"}
		} else { // Pod
			containersPath = []string{"spec", "containers"}
		}

		// Get containers
		containers, found, err := unstructured.NestedSlice(obj.Object, containersPath...)
		if err != nil || !found {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to get containers from %s %s", resourceKind, resourceName))
			continue
		}

		// Update each container
		for i, container := range containers {
			containerMap, ok := container.(map[string]interface{})
			if !ok {
				continue
			}

			// Get existing env vars
			existingEnv, _, _ := unstructured.NestedSlice(containerMap, "env")
			if existingEnv == nil {
				existingEnv = []interface{}{}
			}

			// Build hierarchy: secrets > variables > compose
			// Secrets have highest priority, so track which keys are secrets
			secretKeys := make(map[string]bool)
			for key := range resolvedKeys {
				secretKeys[key] = true
			}

			// Build set of all keys that will be injected
			keysToInject := make(map[string]bool)
			for key := range mergedVars {
				keysToInject[key] = true
			}
			for key := range resolvedKeys {
				keysToInject[key] = true
			}

			// Filter out existing env vars that conflict with injected ones
			// This allows LisstoVariables/Secrets to override compose values
			filteredEnv := []interface{}{}
			for _, envVar := range existingEnv {
				envMap, ok := envVar.(map[string]interface{})
				if !ok {
					continue
				}
				name, ok := envMap["name"].(string)
				if !ok {
					continue
				}
				// Keep only env vars that won't conflict
				if !keysToInject[name] {
					filteredEnv = append(filteredEnv, envVar)
				}
			}

			// Add merged variables (these override compose values, but NOT secrets)
			for key, value := range mergedVars {
				// Skip if this key is also a secret (secrets have higher priority)
				if secretKeys[key] {
					continue
				}
				filteredEnv = append(filteredEnv, map[string]interface{}{
					"name":  key,
					"value": value,
				})
				result.VariablesInjected++
			}

			// Add secret key refs (these override both compose and variables)
			if stackSecretName != "" {
				for key := range resolvedKeys {
					filteredEnv = append(filteredEnv, map[string]interface{}{
						"name": key,
						"valueFrom": map[string]interface{}{
							"secretKeyRef": map[string]interface{}{
								"name": stackSecretName,
								"key":  key,
							},
						},
					})
					result.SecretsInjected++
				}
			}

			// Update container env
			containerMap["env"] = filteredEnv
			containers[i] = containerMap
		}

		// Write back containers
		if err := unstructured.SetNestedSlice(obj.Object, containers, containersPath...); err != nil {
			log.Error(err, "Failed to set containers with config",
				"kind", resourceKind,
				"name", resourceName)
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to update containers in %s %s: %v", resourceKind, resourceName, err))
		} else {
			log.Info("Injected config",
				"kind", resourceKind,
				"name", resourceName,
				"variables", len(mergedVars),
				"secrets", len(resolvedKeys))
		}
	}

	return result
}

// getBlueprint fetches the blueprint referenced by the stack
func (r *StackReconciler) getBlueprint(ctx context.Context, stack *envv1alpha1.Stack) (*envv1alpha1.Blueprint, error) {
	// Parse blueprint reference using shared utility
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
