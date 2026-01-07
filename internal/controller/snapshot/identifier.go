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
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

const (
	// ObjectStorageSecretName is the conventional name for the object storage credentials secret
	ObjectStorageSecretName = "lissto-object-storage"

	// ObjectStorageSecretNamespace is the conventional namespace for the object storage credentials secret
	ObjectStorageSecretNamespace = "lissto-system"

	// SnapshotAgentImage is the conventional image for the snapshot agent
	SnapshotAgentImage = "ghcr.io/lissto-dev/snapshot-agent:v1"

	// HashLength is the length of truncated hashes used in paths
	HashLength = 8
)

// GenerateKey creates a unique identifier key for restore matching
// Format: repo:serviceName:imageName:mountPath
// Note: image tag is stripped to allow version upgrades
func GenerateKey(id envv1alpha1.VolumeIdentifier) string {
	imageName := stripImageTag(id.Image)
	return fmt.Sprintf("%s:%s:%s:%s", id.Repo, id.ServiceName, imageName, id.MountPath)
}

// GenerateStoragePath creates the object storage path for a snapshot
// Format: {user}/{env}/{repo-hash}/{service}/{mountPath-hash}/{snapshot-name}.tar.gz
func GenerateStoragePath(user, env string, id envv1alpha1.VolumeIdentifier, snapshotName string) string {
	repoHash := shortHash(id.Repo)
	mountPathHash := shortHash(id.MountPath)
	return fmt.Sprintf("%s/%s/%s/%s/%s/%s.tar.gz",
		user, env, repoHash, id.ServiceName, mountPathHash, snapshotName)
}

// MatchesIdentifier checks if two volume identifiers match for restore purposes
// Matching is based on: repo + serviceName + imageName (without tag) + mountPath
func MatchesIdentifier(a, b envv1alpha1.VolumeIdentifier) bool {
	return a.Repo == b.Repo &&
		a.ServiceName == b.ServiceName &&
		stripImageTag(a.Image) == stripImageTag(b.Image) &&
		a.MountPath == b.MountPath
}

// stripImageTag removes the tag from an image reference
// Examples:
//   - "postgres:15" -> "postgres"
//   - "postgres:15.2" -> "postgres"
//   - "registry.io/org/postgres:15" -> "registry.io/org/postgres"
//   - "postgres" -> "postgres"
func stripImageTag(image string) string {
	// Handle digest references (image@sha256:...)
	if idx := strings.LastIndex(image, "@"); idx != -1 {
		return image[:idx]
	}

	// Handle tag references (image:tag)
	// Be careful with registry ports (registry.io:5000/image:tag)
	lastSlash := strings.LastIndex(image, "/")
	if lastSlash == -1 {
		// No slash, simple image:tag format
		if idx := strings.LastIndex(image, ":"); idx != -1 {
			return image[:idx]
		}
		return image
	}

	// Has slash, check for tag after the last slash
	afterSlash := image[lastSlash+1:]
	if idx := strings.LastIndex(afterSlash, ":"); idx != -1 {
		return image[:lastSlash+1+idx]
	}
	return image
}

// shortHash creates a truncated SHA256 hash of the input
func shortHash(input string) string {
	hash := sha256.Sum256([]byte(input))
	return hex.EncodeToString(hash[:])[:HashLength]
}

// GenerateSnapshotName creates a unique name for a VolumeSnapshot
// Format: vs-{service}-{random-suffix}
func GenerateSnapshotName(serviceName string, suffix string) string {
	return fmt.Sprintf("vs-%s-%s", serviceName, suffix)
}
