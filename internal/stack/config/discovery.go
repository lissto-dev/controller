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

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// Discoverer finds LisstoVariables and LisstoSecrets that match a stack
type Discoverer struct {
	client   client.Client
	globalNS string
}

// NewDiscoverer creates a new config discoverer
func NewDiscoverer(c client.Client, globalNS string) *Discoverer {
	return &Discoverer{client: c, globalNS: globalNS}
}

// DiscoverVariables finds all LisstoVariable resources that match the stack
func (d *Discoverer) DiscoverVariables(ctx context.Context, stack *envv1alpha1.Stack, repository string) ([]envv1alpha1.LisstoVariable, error) {
	log := logf.FromContext(ctx)
	userNS := stack.Namespace

	userVars := &envv1alpha1.LisstoVariableList{}
	if err := d.client.List(ctx, userVars, client.InNamespace(userNS)); err != nil {
		log.Error(err, "Failed to list variables in user namespace")
		return nil, err
	}

	var globalVars *envv1alpha1.LisstoVariableList
	if d.globalNS != userNS {
		globalVars = &envv1alpha1.LisstoVariableList{}
		if err := d.client.List(ctx, globalVars, client.InNamespace(d.globalNS)); err != nil {
			log.Error(err, "Failed to list variables in global namespace")
			globalVars = nil
		}
	}

	var matched []envv1alpha1.LisstoVariable

	for _, v := range userVars.Items {
		if matchesStack(&v, stack, repository) {
			matched = append(matched, v)
		}
	}

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
		"env", stack.Spec.Env)

	return matched, nil
}

// DiscoverSecrets finds all LisstoSecret resources that match the stack
func (d *Discoverer) DiscoverSecrets(ctx context.Context, stack *envv1alpha1.Stack, repository string) ([]envv1alpha1.LisstoSecret, error) {
	log := logf.FromContext(ctx)
	userNS := stack.Namespace

	userSecrets := &envv1alpha1.LisstoSecretList{}
	if err := d.client.List(ctx, userSecrets, client.InNamespace(userNS)); err != nil {
		log.Error(err, "Failed to list secrets in user namespace")
		return nil, err
	}

	var globalSecrets *envv1alpha1.LisstoSecretList
	if d.globalNS != userNS {
		globalSecrets = &envv1alpha1.LisstoSecretList{}
		if err := d.client.List(ctx, globalSecrets, client.InNamespace(d.globalNS)); err != nil {
			log.Error(err, "Failed to list secrets in global namespace")
			globalSecrets = nil
		}
	}

	var matched []envv1alpha1.LisstoSecret

	for _, s := range userSecrets.Items {
		if matchesStackSecret(&s, stack, repository) {
			matched = append(matched, s)
		}
	}

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
		"env", stack.Spec.Env)

	return matched, nil
}

// matchesStack checks if a LisstoVariable matches the stack's scope criteria
func matchesStack(v *envv1alpha1.LisstoVariable, stack *envv1alpha1.Stack, repository string) bool {
	scope := v.GetScope()

	switch scope {
	case ScopeGlobal:
		return true
	case ScopeRepo:
		return repository != "" && v.Spec.Repository != "" && v.Spec.Repository == repository
	case ScopeEnv:
		return stack.Spec.Env != "" && v.Spec.Env != "" && v.Spec.Env == stack.Spec.Env
	default:
		return false
	}
}

// matchesStackSecret checks if a LisstoSecret matches the stack's scope criteria
func matchesStackSecret(s *envv1alpha1.LisstoSecret, stack *envv1alpha1.Stack, repository string) bool {
	scope := s.GetScope()

	switch scope {
	case ScopeGlobal:
		return true
	case ScopeRepo:
		return repository != "" && s.Spec.Repository != "" && s.Spec.Repository == repository
	case ScopeEnv:
		return stack.Spec.Env != "" && s.Spec.Env != "" && s.Spec.Env == stack.Spec.Env
	default:
		return false
	}
}
