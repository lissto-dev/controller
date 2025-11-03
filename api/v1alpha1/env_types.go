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

// EnvSpec defines the desired state of Env
// The env name is stored in metadata.name (Kubernetes standard)
type EnvSpec struct {
	// EnvSpec is empty - name is in metadata.name
}

// EnvStatus defines the observed state of Env.
type EnvStatus struct {
	// conditions represent the current state of the Env resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion
// +kubebuilder:rbac:groups=env.lissto.dev,resources=envs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=env.lissto.dev,resources=envs/status,verbs=get;update;patch

// Env is the Schema for the envs API
type Env struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Env
	// +required
	Spec EnvSpec `json:"spec"`

	// status defines the observed state of Env
	// +optional
	Status EnvStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// EnvList contains a list of Env
type EnvList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Env `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Env{}, &EnvList{})
}
