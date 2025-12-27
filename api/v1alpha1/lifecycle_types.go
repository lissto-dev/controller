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

// LifecycleSpec defines the desired state of Lifecycle
type LifecycleSpec struct {
	// TargetKind specifies the kind of objects to manage (e.g., "Stack", "Blueprint", "VolumeSnapshot")
	// +required
	// +kubebuilder:validation:Enum=Stack;Blueprint;VolumeSnapshot
	TargetKind string `json:"targetKind"`

	// LabelSelector optionally filters objects by labels.
	// If not provided, applies to all objects of TargetKind across the cluster.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`

	// Interval specifies how often to run the lifecycle tasks.
	// Examples: "5m", "1h", "24h"
	// +required
	Interval metav1.Duration `json:"interval"`

	// Tasks is the list of lifecycle tasks to execute on matching objects.
	// +required
	// +kubebuilder:validation:MinItems=1
	Tasks []LifecycleTask `json:"tasks"`
}

// LifecycleTask defines a single lifecycle operation
type LifecycleTask struct {
	// Name is an optional identifier for the task (used in events and logging)
	// +optional
	Name string `json:"name,omitempty"`

	// Delete removes objects older than specified duration
	// +optional
	Delete *DeleteTask `json:"delete,omitempty"`

	// ScaleDown scales deployments/statefulsets to 0 replicas
	// Used before snapshot to release volume locks
	// +optional
	ScaleDown *ScaleDownTask `json:"scaleDown,omitempty"`

	// ScaleUp restores deployments/statefulsets to their original replica counts
	// Used after snapshot to resume workloads
	// +optional
	ScaleUp *ScaleUpTask `json:"scaleUp,omitempty"`

	// Snapshot creates volume snapshots for matching stacks
	// Uploads volume data to object storage
	// +optional
	Snapshot *SnapshotTask `json:"snapshot,omitempty"`
}

// DeleteTask configures automatic deletion of objects based on age
type DeleteTask struct {
	// OlderThan specifies the minimum age for deletion.
	// Objects created more than this duration ago will be deleted.
	// Examples: "30m", "2h", "24h", "7d"
	// +required
	OlderThan metav1.Duration `json:"olderThan"`
}

// ScaleDownTask configures scaling down of workloads
type ScaleDownTask struct {
	// Timeout specifies how long to wait for pods to terminate
	// Default: 5m
	// +optional
	Timeout metav1.Duration `json:"timeout,omitempty"`
}

// ScaleUpTask configures scaling up of workloads to original replicas
type ScaleUpTask struct {
	// Empty struct - restores original replica counts stored during scale down
}

// SnapshotTask configures volume snapshot creation
type SnapshotTask struct {
	// Empty struct - uses controller config and conventions
	// - Storage config from controller config
	// - Credentials from lissto-object-storage secret
	// - Path: {user}/{env}/{repo-hash}/{service}/{mountPath-hash}/{id}.tar.gz
	// - Compression: always gzip
}

// LifecycleStatus defines the observed state of Lifecycle
type LifecycleStatus struct {
	// Conditions represent the current state of the Lifecycle resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// LastRunTime is the timestamp of the last task execution
	// +optional
	LastRunTime *metav1.Time `json:"lastRunTime,omitempty"`

	// NextRunTime is the scheduled time for the next task execution
	// +optional
	NextRunTime *metav1.Time `json:"nextRunTime,omitempty"`

	// SuccessfulTasks is the total count of successfully executed tasks
	// +optional
	SuccessfulTasks int64 `json:"successfulTasks"`

	// FailedTasks is the total count of failed task executions
	// +optional
	FailedTasks int64 `json:"failedTasks"`

	// LastRunStats contains statistics from the most recent run
	// +optional
	LastRunStats *RunStats `json:"lastRunStats,omitempty"`
}

// RunStats contains statistics from a single lifecycle run
type RunStats struct {
	// ObjectsEvaluated is the number of objects that matched the selector
	// +optional
	ObjectsEvaluated int64 `json:"objectsEvaluated"`

	// ObjectsDeleted is the number of objects deleted in this run
	// +optional
	ObjectsDeleted int64 `json:"objectsDeleted"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster
// +kubebuilder:printcolumn:name="Target",type=string,JSONPath=`.spec.targetKind`
// +kubebuilder:printcolumn:name="Interval",type=string,JSONPath=`.spec.interval`
// +kubebuilder:printcolumn:name="Last Run",type=date,JSONPath=`.status.lastRunTime`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Lifecycle is the Schema for the lifecycles API.
// It manages the lifecycle of Lissto objects (Stacks, Blueprints) by executing
// configured tasks at specified intervals.
type Lifecycle struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Lifecycle
	// +required
	Spec LifecycleSpec `json:"spec"`

	// status defines the observed state of Lifecycle
	// +optional
	Status LifecycleStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// LifecycleList contains a list of Lifecycle
type LifecycleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Lifecycle `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Lifecycle{}, &LifecycleList{})
}
