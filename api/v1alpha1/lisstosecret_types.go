/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LisstoSecretSpec defines the desired state of LisstoSecret
type LisstoSecretSpec struct {
	// Scope defines the hierarchy level for this secret config
	// +kubebuilder:validation:Enum=env;repo;global
	// +kubebuilder:default=env
	// +optional
	Scope string `json:"scope,omitempty"`

	// Env binds this config to a specific Env resource (required for scope=env)
	// +optional
	Env string `json:"env,omitempty"`

	// Repository binds this config to a specific repository (required for scope=repo)
	// +optional
	Repository string `json:"repository,omitempty"`

	// Keys contains the secret key names (values are stored in the referenced K8s Secret)
	// +optional
	Keys []string `json:"keys,omitempty"`

	// SecretRef is the name of the K8s Secret containing the actual secret values
	// This Secret is managed by the API, not the controller
	// +optional
	SecretRef string `json:"secretRef,omitempty"`
}

// LisstoSecretStatus defines the observed state of LisstoSecret
type LisstoSecretStatus struct {
	// Conditions represent the current state of the LisstoSecret resource
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// SyncedKeys tracks which keys have been synced to the managed secret
	// +optional
	SyncedKeys []string `json:"syncedKeys,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Scope",type=string,JSONPath=`.spec.scope`
// +kubebuilder:printcolumn:name="Env",type=string,JSONPath=`.spec.env`
// +kubebuilder:printcolumn:name="Repository",type=string,JSONPath=`.spec.repository`
// +kubebuilder:printcolumn:name="Keys",type=integer,JSONPath=`.status.syncedKeys`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LisstoSecret is the Schema for the lisstosecrets API
// It stores write-only secret key references with hierarchical scope (env > repo > global)
// Secret values are stored in a separate K8s Secret referenced by secretRef
type LisstoSecret struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec LisstoSecretSpec `json:"spec"`

	// +optional
	Status LisstoSecretStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// LisstoSecretList contains a list of LisstoSecret
type LisstoSecretList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LisstoSecret `json:"items"`
}

// GetScope returns the scope, defaulting to "env" if not set
func (s *LisstoSecret) GetScope() string {
	if s.Spec.Scope == "" {
		return "env"
	}
	return s.Spec.Scope
}

// GetSecretRef returns the secret reference name
// Defaults to {name}-data if not explicitly set
func (s *LisstoSecret) GetSecretRef() string {
	if s.Spec.SecretRef == "" {
		return s.Name + "-data"
	}
	return s.Spec.SecretRef
}

func init() {
	SchemeBuilder.Register(&LisstoSecret{}, &LisstoSecretList{})
}
