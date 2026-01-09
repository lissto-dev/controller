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

const (
	// ResourceClassAnnotation is required on all resources to classify them as state or workload
	ResourceClassAnnotation = "lissto.dev/class"

	// ResourceClassState marks resources that are preserved during suspension (e.g., PVCs)
	ResourceClassState = "state"

	// ResourceClassWorkload marks resources that are deleted during suspension (e.g., Deployments)
	ResourceClassWorkload = "workload"

	// MaxPhaseHistoryLength is the maximum number of phase transitions to keep in history
	MaxPhaseHistoryLength = 10
)

// StackPhase represents the lifecycle phase of a Stack
type StackPhase string

const (
	// StackPhaseRunning means all (non-suspended) services have their workloads applied
	StackPhaseRunning StackPhase = "Running"

	// StackPhaseSuspending means workloads are being deleted, waiting for pods to terminate
	StackPhaseSuspending StackPhase = "Suspending"

	// StackPhaseSuspended means all workload resources are deleted, only state resources exist
	StackPhaseSuspended StackPhase = "Suspended"

	// StackPhaseResuming means workloads are being recreated
	StackPhaseResuming StackPhase = "Resuming"
)

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

	// ContainerName is the custom container name if specified in docker-compose
	// If empty, defaults to service name for matching
	// +optional
	ContainerName string `json:"containerName,omitempty"`
}

// ServiceVolumes specifies volume snapshots to restore for a service
type ServiceVolumes struct {
	// SnapshotRefs references VolumeSnapshots to restore
	// Each snapshot's volumeIdentifier.mountPath determines the target PVC
	// +required
	SnapshotRefs []string `json:"snapshotRefs"`
}

// StackSpec defines the desired state of Stack
type StackSpec struct {
	// BlueprintReference references the Blueprint resource
	// This field is immutable after creation
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="blueprintReference is immutable after creation"
	BlueprintReference string `json:"blueprintReference"`

	// ManifestsConfigMapRef references the ConfigMap containing the generated Kubernetes YAML manifests
	// This field is immutable after creation
	// +optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="manifestsConfigMapRef is immutable after creation"
	ManifestsConfigMapRef string `json:"manifestsConfigMapRef"`

	// Env references the Env resource
	// This field is immutable after creation
	// +required
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="env is immutable after creation"
	Env string `json:"env"`

	// Images contains the service to image information mapping
	// This is the only mutable field - updates trigger rolling deployments
	// +required
	Images map[string]ImageInfo `json:"images"`

	// Volumes specifies snapshots to restore for each service
	// The snapshot's volumeIdentifier.mountPath determines which PVC to restore
	// +optional
	Volumes map[string]ServiceVolumes `json:"volumes,omitempty"`

	// Metadata contains stack creation metadata
	// This field is mutable and can be updated to track image sources
	// +optional
	Metadata StackMetadata `json:"metadata,omitempty"`

	// Suspended when true causes all workload resources to be deleted
	// while preserving state resources (those with lissto.dev/class: state)
	// +optional
	Suspended bool `json:"suspended,omitempty"`

	// SuspendedServices lists services to suspend even when Suspended is false
	// Each service name corresponds to the io.kompose.service label value
	// All workload resources for these services will be deleted
	// +optional
	SuspendedServices []string `json:"suspendedServices,omitempty"`

	// SuspendTimeout is how long to wait for workloads to terminate when suspending
	// Defaults to controller config stack.defaultSuspendTimeout (default: 5m)
	// +optional
	SuspendTimeout *metav1.Duration `json:"suspendTimeout,omitempty"`
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

// PhaseTransition records a phase change in the stack lifecycle
type PhaseTransition struct {
	// Phase that was transitioned to
	Phase StackPhase `json:"phase"`

	// TransitionTime is when the transition occurred
	TransitionTime metav1.Time `json:"transitionTime"`

	// Reason is a machine-readable reason for the transition
	// +optional
	Reason string `json:"reason,omitempty"`

	// Message is a human-readable description
	// +optional
	Message string `json:"message,omitempty"`
}

// ServiceStatus tracks the status of a single service within the stack
type ServiceStatus struct {
	// Phase is the current phase of this service
	Phase StackPhase `json:"phase"`

	// SuspendedAt is when the service was suspended (nil if running)
	// +optional
	SuspendedAt *metav1.Time `json:"suspendedAt,omitempty"`
}

// StackStatus defines the observed state of Stack.
type StackStatus struct {
	// Phase indicates the current lifecycle phase of the Stack
	// +optional
	Phase StackPhase `json:"phase,omitempty"`

	// PhaseHistory tracks recent phase transitions (last 10)
	// +optional
	// +listType=atomic
	PhaseHistory []PhaseTransition `json:"phaseHistory,omitempty"`

	// Services contains per-service status information
	// +optional
	Services map[string]ServiceStatus `json:"services,omitempty"`

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

	// ObservedGeneration is the generation of the Stack that was most recently reconciled
	// If this differs from metadata.generation, the spec has changed but hasn't been reconciled yet
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
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
