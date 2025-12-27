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
	"testing"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

func TestStripImageTag(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		expected string
	}{
		{
			name:     "simple image with tag",
			image:    "postgres:15",
			expected: "postgres",
		},
		{
			name:     "image with version tag",
			image:    "postgres:15.2",
			expected: "postgres",
		},
		{
			name:     "image without tag",
			image:    "postgres",
			expected: "postgres",
		},
		{
			name:     "registry with port and tag",
			image:    "registry.io:5000/org/postgres:15",
			expected: "registry.io:5000/org/postgres",
		},
		{
			name:     "full registry path with tag",
			image:    "registry.io/org/postgres:15",
			expected: "registry.io/org/postgres",
		},
		{
			name:     "image with digest",
			image:    "postgres@sha256:abc123",
			expected: "postgres",
		},
		{
			name:     "full path with digest",
			image:    "registry.io/org/postgres@sha256:abc123",
			expected: "registry.io/org/postgres",
		},
		{
			name:     "ghcr.io image",
			image:    "ghcr.io/lissto-dev/app:v1.0.0",
			expected: "ghcr.io/lissto-dev/app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripImageTag(tt.image)
			if result != tt.expected {
				t.Errorf("stripImageTag(%q) = %q, want %q", tt.image, result, tt.expected)
			}
		})
	}
}

func TestGenerateKey(t *testing.T) {
	id := envv1alpha1.VolumeIdentifier{
		Repo:        "github.com/org/repo",
		ServiceName: "postgres",
		Image:       "postgres:15",
		MountPath:   "/var/lib/postgresql/data",
	}

	key := GenerateKey(id)
	expected := "github.com/org/repo:postgres:postgres:/var/lib/postgresql/data"

	if key != expected {
		t.Errorf("GenerateKey() = %q, want %q", key, expected)
	}
}

func TestGenerateStoragePath(t *testing.T) {
	id := envv1alpha1.VolumeIdentifier{
		Repo:        "github.com/org/repo",
		ServiceName: "postgres",
		Image:       "postgres:15",
		MountPath:   "/var/lib/postgresql/data",
	}

	path := GenerateStoragePath("user1", "dev", id, "vs-postgres-12345")

	// Check that path contains expected components
	if len(path) == 0 {
		t.Error("GenerateStoragePath() returned empty string")
	}

	// Path should contain user, env, service, and snapshot name
	if !contains(path, "user1") {
		t.Errorf("path %q should contain user1", path)
	}
	if !contains(path, "dev") {
		t.Errorf("path %q should contain dev", path)
	}
	if !contains(path, "postgres") {
		t.Errorf("path %q should contain postgres", path)
	}
	if !contains(path, "vs-postgres-12345.tar.gz") {
		t.Errorf("path %q should contain vs-postgres-12345.tar.gz", path)
	}
}

func TestMatchesIdentifier(t *testing.T) {
	base := envv1alpha1.VolumeIdentifier{
		Repo:        "github.com/org/repo",
		ServiceName: "postgres",
		Image:       "postgres:15",
		MountPath:   "/var/lib/postgresql/data",
	}

	tests := []struct {
		name     string
		a        envv1alpha1.VolumeIdentifier
		b        envv1alpha1.VolumeIdentifier
		expected bool
	}{
		{
			name:     "identical",
			a:        base,
			b:        base,
			expected: true,
		},
		{
			name: "different image tag",
			a:    base,
			b: envv1alpha1.VolumeIdentifier{
				Repo:        "github.com/org/repo",
				ServiceName: "postgres",
				Image:       "postgres:16", // Different tag
				MountPath:   "/var/lib/postgresql/data",
			},
			expected: true, // Should match - tags are stripped
		},
		{
			name: "different repo",
			a:    base,
			b: envv1alpha1.VolumeIdentifier{
				Repo:        "github.com/other/repo",
				ServiceName: "postgres",
				Image:       "postgres:15",
				MountPath:   "/var/lib/postgresql/data",
			},
			expected: false,
		},
		{
			name: "different service",
			a:    base,
			b: envv1alpha1.VolumeIdentifier{
				Repo:        "github.com/org/repo",
				ServiceName: "mysql",
				Image:       "postgres:15",
				MountPath:   "/var/lib/postgresql/data",
			},
			expected: false,
		},
		{
			name: "different mount path",
			a:    base,
			b: envv1alpha1.VolumeIdentifier{
				Repo:        "github.com/org/repo",
				ServiceName: "postgres",
				Image:       "postgres:15",
				MountPath:   "/data",
			},
			expected: false,
		},
		{
			name: "different image name",
			a:    base,
			b: envv1alpha1.VolumeIdentifier{
				Repo:        "github.com/org/repo",
				ServiceName: "postgres",
				Image:       "mysql:8", // Different image
				MountPath:   "/var/lib/postgresql/data",
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MatchesIdentifier(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("MatchesIdentifier() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestShortHash(t *testing.T) {
	hash1 := shortHash("github.com/org/repo")
	hash2 := shortHash("github.com/org/repo")
	hash3 := shortHash("github.com/other/repo")

	// Same input should produce same hash
	if hash1 != hash2 {
		t.Errorf("shortHash() not deterministic: %q != %q", hash1, hash2)
	}

	// Different input should produce different hash
	if hash1 == hash3 {
		t.Errorf("shortHash() collision: %q == %q", hash1, hash3)
	}

	// Hash should be 8 characters
	if len(hash1) != HashLength {
		t.Errorf("shortHash() length = %d, want %d", len(hash1), HashLength)
	}
}

func TestGenerateSnapshotName(t *testing.T) {
	name := GenerateSnapshotName("postgres", "12345")
	expected := "vs-postgres-12345"

	if name != expected {
		t.Errorf("GenerateSnapshotName() = %q, want %q", name, expected)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
