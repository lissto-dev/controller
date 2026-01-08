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
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	kindDeployment = "Deployment"
	kindPod        = "Pod"
)

// Injector handles injecting config into workloads
type Injector struct{}

// NewInjector creates a new config injector
func NewInjector() *Injector {
	return &Injector{}
}

// Inject injects environment variables and secret refs into deployments and pods
func (i *Injector) Inject(ctx context.Context, objects []*unstructured.Unstructured,
	mergedVars map[string]string, resolvedKeys map[string]SecretKeySource,
	stackSecretName string, metadata StackMetadata) *InjectionResult {

	log := logf.FromContext(ctx)
	result := &InjectionResult{}

	for _, obj := range objects {
		if obj.GetKind() != kindDeployment && obj.GetKind() != kindPod {
			continue
		}

		resourceKind := obj.GetKind()
		resourceName := obj.GetName()
		containersPath := getContainersPath(resourceKind)

		workloadAnnotations := getWorkloadAnnotations(obj)
		injectionConfig := extractInjectionConfig(workloadAnnotations)

		log.V(1).Info("Extracted injection config",
			"kind", resourceKind, "name", resourceName,
			"autoInject", injectionConfig.AutoInject,
			"secretMappings", len(injectionConfig.SecretMap),
			"varMappings", len(injectionConfig.VarMap))

		containers, found, err := unstructured.NestedSlice(obj.Object, containersPath...)
		if err != nil || !found {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to get containers from %s %s", resourceKind, resourceName))
			continue
		}

		for idx, container := range containers {
			containerMap, ok := container.(map[string]interface{})
			if !ok {
				continue
			}

			existingEnv, _, _ := unstructured.NestedSlice(containerMap, "env")
			if existingEnv == nil {
				existingEnv = []interface{}{}
			}

			existingEnvNames := collectExistingEnvNames(existingEnv)
			mappedVars, mappedSecrets, mappingWarnings := filterEnvWithConfig(
				mergedVars, resolvedKeys, existingEnvNames, injectionConfig)

			for _, w := range mappingWarnings {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("%s/%s: %s", resourceKind, resourceName, w))
			}

			containerMap["env"] = buildInjectionEnv(
				existingEnv, mappedVars, mappedSecrets, stackSecretName, metadata, result)
			containers[idx] = containerMap
		}

		if err := unstructured.SetNestedSlice(obj.Object, containers, containersPath...); err != nil {
			log.Error(err, "Failed to set containers with config",
				"kind", resourceKind, "name", resourceName)
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Failed to update containers in %s %s: %v", resourceKind, resourceName, err))
		} else {
			log.V(1).Info("Injected config into workload",
				"kind", resourceKind, "name", resourceName,
				"containers", len(containers),
				"variablesInjected", result.VariablesInjected,
				"secretsInjected", result.SecretsInjected)
		}
	}

	return result
}

// getContainersPath returns the path to containers based on resource kind
func getContainersPath(resourceKind string) []string {
	if resourceKind == kindDeployment {
		return []string{"spec", "template", "spec", "containers"}
	}
	return []string{"spec", "containers"}
}

// getWorkloadAnnotations extracts annotations from workload metadata
func getWorkloadAnnotations(obj *unstructured.Unstructured) map[string]string {
	if obj.GetKind() == kindDeployment {
		annotations, found, _ := unstructured.NestedStringMap(obj.Object, "spec", "template", "metadata", "annotations")
		if found && annotations != nil {
			return annotations
		}
	}
	return obj.GetAnnotations()
}

// extractInjectionConfig extracts injection configuration from workload annotations
func extractInjectionConfig(annotations map[string]string) InjectionConfig {
	config := InjectionConfig{
		AutoInject: true,
		SecretMap:  make(map[string]string),
		VarMap:     make(map[string]string),
	}

	if annotations == nil {
		return config
	}

	config.AutoInject = parseAutoInject(annotations[AnnotationAutoInject])
	config.SecretMap = parseKeyMapping(annotations[AnnotationInjectSecrets])
	config.VarMap = parseKeyMapping(annotations[AnnotationInjectVars])

	return config
}

// parseAutoInject parses the auto-inject annotation value
func parseAutoInject(value string) bool {
	if value == "" {
		return true
	}
	return strings.ToLower(value) != "false"
}

// parseKeyMapping parses a comma-separated key mapping annotation
func parseKeyMapping(annotation string) map[string]string {
	result := make(map[string]string)
	if annotation == "" {
		return result
	}

	pairs := strings.Split(annotation, ",")
	for _, pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		target := strings.TrimSpace(parts[0])
		source := strings.TrimSpace(parts[1])
		if target != "" && source != "" {
			result[target] = source
		}
	}
	return result
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
		if !IsReservedEnvVar(name) {
			names[name] = true
		}
	}
	return names
}

// filterEnvWithConfig filters variables/secrets based on injection config
func filterEnvWithConfig(mergedVars map[string]string, resolvedKeys map[string]SecretKeySource,
	existingEnvNames map[string]bool, config InjectionConfig) ([]MappedVar, []MappedSecret, []string) {

	var filteredVars []MappedVar
	var filteredSecrets []MappedSecret
	var warnings []string

	addedTargets := make(map[string]bool)

	// Process explicit variable mappings
	for targetEnv, sourceKey := range config.VarMap {
		if IsReservedEnvVar(targetEnv) {
			continue
		}
		if value, exists := mergedVars[sourceKey]; exists {
			filteredVars = append(filteredVars, MappedVar{
				TargetEnvName: targetEnv,
				SourceKey:     sourceKey,
				Value:         value,
			})
			addedTargets[targetEnv] = true
		} else {
			warnings = append(warnings, fmt.Sprintf(
				"variable mapping %s=%s: source key %q not found in LisstoVariables",
				targetEnv, sourceKey, sourceKey))
		}
	}

	// Process explicit secret mappings
	for targetEnv, sourceKey := range config.SecretMap {
		if IsReservedEnvVar(targetEnv) {
			continue
		}
		if source, exists := resolvedKeys[sourceKey]; exists {
			filteredSecrets = append(filteredSecrets, MappedSecret{
				TargetEnvName: targetEnv,
				SourceKey:     sourceKey,
				Source:        source,
			})
			addedTargets[targetEnv] = true
		} else {
			warnings = append(warnings, fmt.Sprintf(
				"secret mapping %s=%s: source key %q not found in LisstoSecrets",
				targetEnv, sourceKey, sourceKey))
		}
	}

	// Auto-inject matching declared env vars
	if config.AutoInject {
		for key, source := range resolvedKeys {
			if existingEnvNames[key] && !addedTargets[key] {
				filteredSecrets = append(filteredSecrets, MappedSecret{
					TargetEnvName: key,
					SourceKey:     key,
					Source:        source,
				})
				addedTargets[key] = true
			}
		}

		for key, value := range mergedVars {
			if existingEnvNames[key] && !addedTargets[key] {
				filteredVars = append(filteredVars, MappedVar{
					TargetEnvName: key,
					SourceKey:     key,
					Value:         value,
				})
				addedTargets[key] = true
			}
		}
	}

	return filteredVars, filteredSecrets, warnings
}

// buildInjectionEnv builds the final env var list with mapped variables and secrets
func buildInjectionEnv(existingEnv []interface{}, mappedVars []MappedVar,
	mappedSecrets []MappedSecret, stackSecretName string, metadata StackMetadata,
	result *InjectionResult) []interface{} {

	secretTargets := make(map[string]bool)
	for _, s := range mappedSecrets {
		secretTargets[s.TargetEnvName] = true
	}

	targetsToInject := make(map[string]bool)
	for _, v := range mappedVars {
		targetsToInject[v.TargetEnvName] = true
	}
	for _, s := range mappedSecrets {
		targetsToInject[s.TargetEnvName] = true
	}

	filteredEnv := make([]interface{}, 0, len(existingEnv)+len(mappedVars)+len(mappedSecrets)+3)
	for _, envVar := range existingEnv {
		envMap, ok := envVar.(map[string]interface{})
		if !ok {
			continue
		}
		name, ok := envMap["name"].(string)
		if !ok || IsReservedEnvVar(name) || targetsToInject[name] {
			continue
		}
		filteredEnv = append(filteredEnv, envVar)
	}

	// Inject reserved metadata
	filteredEnv = append(filteredEnv,
		map[string]interface{}{"name": "LISSTO_ENV", "value": metadata.Env},
		map[string]interface{}{"name": "LISSTO_STACK", "value": metadata.StackName},
		map[string]interface{}{"name": "LISSTO_USER", "value": metadata.User},
	)
	result.MetadataInjected = 3

	// Add variables (skip if target is also a secret)
	for _, v := range mappedVars {
		if secretTargets[v.TargetEnvName] {
			continue
		}
		filteredEnv = append(filteredEnv, map[string]interface{}{
			"name": v.TargetEnvName, "value": v.Value,
		})
		result.VariablesInjected++
	}

	// Add secret refs
	if stackSecretName != "" {
		for _, s := range mappedSecrets {
			filteredEnv = append(filteredEnv, map[string]interface{}{
				"name": s.TargetEnvName,
				"valueFrom": map[string]interface{}{
					"secretKeyRef": map[string]interface{}{
						"name": stackSecretName,
						"key":  s.SourceKey,
					},
				},
			})
			result.SecretsInjected++
		}
	}

	return filteredEnv
}
