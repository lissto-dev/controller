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
	"reflect"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/pkg/util"
)

// Reserved environment variable prefixes that cannot be set by users
// These are injected by the controller and override any user values
const ReservedEnvPrefix = "LISSTO_"

// Config scope constants for variable/secret hierarchy
const (
	scopeEnv    = "env"
	scopeRepo   = "repo"
	scopeGlobal = "global"
)

// Kubernetes resource kinds
const (
	kindDeployment = "Deployment"
	kindPod        = "Pod"
)

// isReservedEnvVar checks if an env var name is reserved
// Any variable starting with LISSTO_ is considered reserved
func isReservedEnvVar(name string) bool {
	return strings.HasPrefix(name, ReservedEnvPrefix)
}

// ConfigInjectionResult tracks the result of config injection
type ConfigInjectionResult struct {
	VariablesInjected int
	SecretsInjected   int
	MetadataInjected  int                 // Count of reserved metadata variables injected
	MissingSecretKeys map[string][]string // secretRef -> missing keys
	Warnings          []string
}

// StackMetadata contains reserved metadata to inject as environment variables
type StackMetadata struct {
	Env       string
	StackName string
	User      string
}

// extractStackMetadata extracts metadata from Stack resource for injection
func extractStackMetadata(stack *envv1alpha1.Stack) StackMetadata {
	// Extract env from Stack spec (e.g., "dev-daniel", "staging")
	env := stack.Spec.Env

	// Extract user from Stack annotation
	// Note: Annotations can be modified by users with edit permissions,
	// but RBAC typically limits this. Can be moved to Spec later if needed.
	user := "unknown"
	if stack.Annotations != nil {
		if createdBy, ok := stack.Annotations["lissto.dev/created-by"]; ok && createdBy != "" {
			user = createdBy
		}
	}

	return StackMetadata{
		Env:       env,
		StackName: stack.Name,
		User:      user,
	}
}

// scopePriority returns the priority of a scope (lower = higher priority)
func scopePriority(scope string) int {
	switch scope {
	case scopeEnv:
		return 1
	case scopeRepo:
		return 2
	case scopeGlobal:
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
	log := logf.Log.WithName("config-injection")

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

	// Filter out reserved variables
	filtered := make(map[string]string)
	for key, value := range merged {
		if isReservedEnvVar(key) {
			log.V(1).Info("Skipping reserved environment variable from LisstoVariable",
				"key", key)
			continue
		}
		filtered[key] = value
	}

	return filtered
}

// secretKeySource tracks which LisstoSecret provides each key
type secretKeySource struct {
	LisstoSecret *envv1alpha1.LisstoSecret
	Key          string
	Priority     int
}

// resolveSecretKeys resolves which secret provides each key (key-level resolution)
func (r *StackReconciler) resolveSecretKeys(secrets []envv1alpha1.LisstoSecret) map[string]secretKeySource {
	log := logf.Log.WithName("config-injection")
	resolved := make(map[string]secretKeySource)

	for i := range secrets {
		secret := &secrets[i]
		priority := scopePriority(secret.GetScope())

		for _, key := range secret.Spec.Keys {
			// Skip reserved variable names
			if isReservedEnvVar(key) {
				log.V(1).Info("Skipping reserved environment variable from LisstoSecret",
					"key", key,
					"secret", secret.Name)
				continue
			}

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
		// Update existing only if data has changed
		if !reflect.DeepEqual(existing.Data, mergedData) {
			existing.Data = mergedData
			if err := r.Update(ctx, existing); err != nil {
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

// collectExistingEnvNames extracts non-reserved env var names from a container
func collectExistingEnvNames(existingEnv []interface{}) map[string]bool {
	names := make(map[string]bool)
	for _, envVar := range existingEnv {
		envMap, ok := envVar.(map[string]interface{})
		if !ok {
			continue
		}
		name, ok := envMap["name"].(string)
		if !ok {
			continue
		}
		if !isReservedEnvVar(name) {
			names[name] = true
		}
	}
	return names
}

// filterEnvByDeclared filters variables/secrets to only those declared in container
func filterEnvByDeclared(mergedVars map[string]string, resolvedKeys map[string]secretKeySource,
	existingEnvNames map[string]bool) (map[string]string, map[string]secretKeySource) {
	filteredVars := make(map[string]string)
	for key, value := range mergedVars {
		if existingEnvNames[key] {
			filteredVars[key] = value
		}
	}

	filteredSecrets := make(map[string]secretKeySource)
	for key, source := range resolvedKeys {
		if existingEnvNames[key] {
			filteredSecrets[key] = source
		}
	}
	return filteredVars, filteredSecrets
}

// buildInjectionEnv builds the final env var list with proper hierarchy
func buildInjectionEnv(existingEnv []interface{}, filteredVars map[string]string,
	filteredSecrets map[string]secretKeySource, stackSecretName string, metadata StackMetadata,
	result *ConfigInjectionResult) []interface{} {
	// Build set of secret keys (highest priority)
	secretKeys := make(map[string]bool)
	for key := range filteredSecrets {
		secretKeys[key] = true
	}

	// Build set of all keys to inject
	keysToInject := make(map[string]bool)
	for key := range filteredVars {
		keysToInject[key] = true
	}
	for key := range filteredSecrets {
		keysToInject[key] = true
	}

	// Filter existing env vars (remove conflicts and reserved)
	filteredEnv := make([]interface{}, 0, len(existingEnv)+len(filteredVars)+len(filteredSecrets)+3)
	for _, envVar := range existingEnv {
		envMap, ok := envVar.(map[string]interface{})
		if !ok {
			continue
		}
		name, ok := envMap["name"].(string)
		if !ok || isReservedEnvVar(name) || keysToInject[name] {
			continue
		}
		filteredEnv = append(filteredEnv, envVar)
	}

	// Inject reserved metadata (always injected)
	filteredEnv = append(filteredEnv,
		map[string]interface{}{"name": "LISSTO_ENV", "value": metadata.Env},
		map[string]interface{}{"name": "LISSTO_STACK", "value": metadata.StackName},
		map[string]interface{}{"name": "LISSTO_USER", "value": metadata.User},
	)
	result.MetadataInjected = 3

	// Add variables (skip if also a secret)
	for key, value := range filteredVars {
		if secretKeys[key] {
			continue
		}
		filteredEnv = append(filteredEnv, map[string]interface{}{"name": key, "value": value})
		result.VariablesInjected++
	}

	// Add secret refs
	if stackSecretName != "" {
		for key := range filteredSecrets {
			filteredEnv = append(filteredEnv, map[string]interface{}{
				"name": key,
				"valueFrom": map[string]interface{}{
					"secretKeyRef": map[string]interface{}{"name": stackSecretName, "key": key},
				},
			})
			result.SecretsInjected++
		}
	}

	return filteredEnv
}

// getContainersPath returns the path to containers based on resource kind
func getContainersPath(resourceKind string) []string {
	if resourceKind == kindDeployment {
		return []string{"spec", "template", "spec", "containers"}
	}
	return []string{"spec", "containers"}
}

// injectConfigIntoDeployments injects environment variables and secret refs into deployments
func (r *StackReconciler) injectConfigIntoWorkloads(ctx context.Context, objects []*unstructured.Unstructured,
	mergedVars map[string]string, resolvedKeys map[string]secretKeySource, stackSecretName string, metadata StackMetadata) *ConfigInjectionResult {
	log := logf.FromContext(ctx)
	result := &ConfigInjectionResult{}

	for _, obj := range objects {
		if obj.GetKind() != kindDeployment && obj.GetKind() != kindPod {
			continue
		}

		resourceKind := obj.GetKind()
		resourceName := obj.GetName()
		containersPath := getContainersPath(resourceKind)

		containers, found, err := unstructured.NestedSlice(obj.Object, containersPath...)
		if err != nil || !found {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to get containers from %s %s", resourceKind, resourceName))
			continue
		}

		for i, container := range containers {
			containerMap, ok := container.(map[string]interface{})
			if !ok {
				continue
			}

			existingEnv, _, _ := unstructured.NestedSlice(containerMap, "env")
			if existingEnv == nil {
				existingEnv = []interface{}{}
			}

			existingEnvNames := collectExistingEnvNames(existingEnv)
			filteredVars, filteredSecrets := filterEnvByDeclared(mergedVars, resolvedKeys, existingEnvNames)
			containerMap["env"] = buildInjectionEnv(existingEnv, filteredVars, filteredSecrets, stackSecretName, metadata, result)
			containers[i] = containerMap
		}

		if err := unstructured.SetNestedSlice(obj.Object, containers, containersPath...); err != nil {
			log.Error(err, "Failed to set containers with config", "kind", resourceKind, "name", resourceName)
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to update containers in %s %s: %v", resourceKind, resourceName, err))
		} else {
			log.V(1).Info("Injected config into workload", "kind", resourceKind, "name", resourceName,
				"containers", len(containers), "variablesInjected", result.VariablesInjected, "secretsInjected", result.SecretsInjected)
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
