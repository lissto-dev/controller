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
	// VolumeSnapshotAnnotationSourceBlueprint is the annotation key for the source blueprint
	// Used for auto-restore scenarios during maintenance
	VolumeSnapshotAnnotationSourceBlueprint = "env.lissto.dev/source-blueprint"

	// VolumeSnapshotLabelUser is the label key for the user
	VolumeSnapshotLabelUser = "env.lissto.dev/user"

	// VolumeSnapshotLabelEnv is the label key for the environment
	VolumeSnapshotLabelEnv = "env.lissto.dev/env"

	// VolumeSnapshotLabelRepoHash is the label key for the repository hash
	VolumeSnapshotLabelRepoHash = "env.lissto.dev/repo-hash"

	// VolumeSnapshotLabelService is the label key for the service name
	VolumeSnapshotLabelService = "env.lissto.dev/service"
)

// VolumeSnapshotPhase represents the current phase of a VolumeSnapshot
type VolumeSnapshotPhase string

const (
	// VolumeSnapshotPhasePending indicates the snapshot is waiting to be processed
	VolumeSnapshotPhasePending VolumeSnapshotPhase = "Pending"

	// VolumeSnapshotPhaseInProgress indicates the snapshot is being created
	VolumeSnapshotPhaseInProgress VolumeSnapshotPhase = "InProgress"

	// VolumeSnapshotPhaseCompleted indicates the snapshot was created successfully
	VolumeSnapshotPhaseCompleted VolumeSnapshotPhase = "Completed"

	// VolumeSnapshotPhaseFailed indicates the snapshot creation failed
	VolumeSnapshotPhaseFailed VolumeSnapshotPhase = "Failed"
)

// VolumeIdentifier uniquely identifies a volume for snapshot and restore matching
type VolumeIdentifier struct {
	// Repo is the source repository URL (from blueprint)
	// +required
	Repo string `json:"repo"`

	// ServiceName from docker-compose
	// +required
	ServiceName string `json:"serviceName"`

	// Image used by the service (e.g., postgres:15)
	// +required
	Image string `json:"image"`

	// MountPath inside the container (e.g., /var/lib/postgresql/data)
	// +required
	MountPath string `json:"mountPath"`
}

// VolumeSnapshotSpec defines the desired state of VolumeSnapshot
type VolumeSnapshotSpec struct {
	// VolumeIdentifier uniquely identifies the volume for restore matching
	// +required
	VolumeIdentifier VolumeIdentifier `json:"volumeIdentifier"`
}

// VolumeSnapshotStatus defines the observed state of VolumeSnapshot
type VolumeSnapshotStatus struct {
	// Phase represents the current phase of the snapshot
	// +optional
	Phase VolumeSnapshotPhase `json:"phase,omitempty"`

	// Size is the size of the snapshot (e.g., "1Gi")
	// +optional
	Size string `json:"size,omitempty"`

	// StoragePath is the path in object storage where the snapshot is stored
	// Format: {user}/{env}/{repo-hash}/{service}/{mountPath-hash}/{snapshot-id}.tar.gz
	// +optional
	StoragePath string `json:"storagePath,omitempty"`

	// Checksum is the SHA256 checksum of the snapshot archive
	// +optional
	Checksum string `json:"checksum,omitempty"`

	// StartTime is when the snapshot started
	// +optional
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompletionTime is when the snapshot completed
	// +optional
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`

	// Message provides additional information about the current phase
	// +optional
	Message string `json:"message,omitempty"`

	// JobName is the name of the Job that created this snapshot
	// +optional
	JobName string `json:"jobName,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Service",type=string,JSONPath=`.spec.volumeIdentifier.serviceName`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Size",type=string,JSONPath=`.status.size`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// VolumeSnapshot is the Schema for the volumesnapshots API.
// It represents a point-in-time snapshot of a volume stored in object storage.
type VolumeSnapshot struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of VolumeSnapshot
	// +required
	Spec VolumeSnapshotSpec `json:"spec"`

	// status defines the observed state of VolumeSnapshot
	// +optional
	Status VolumeSnapshotStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VolumeSnapshotList contains a list of VolumeSnapshot
type VolumeSnapshotList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VolumeSnapshot `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VolumeSnapshot{}, &VolumeSnapshotList{})
}
