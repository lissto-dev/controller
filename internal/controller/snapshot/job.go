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

package snapshot

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

const (
	// SnapshotJobLabelKey is the label key for identifying snapshot jobs
	SnapshotJobLabelKey = "env.lissto.dev/snapshot-job"

	// RestoreJobLabelKey is the label key for identifying restore jobs
	RestoreJobLabelKey = "env.lissto.dev/restore-job"

	// VolumeSnapshotRefLabel is the label key for referencing the VolumeSnapshot
	VolumeSnapshotRefLabel = "env.lissto.dev/volume-snapshot"

	// DataVolumeName is the name of the volume mount for the PVC data
	DataVolumeName = "data"

	// DataMountPath is the path where the PVC is mounted in the snapshot/restore job
	DataMountPath = "/data"

	// CredentialsVolumeName is the name of the volume mount for storage credentials
	CredentialsVolumeName = "credentials"

	// CredentialsMountPath is the path where credentials are mounted
	CredentialsMountPath = "/credentials"
)

// SnapshotJobConfig holds configuration for creating a snapshot job
type SnapshotJobConfig struct {
	// Name is the name of the job
	Name string

	// Namespace is the namespace for the job
	Namespace string

	// PVCName is the name of the PVC to snapshot
	PVCName string

	// StoragePath is the destination path in object storage
	StoragePath string

	// VolumeSnapshotName is the name of the VolumeSnapshot CR
	VolumeSnapshotName string

	// Labels to apply to the job
	Labels map[string]string
}

// RestoreJobConfig holds configuration for creating a restore job
type RestoreJobConfig struct {
	// Name is the name of the job
	Name string

	// Namespace is the namespace for the job
	Namespace string

	// PVCName is the name of the PVC to restore to
	PVCName string

	// StoragePath is the source path in object storage
	StoragePath string

	// VolumeSnapshotName is the name of the VolumeSnapshot CR being restored
	VolumeSnapshotName string

	// Labels to apply to the job
	Labels map[string]string
}

// BuildSnapshotJob creates a Kubernetes Job spec for snapshotting a PVC to object storage
func BuildSnapshotJob(config SnapshotJobConfig) *batchv1.Job {
	backoffLimit := int32(3)
	ttlSeconds := int32(3600) // Clean up completed jobs after 1 hour

	labels := map[string]string{
		SnapshotJobLabelKey:    "true",
		VolumeSnapshotRefLabel: config.VolumeSnapshotName,
	}
	for k, v := range config.Labels {
		labels[k] = v
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:  "snapshot",
							Image: SnapshotAgentImage,
							Args: []string{
								"snapshot",
								"--source", DataMountPath,
								"--destination", config.StoragePath,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      DataVolumeName,
									MountPath: DataMountPath,
									ReadOnly:  true,
								},
								{
									Name:      CredentialsVolumeName,
									MountPath: CredentialsMountPath,
									ReadOnly:  true,
								},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: ObjectStorageSecretName,
										},
									},
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: DataVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: config.PVCName,
									ReadOnly:  true,
								},
							},
						},
						{
							Name: CredentialsVolumeName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: ObjectStorageSecretName,
								},
							},
						},
					},
				},
			},
		},
	}
}

// BuildRestoreJob creates a Kubernetes Job spec for restoring a PVC from object storage
func BuildRestoreJob(config RestoreJobConfig) *batchv1.Job {
	backoffLimit := int32(3)
	ttlSeconds := int32(3600) // Clean up completed jobs after 1 hour

	labels := map[string]string{
		RestoreJobLabelKey:     "true",
		VolumeSnapshotRefLabel: config.VolumeSnapshotName,
	}
	for k, v := range config.Labels {
		labels[k] = v
	}

	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: config.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoffLimit,
			TTLSecondsAfterFinished: &ttlSeconds,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyOnFailure,
					Containers: []corev1.Container{
						{
							Name:  "restore",
							Image: SnapshotAgentImage,
							Args: []string{
								"restore",
								"--source", config.StoragePath,
								"--destination", DataMountPath,
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      DataVolumeName,
									MountPath: DataMountPath,
									ReadOnly:  false, // Need write access for restore
								},
								{
									Name:      CredentialsVolumeName,
									MountPath: CredentialsMountPath,
									ReadOnly:  true,
								},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: ObjectStorageSecretName,
										},
									},
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: DataVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: config.PVCName,
									ReadOnly:  false, // Need write access for restore
								},
							},
						},
						{
							Name: CredentialsVolumeName,
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: ObjectStorageSecretName,
								},
							},
						},
					},
				},
			},
		},
	}
}

// GenerateSnapshotJobName creates a job name for a snapshot operation
func GenerateSnapshotJobName(volumeSnapshotName string) string {
	return fmt.Sprintf("snapshot-%s", volumeSnapshotName)
}

// GenerateRestoreJobName creates a job name for a restore operation
func GenerateRestoreJobName(volumeSnapshotName, suffix string) string {
	return fmt.Sprintf("restore-%s-%s", volumeSnapshotName, suffix)
}

// GetVolumeSnapshotLabels returns standard labels for a VolumeSnapshot
func GetVolumeSnapshotLabels(user, env, service string, id envv1alpha1.VolumeIdentifier) map[string]string {
	return map[string]string{
		envv1alpha1.VolumeSnapshotLabelUser:     user,
		envv1alpha1.VolumeSnapshotLabelEnv:      env,
		envv1alpha1.VolumeSnapshotLabelService:  service,
		envv1alpha1.VolumeSnapshotLabelRepoHash: shortHash(id.Repo),
	}
}
