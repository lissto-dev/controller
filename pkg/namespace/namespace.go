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

// Package namespace provides centralized namespace resolution and scoped ID handling
// for Lissto resources. This package is shared across the controller, API, and CLI.
package namespace

import (
	"fmt"
	"strings"
)

// Manager handles namespace resolution and scoped ID operations.
// It encapsulates the namespace configuration and provides methods for:
// - Converting between k8s namespaces and user-facing scopes
// - Parsing and generating scoped IDs (e.g., "daniel/blueprint-123")
// - Validating namespace access
type Manager struct {
	globalNamespace string
	developerPrefix string
}

// NewManager creates a new namespace manager with the given configuration.
// globalNamespace is the k8s namespace for global resources (e.g., "lissto-global")
// developerPrefix is the prefix for developer namespaces (e.g., "lissto-" or "dev-")
func NewManager(globalNamespace, developerPrefix string) *Manager {
	return &Manager{
		globalNamespace: globalNamespace,
		developerPrefix: developerPrefix,
	}
}

// GetGlobalNamespace returns the k8s namespace for global resources.
func (m *Manager) GetGlobalNamespace() string {
	return m.globalNamespace
}

// GetDeveloperPrefix returns the prefix used for developer namespaces.
func (m *Manager) GetDeveloperPrefix() string {
	return m.developerPrefix
}

// GetDeveloperNamespace returns the k8s namespace for a developer.
// Example: GetDeveloperNamespace("daniel") -> "lissto-daniel" (if prefix is "lissto-")
func (m *Manager) GetDeveloperNamespace(username string) string {
	return m.developerPrefix + username
}

// IsGlobalNamespace checks if a k8s namespace is the global namespace.
func (m *Manager) IsGlobalNamespace(namespace string) bool {
	return namespace == m.globalNamespace
}

// IsDeveloperNamespace checks if a k8s namespace is a developer namespace.
func (m *Manager) IsDeveloperNamespace(namespace string) bool {
	return strings.HasPrefix(namespace, m.developerPrefix) && namespace != m.globalNamespace
}

// GetOwnerFromNamespace extracts the owner username from a developer namespace.
// Returns an error if the namespace is not a developer namespace.
// Example: GetOwnerFromNamespace("lissto-daniel") -> "daniel" (if prefix is "lissto-")
func (m *Manager) GetOwnerFromNamespace(namespace string) (string, error) {
	if !m.IsDeveloperNamespace(namespace) {
		return "", fmt.Errorf("not a developer namespace: %s", namespace)
	}
	return strings.TrimPrefix(namespace, m.developerPrefix), nil
}

// NormalizeToScope converts a k8s namespace to a user-facing scope.
// Global namespace -> "global"
// Developer namespace -> username (e.g., "lissto-daniel" -> "daniel")
// Returns an error for unknown namespace types.
func (m *Manager) NormalizeToScope(namespace string) (string, error) {
	if m.IsGlobalNamespace(namespace) {
		return "global", nil
	}
	if m.IsDeveloperNamespace(namespace) {
		return m.GetOwnerFromNamespace(namespace)
	}
	return "", fmt.Errorf("unknown namespace type: %s", namespace)
}

// GenerateScopedID creates a scoped identifier from a k8s namespace and resource name.
// Example: GenerateScopedID("lissto-global", "bp-123") -> "global/bp-123"
// Example: GenerateScopedID("lissto-daniel", "bp-456") -> "daniel/bp-456"
// Returns an error if the namespace type is unknown.
func (m *Manager) GenerateScopedID(namespace, name string) (string, error) {
	scope, err := m.NormalizeToScope(namespace)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", scope, name), nil
}

// MustGenerateScopedID is like GenerateScopedID but panics on error.
// Use only when the namespace is guaranteed to be valid.
func (m *Manager) MustGenerateScopedID(namespace, name string) string {
	id, err := m.GenerateScopedID(namespace, name)
	if err != nil {
		panic(err)
	}
	return id
}

// ParseScopedID parses a scoped ID into k8s namespace and resource name.
// Supports both scoped format ("scope/name") and legacy format ("name").
// For legacy format, returns empty namespace and the full ID as name.
//
// Examples:
//   - "global/bp-123" -> ("lissto-global", "bp-123", nil)
//   - "daniel/bp-456" -> ("lissto-daniel", "bp-456", nil)
//   - "bp-789" -> ("", "bp-789", nil) // legacy format
func (m *Manager) ParseScopedID(scopedID string) (namespace, name string, err error) {
	idx := strings.IndexByte(scopedID, '/')
	if idx == -1 {
		// Legacy format: just the name
		return "", scopedID, nil
	}

	scope := scopedID[:idx]
	name = scopedID[idx+1:]

	if scope == "" || name == "" {
		return "", "", fmt.Errorf("invalid scoped ID format: %s (both scope and name must be non-empty)", scopedID)
	}

	// Convert scope to k8s namespace
	if scope == "global" {
		namespace = m.globalNamespace
	} else {
		namespace = m.GetDeveloperNamespace(scope)
	}

	return namespace, name, nil
}

// ParseScopedIDWithDefault parses a scoped ID, using defaultNamespace for legacy format.
// This is useful when you want to resolve legacy IDs to a specific namespace.
//
// Examples (with defaultNamespace="lissto-daniel"):
//   - "global/bp-123" -> ("lissto-global", "bp-123", nil)
//   - "daniel/bp-456" -> ("lissto-daniel", "bp-456", nil)
//   - "bp-789" -> ("lissto-daniel", "bp-789", nil) // uses default
func (m *Manager) ParseScopedIDWithDefault(scopedID, defaultNamespace string) (namespace, name string, err error) {
	namespace, name, err = m.ParseScopedID(scopedID)
	if err != nil {
		return "", "", err
	}
	if namespace == "" {
		namespace = defaultNamespace
	}
	return namespace, name, nil
}

// IsNamespaceAllowed checks if a namespace is in the allowed list.
// Supports wildcard "*" which allows all namespaces.
func IsNamespaceAllowed(namespace string, allowedNamespaces []string) bool {
	if len(allowedNamespaces) > 0 && allowedNamespaces[0] == "*" {
		return true
	}
	for _, allowed := range allowedNamespaces {
		if allowed == namespace {
			return true
		}
	}
	return false
}

// ResolveNamespaceFromID parses a scoped ID and validates against allowed namespaces.
// Returns:
//   - targetNamespace: the specific namespace to search (if scoped ID and allowed)
//   - name: the resource name
//   - searchAll: true if should search all allowed namespaces (legacy behavior)
//
// This centralizes the logic for handling both scoped IDs and legacy format.
func (m *Manager) ResolveNamespaceFromID(idParam string, allowedNamespaces []string) (targetNamespace, name string, searchAll bool) {
	// Parse the ID (could be scoped like "daniel/ID" or legacy format "ID")
	parsedNS, parsedName, err := m.ParseScopedID(idParam)
	if err != nil {
		// Invalid format - treat as not found
		return "", idParam, false
	}

	// If scoped reference, check if user can access that namespace
	if parsedNS != "" {
		if IsNamespaceAllowed(parsedNS, allowedNamespaces) {
			// User can access this specific namespace
			return parsedNS, parsedName, false
		}
		// User cannot access this namespace - return empty to trigger 404
		return "", parsedName, false
	}

	// Legacy format: search all allowed namespaces
	return "", parsedName, true
}

// ResolveNamespacesToSearch determines which namespaces to search for a resource.
// Returns an ordered list of namespaces to try (user namespace first, then global if allowed).
//
// Parameters:
//   - targetNS: specific namespace from scoped ID (empty for legacy format)
//   - userNS: the user's developer namespace
//   - globalNS: the global namespace
//   - searchAll: true if should search all allowed namespaces (from ResolveNamespaceFromID)
//   - allowedNS: list of namespaces the user can access
func ResolveNamespacesToSearch(targetNS, userNS, globalNS string, searchAll bool, allowedNS []string) []string {
	// Scoped ID: search only that specific namespace
	if targetNS != "" {
		return []string{targetNS}
	}

	// Not authorized for scoped namespace
	if !searchAll {
		return []string{}
	}

	// Legacy ID: try user namespace first
	namespaces := []string{userNS}

	// Add global if allowed (for read operations)
	if IsNamespaceAllowed(globalNS, allowedNS) {
		namespaces = append(namespaces, globalNS)
	}

	return namespaces
}
