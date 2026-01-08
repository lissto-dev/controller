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
	"strings"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// Reserved environment variable prefixes that cannot be set by users
const ReservedEnvPrefix = "LISSTO_"

// Config scope constants for variable/secret hierarchy
const (
	ScopeEnv    = "env"
	ScopeRepo   = "repo"
	ScopeGlobal = "global"
)

// Annotation keys for injection control
const (
	AnnotationAutoInject    = "lissto.dev/auto-inject"
	AnnotationInjectSecrets = "lissto.dev/inject-secrets"
	AnnotationInjectVars    = "lissto.dev/inject-vars"
)

// InjectionResult tracks the result of config injection
type InjectionResult struct {
	VariablesInjected int
	SecretsInjected   int
	MetadataInjected  int
	MissingSecretKeys map[string][]string
	Warnings          []string
}

// StackMetadata contains reserved metadata to inject as environment variables
type StackMetadata struct {
	Env       string
	StackName string
	User      string
}

// ExtractStackMetadata extracts metadata from Stack resource for injection
func ExtractStackMetadata(stack *envv1alpha1.Stack) StackMetadata {
	user := "unknown"
	if stack.Annotations != nil {
		if createdBy, ok := stack.Annotations["lissto.dev/created-by"]; ok && createdBy != "" {
			user = createdBy
		}
	}

	return StackMetadata{
		Env:       stack.Spec.Env,
		StackName: stack.Name,
		User:      user,
	}
}

// InjectionConfig holds parsed injection configuration from workload annotations
type InjectionConfig struct {
	AutoInject bool
	SecretMap  map[string]string // targetEnvName -> sourceSecretKey
	VarMap     map[string]string // targetEnvName -> sourceVarKey
}

// SecretKeySource tracks which LisstoSecret provides each key
type SecretKeySource struct {
	LisstoSecret *envv1alpha1.LisstoSecret
	Key          string
	Priority     int
}

// MappedVar represents a variable with target env name and source key
type MappedVar struct {
	TargetEnvName string
	SourceKey     string
	Value         string
}

// MappedSecret represents a secret with target env name and source key
type MappedSecret struct {
	TargetEnvName string
	SourceKey     string
	Source        SecretKeySource
}

// IsReservedEnvVar checks if an env var name is reserved
func IsReservedEnvVar(name string) bool {
	return strings.HasPrefix(name, ReservedEnvPrefix)
}

// ScopePriority returns the priority of a scope (lower = higher priority)
func ScopePriority(scope string) int {
	switch scope {
	case ScopeEnv:
		return 1
	case ScopeRepo:
		return 2
	case ScopeGlobal:
		return 3
	default:
		return 99
	}
}
