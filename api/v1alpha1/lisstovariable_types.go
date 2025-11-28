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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LisstoVariableSpec defines the desired state of LisstoVariable
type LisstoVariableSpec struct {
	// Scope defines the hierarchy level for this variable config
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

	// Data contains the environment variable key-value pairs
	// +optional
	Data map[string]string `json:"data,omitempty"`
}

// LisstoVariableStatus defines the observed state of LisstoVariable
type LisstoVariableStatus struct {
	// Conditions represent the current state of the LisstoVariable resource
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Scope",type=string,JSONPath=`.spec.scope`
// +kubebuilder:printcolumn:name="Env",type=string,JSONPath=`.spec.env`
// +kubebuilder:printcolumn:name="Repository",type=string,JSONPath=`.spec.repository`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// LisstoVariable is the Schema for the lisstovariables API
// It stores readable environment variables with hierarchical scope (env > repo > global)
type LisstoVariable struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// +required
	Spec LisstoVariableSpec `json:"spec"`

	// +optional
	Status LisstoVariableStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// LisstoVariableList contains a list of LisstoVariable
type LisstoVariableList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LisstoVariable `json:"items"`
}

// GetScope returns the scope, defaulting to "env" if not set
func (v *LisstoVariable) GetScope() string {
	if v.Spec.Scope == "" {
		return "env"
	}
	return v.Spec.Scope
}

func init() {
	SchemeBuilder.Register(&LisstoVariable{}, &LisstoVariableList{})
}
