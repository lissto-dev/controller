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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ImageInfo contains information about a resolved image
type ImageInfo struct {
	// Digest is the full image digest (e.g., sha256:abc123...)
	// +required
	Digest string `json:"digest"`

	// Image is the user-friendly image tag that was resolved (e.g., myimage:v1.2.3)
	// +optional
	Image string `json:"image,omitempty"`

	// URL is the exposed service URL (if the service is exposed)
	// +optional
	URL string `json:"url,omitempty"`
}

// StackSpec defines the desired state of Stack
type StackSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html

	// BlueprintReference references the Blueprint resource
	// +required
	BlueprintReference string `json:"blueprintReference"`

	// ManifestsConfigMapRef references the ConfigMap containing the generated Kubernetes YAML manifests
	// +optional
	ManifestsConfigMapRef string `json:"manifestsConfigMapRef"`

	// Env references the Env resource
	// +required
	Env string `json:"env"`

	// Images contains the service to image information mapping
	// +required
	Images map[string]ImageInfo `json:"images"`

	// Metadata contains stack creation metadata
	// +optional
	Metadata StackMetadata `json:"metadata,omitempty"`
}

// StackMetadata contains metadata about the stack creation
type StackMetadata struct {
	// Commit hash used for image resolution
	// +optional
	Commit string `json:"commit,omitempty"`

	// Tag used for image resolution
	// +optional
	Tag string `json:"tag,omitempty"`

	// Branch used for image resolution
	// +optional
	Branch string `json:"branch,omitempty"`

	// Author who created the stack
	// +optional
	Author string `json:"author,omitempty"`
}

// StackStatus defines the observed state of Stack.
type StackStatus struct {
	// Conditions track the status of each resource and overall stack state
	// - Type="Ready": Overall stack readiness (Status=True/False)
	// - Type="Resource/{Kind}/{Name}": Individual resource status
	//   Status=True (applied successfully), False (failed)
	//   Reason="Applied" or "Failed"
	//   Message contains error details if failed
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// Stack is the Schema for the stacks API
type Stack struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Stack
	// +required
	Spec StackSpec `json:"spec"`

	// status defines the observed state of Stack
	// +optional
	Status StackStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// StackList contains a list of Stack
type StackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Stack `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Stack{}, &StackList{})
}
