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
	"sort"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// Resolver handles merging variables and secrets with hierarchy
type Resolver struct{}

// NewResolver creates a new config resolver
func NewResolver() *Resolver {
	return &Resolver{}
}

// ResolveVariables merges variables with hierarchy (env > repo > global)
func (r *Resolver) ResolveVariables(variables []envv1alpha1.LisstoVariable) map[string]string {
	log := logf.Log.WithName("config-resolver")

	// Sort by priority (higher priority = lower number)
	sort.Slice(variables, func(i, j int) bool {
		return ScopePriority(variables[i].GetScope()) > ScopePriority(variables[j].GetScope())
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
		if IsReservedEnvVar(key) {
			log.V(1).Info("Skipping reserved environment variable from LisstoVariable", "key", key)
			continue
		}
		filtered[key] = value
	}

	return filtered
}

// ResolveSecretKeys resolves which secret provides each key (key-level resolution)
func (r *Resolver) ResolveSecretKeys(secrets []envv1alpha1.LisstoSecret) map[string]SecretKeySource {
	log := logf.Log.WithName("config-resolver")
	resolved := make(map[string]SecretKeySource)

	for i := range secrets {
		secret := &secrets[i]
		priority := ScopePriority(secret.GetScope())

		for _, key := range secret.Spec.Keys {
			if IsReservedEnvVar(key) {
				log.V(1).Info("Skipping reserved environment variable from LisstoSecret",
					"key", key, "secret", secret.Name)
				continue
			}

			existing, found := resolved[key]
			if !found || priority < existing.Priority {
				resolved[key] = SecretKeySource{
					LisstoSecret: secret,
					Key:          key,
					Priority:     priority,
				}
			}
		}
	}

	return resolved
}
