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
	"strings"
	"testing"
)

func TestBuildSnapshotJob(t *testing.T) {
	config := SnapshotJobConfig{
		Name:               "snapshot-job-123",
		Namespace:          "test-namespace",
		PVCName:            "postgres-data",
		StoragePath:        "user/dev/abc123/postgres/def456/vs-123.tar.gz",
		VolumeSnapshotName: "vs-postgres-123",
		Labels: map[string]string{
			"env.lissto.dev/stack": "test-stack",
		},
	}

	job := BuildSnapshotJob(config)

	// Verify job metadata
	if job.Name != "snapshot-job-123" {
		t.Errorf("job.Name = %q, want %q", job.Name, "snapshot-job-123")
	}
	if job.Namespace != "test-namespace" {
		t.Errorf("job.Namespace = %q, want %q", job.Namespace, "test-namespace")
	}

	// Verify labels
	if job.Labels["env.lissto.dev/stack"] != "test-stack" {
		t.Errorf("job.Labels[env.lissto.dev/stack] = %q, want %q", job.Labels["env.lissto.dev/stack"], "test-stack")
	}
	if job.Labels[SnapshotJobLabelKey] != "true" {
		t.Errorf("job.Labels[%s] = %q, want %q", SnapshotJobLabelKey, job.Labels[SnapshotJobLabelKey], "true")
	}

	// Verify spec
	if *job.Spec.BackoffLimit != int32(3) {
		t.Errorf("job.Spec.BackoffLimit = %d, want %d", *job.Spec.BackoffLimit, 3)
	}

	// Verify pod template
	podSpec := job.Spec.Template.Spec
	if len(podSpec.Containers) != 1 {
		t.Fatalf("len(podSpec.Containers) = %d, want 1", len(podSpec.Containers))
	}

	container := podSpec.Containers[0]
	if container.Name != "snapshot" {
		t.Errorf("container.Name = %q, want %q", container.Name, "snapshot")
	}
	if container.Image != SnapshotAgentImage {
		t.Errorf("container.Image = %q, want %q", container.Image, SnapshotAgentImage)
	}

	// Verify args contain the storage path and source
	argsStr := ""
	for _, arg := range container.Args {
		argsStr += arg + " "
	}
	if !containsStr(argsStr, "snapshot") {
		t.Errorf("Args should contain 'snapshot' command, got: %v", container.Args)
	}
	if !containsStr(argsStr, config.StoragePath) {
		t.Errorf("Args should contain storage path %q, got: %v", config.StoragePath, container.Args)
	}
	if !containsStr(argsStr, DataMountPath) {
		t.Errorf("Args should contain data mount path %q, got: %v", DataMountPath, container.Args)
	}

	// Verify volumes
	if len(podSpec.Volumes) != 2 {
		t.Fatalf("len(podSpec.Volumes) = %d, want 2", len(podSpec.Volumes))
	}

	// Verify volume mounts
	if len(container.VolumeMounts) != 2 {
		t.Fatalf("len(container.VolumeMounts) = %d, want 2", len(container.VolumeMounts))
	}

	// Verify PVC volume
	foundPVC := false
	for _, vol := range podSpec.Volumes {
		if vol.PersistentVolumeClaim != nil && vol.PersistentVolumeClaim.ClaimName == "postgres-data" {
			foundPVC = true
			break
		}
	}
	if !foundPVC {
		t.Error("Job should have a volume referencing the PVC")
	}
}

func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}

func TestBuildRestoreJob(t *testing.T) {
	config := RestoreJobConfig{
		Name:               "restore-job-123",
		Namespace:          "test-namespace",
		PVCName:            "postgres-data",
		StoragePath:        "user/dev/abc123/postgres/def456/vs-123.tar.gz",
		VolumeSnapshotName: "vs-postgres-123",
		Labels: map[string]string{
			"env.lissto.dev/stack": "test-stack",
		},
	}

	job := BuildRestoreJob(config)

	// Verify job metadata
	if job.Name != "restore-job-123" {
		t.Errorf("job.Name = %q, want %q", job.Name, "restore-job-123")
	}
	if job.Namespace != "test-namespace" {
		t.Errorf("job.Namespace = %q, want %q", job.Namespace, "test-namespace")
	}

	// Verify labels
	if job.Labels[RestoreJobLabelKey] != "true" {
		t.Errorf("job.Labels[%s] = %q, want %q", RestoreJobLabelKey, job.Labels[RestoreJobLabelKey], "true")
	}

	// Verify pod template
	podSpec := job.Spec.Template.Spec
	container := podSpec.Containers[0]

	// Verify args contain restore command
	argsStr := ""
	for _, arg := range container.Args {
		argsStr += arg + " "
	}
	if !containsStr(argsStr, "restore") {
		t.Errorf("Args should contain 'restore' command, got: %v", container.Args)
	}
	if !containsStr(argsStr, config.StoragePath) {
		t.Errorf("Args should contain storage path %q, got: %v", config.StoragePath, container.Args)
	}

	// Verify PVC is mounted writable (not read-only)
	foundWritableMount := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == DataVolumeName && !mount.ReadOnly {
			foundWritableMount = true
			break
		}
	}
	if !foundWritableMount {
		t.Error("Restore job should have writable mount for data volume")
	}
}

func TestGenerateSnapshotJobName(t *testing.T) {
	name := GenerateSnapshotJobName("vs-postgres-123")
	expected := "snapshot-vs-postgres-123"

	if name != expected {
		t.Errorf("GenerateSnapshotJobName() = %q, want %q", name, expected)
	}
}

func TestGenerateRestoreJobName(t *testing.T) {
	name := GenerateRestoreJobName("vs-postgres-123", "my-stack")
	expected := "restore-vs-postgres-123-my-stack"

	if name != expected {
		t.Errorf("GenerateRestoreJobName() = %q, want %q", name, expected)
	}
}

func TestGenerateRestoreJobName_LongNames(t *testing.T) {
	// Create a very long snapshot name
	longSnapshotName := "vs-postgres-very-long-snapshot-name-that-exceeds-kubernetes-limits"
	longStackName := "my-very-long-stack-name-that-also-exceeds-limits"

	name := GenerateRestoreJobName(longSnapshotName, longStackName)

	// Verify the name is generated (even if long)
	// Note: Kubernetes will reject names > 63 chars, but the generator
	// currently doesn't truncate. This test documents current behavior.
	if name == "" {
		t.Error("GenerateRestoreJobName() should not return empty string")
	}

	// Verify it contains expected prefix
	if !containsStr(name, "restore-") {
		t.Errorf("GenerateRestoreJobName() should start with 'restore-', got: %q", name)
	}
}
