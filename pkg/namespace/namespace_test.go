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

package namespace

import (
	"testing"
)

func TestNewManager(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	if m.GetGlobalNamespace() != "lissto-global" {
		t.Errorf("GetGlobalNamespace() = %q, want %q", m.GetGlobalNamespace(), "lissto-global")
	}
	if m.GetDeveloperPrefix() != "lissto-" {
		t.Errorf("GetDeveloperPrefix() = %q, want %q", m.GetDeveloperPrefix(), "lissto-")
	}
}

func TestGetDeveloperNamespace(t *testing.T) {
	tests := []struct {
		name     string
		prefix   string
		username string
		want     string
	}{
		{"standard prefix", "lissto-", "daniel", "lissto-daniel"},
		{"dev prefix", "dev-", "alice", "dev-alice"},
		{"empty username", "lissto-", "", "lissto-"},
		{"complex username", "ns-", "user.name", "ns-user.name"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager("global", tt.prefix)
			got := m.GetDeveloperNamespace(tt.username)
			if got != tt.want {
				t.Errorf("GetDeveloperNamespace(%q) = %q, want %q", tt.username, got, tt.want)
			}
		})
	}
}

func TestIsGlobalNamespace(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	tests := []struct {
		namespace string
		want      bool
	}{
		{"lissto-global", true},
		{"lissto-daniel", false},
		{"other-namespace", false},
		{"", false},
		{"lissto-global-extra", false},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got := m.IsGlobalNamespace(tt.namespace)
			if got != tt.want {
				t.Errorf("IsGlobalNamespace(%q) = %v, want %v", tt.namespace, got, tt.want)
			}
		})
	}
}

func TestIsDeveloperNamespace(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	tests := []struct {
		namespace string
		want      bool
	}{
		{"lissto-daniel", true},
		{"lissto-alice", true},
		{"lissto-", true}, // edge case: just prefix
		{"lissto-global", false}, // global namespace should not be developer
		{"dev-daniel", false},
		{"other-namespace", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got := m.IsDeveloperNamespace(tt.namespace)
			if got != tt.want {
				t.Errorf("IsDeveloperNamespace(%q) = %v, want %v", tt.namespace, got, tt.want)
			}
		})
	}
}

func TestGetOwnerFromNamespace(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	tests := []struct {
		namespace string
		want      string
		wantErr   bool
	}{
		{"lissto-daniel", "daniel", false},
		{"lissto-alice", "alice", false},
		{"lissto-user.name", "user.name", false},
		{"lissto-", "", false}, // edge case: just prefix
		{"lissto-global", "", true}, // global is not developer
		{"dev-daniel", "", true},
		{"other-namespace", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got, err := m.GetOwnerFromNamespace(tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetOwnerFromNamespace(%q) error = %v, wantErr %v", tt.namespace, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetOwnerFromNamespace(%q) = %q, want %q", tt.namespace, got, tt.want)
			}
		})
	}
}

func TestNormalizeToScope(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	tests := []struct {
		namespace string
		want      string
		wantErr   bool
	}{
		{"lissto-global", "global", false},
		{"lissto-daniel", "daniel", false},
		{"lissto-alice", "alice", false},
		{"other-namespace", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.namespace, func(t *testing.T) {
			got, err := m.NormalizeToScope(tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Errorf("NormalizeToScope(%q) error = %v, wantErr %v", tt.namespace, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("NormalizeToScope(%q) = %q, want %q", tt.namespace, got, tt.want)
			}
		})
	}
}

func TestGenerateScopedID(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	tests := []struct {
		namespace string
		name      string
		want      string
		wantErr   bool
	}{
		{"lissto-global", "bp-123", "global/bp-123", false},
		{"lissto-daniel", "bp-456", "daniel/bp-456", false},
		{"lissto-alice", "stack-789", "alice/stack-789", false},
		{"other-namespace", "bp-123", "", true},
		{"", "bp-123", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.namespace+"/"+tt.name, func(t *testing.T) {
			got, err := m.GenerateScopedID(tt.namespace, tt.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateScopedID(%q, %q) error = %v, wantErr %v", tt.namespace, tt.name, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GenerateScopedID(%q, %q) = %q, want %q", tt.namespace, tt.name, got, tt.want)
			}
		})
	}
}

func TestMustGenerateScopedID(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	// Test valid case
	got := m.MustGenerateScopedID("lissto-global", "bp-123")
	if got != "global/bp-123" {
		t.Errorf("MustGenerateScopedID() = %q, want %q", got, "global/bp-123")
	}

	// Test panic case
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("MustGenerateScopedID() did not panic for invalid namespace")
		}
	}()
	m.MustGenerateScopedID("invalid-namespace", "bp-123")
}

func TestParseScopedID(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	tests := []struct {
		scopedID      string
		wantNamespace string
		wantName      string
		wantErr       bool
	}{
		{"global/bp-123", "lissto-global", "bp-123", false},
		{"daniel/bp-456", "lissto-daniel", "bp-456", false},
		{"alice/stack-789", "lissto-alice", "stack-789", false},
		{"bp-legacy", "", "bp-legacy", false}, // legacy format
		{"just-name", "", "just-name", false}, // legacy format
		{"/bp-123", "", "", true},             // empty scope
		{"daniel/", "", "", true},             // empty name
		{"/", "", "", true},                   // both empty
	}

	for _, tt := range tests {
		t.Run(tt.scopedID, func(t *testing.T) {
			gotNS, gotName, err := m.ParseScopedID(tt.scopedID)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseScopedID(%q) error = %v, wantErr %v", tt.scopedID, err, tt.wantErr)
				return
			}
			if gotNS != tt.wantNamespace {
				t.Errorf("ParseScopedID(%q) namespace = %q, want %q", tt.scopedID, gotNS, tt.wantNamespace)
			}
			if gotName != tt.wantName {
				t.Errorf("ParseScopedID(%q) name = %q, want %q", tt.scopedID, gotName, tt.wantName)
			}
		})
	}
}

func TestParseScopedIDWithDefault(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")
	defaultNS := "lissto-daniel"

	tests := []struct {
		scopedID      string
		wantNamespace string
		wantName      string
		wantErr       bool
	}{
		{"global/bp-123", "lissto-global", "bp-123", false},
		{"alice/bp-456", "lissto-alice", "bp-456", false},
		{"bp-legacy", "lissto-daniel", "bp-legacy", false}, // uses default
		{"just-name", "lissto-daniel", "just-name", false}, // uses default
		{"/bp-123", "", "", true},                          // invalid
	}

	for _, tt := range tests {
		t.Run(tt.scopedID, func(t *testing.T) {
			gotNS, gotName, err := m.ParseScopedIDWithDefault(tt.scopedID, defaultNS)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseScopedIDWithDefault(%q) error = %v, wantErr %v", tt.scopedID, err, tt.wantErr)
				return
			}
			if gotNS != tt.wantNamespace {
				t.Errorf("ParseScopedIDWithDefault(%q) namespace = %q, want %q", tt.scopedID, gotNS, tt.wantNamespace)
			}
			if gotName != tt.wantName {
				t.Errorf("ParseScopedIDWithDefault(%q) name = %q, want %q", tt.scopedID, gotName, tt.wantName)
			}
		})
	}
}

func TestIsNamespaceAllowed(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		allowed   []string
		want      bool
	}{
		{"exact match", "lissto-daniel", []string{"lissto-daniel", "lissto-global"}, true},
		{"no match", "lissto-alice", []string{"lissto-daniel", "lissto-global"}, false},
		{"wildcard", "any-namespace", []string{"*"}, true},
		{"empty allowed", "lissto-daniel", []string{}, false},
		{"empty namespace", "", []string{"lissto-daniel"}, false},
		{"wildcard not first", "any-namespace", []string{"lissto-daniel", "*"}, false}, // wildcard only works as first element
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsNamespaceAllowed(tt.namespace, tt.allowed)
			if got != tt.want {
				t.Errorf("IsNamespaceAllowed(%q, %v) = %v, want %v", tt.namespace, tt.allowed, got, tt.want)
			}
		})
	}
}

func TestResolveNamespaceFromID(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")
	allowedNS := []string{"lissto-daniel", "lissto-global"}

	tests := []struct {
		name          string
		idParam       string
		wantTargetNS  string
		wantName      string
		wantSearchAll bool
	}{
		{"scoped global allowed", "global/bp-123", "lissto-global", "bp-123", false},
		{"scoped user allowed", "daniel/bp-456", "lissto-daniel", "bp-456", false},
		{"scoped user not allowed", "alice/bp-789", "", "bp-789", false},
		{"legacy format", "bp-legacy", "", "bp-legacy", true},
		{"invalid format", "/bp-123", "", "/bp-123", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTargetNS, gotName, gotSearchAll := m.ResolveNamespaceFromID(tt.idParam, allowedNS)
			if gotTargetNS != tt.wantTargetNS {
				t.Errorf("ResolveNamespaceFromID(%q) targetNS = %q, want %q", tt.idParam, gotTargetNS, tt.wantTargetNS)
			}
			if gotName != tt.wantName {
				t.Errorf("ResolveNamespaceFromID(%q) name = %q, want %q", tt.idParam, gotName, tt.wantName)
			}
			if gotSearchAll != tt.wantSearchAll {
				t.Errorf("ResolveNamespaceFromID(%q) searchAll = %v, want %v", tt.idParam, gotSearchAll, tt.wantSearchAll)
			}
		})
	}
}

func TestResolveNamespaceFromIDWithWildcard(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")
	allowedNS := []string{"*"} // admin access

	tests := []struct {
		name          string
		idParam       string
		wantTargetNS  string
		wantName      string
		wantSearchAll bool
	}{
		{"scoped any user", "alice/bp-123", "lissto-alice", "bp-123", false},
		{"scoped global", "global/bp-456", "lissto-global", "bp-456", false},
		{"legacy format", "bp-legacy", "", "bp-legacy", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTargetNS, gotName, gotSearchAll := m.ResolveNamespaceFromID(tt.idParam, allowedNS)
			if gotTargetNS != tt.wantTargetNS {
				t.Errorf("ResolveNamespaceFromID(%q) targetNS = %q, want %q", tt.idParam, gotTargetNS, tt.wantTargetNS)
			}
			if gotName != tt.wantName {
				t.Errorf("ResolveNamespaceFromID(%q) name = %q, want %q", tt.idParam, gotName, tt.wantName)
			}
			if gotSearchAll != tt.wantSearchAll {
				t.Errorf("ResolveNamespaceFromID(%q) searchAll = %v, want %v", tt.idParam, gotSearchAll, tt.wantSearchAll)
			}
		})
	}
}

func TestResolveNamespacesToSearch(t *testing.T) {
	userNS := "lissto-daniel"
	globalNS := "lissto-global"

	tests := []struct {
		name      string
		targetNS  string
		searchAll bool
		allowedNS []string
		want      []string
	}{
		{
			"scoped ID - specific namespace",
			"lissto-alice",
			false,
			[]string{"lissto-daniel", "lissto-global"},
			[]string{"lissto-alice"},
		},
		{
			"legacy ID - user and global allowed",
			"",
			true,
			[]string{"lissto-daniel", "lissto-global"},
			[]string{"lissto-daniel", "lissto-global"},
		},
		{
			"legacy ID - only user allowed",
			"",
			true,
			[]string{"lissto-daniel"},
			[]string{"lissto-daniel"},
		},
		{
			"not authorized",
			"",
			false,
			[]string{"lissto-daniel", "lissto-global"},
			[]string{},
		},
		{
			"wildcard allowed",
			"",
			true,
			[]string{"*"},
			[]string{"lissto-daniel", "lissto-global"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveNamespacesToSearch(tt.targetNS, userNS, globalNS, tt.searchAll, tt.allowedNS)
			if len(got) != len(tt.want) {
				t.Errorf("ResolveNamespacesToSearch() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ResolveNamespacesToSearch()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestRoundTrip(t *testing.T) {
	m := NewManager("lissto-global", "lissto-")

	// Test that GenerateScopedID and ParseScopedID are inverses
	testCases := []struct {
		namespace string
		name      string
	}{
		{"lissto-global", "blueprint-123"},
		{"lissto-daniel", "stack-456"},
		{"lissto-alice", "env-789"},
	}

	for _, tc := range testCases {
		t.Run(tc.namespace+"/"+tc.name, func(t *testing.T) {
			// Generate scoped ID
			scopedID, err := m.GenerateScopedID(tc.namespace, tc.name)
			if err != nil {
				t.Fatalf("GenerateScopedID() error = %v", err)
			}

			// Parse it back
			gotNS, gotName, err := m.ParseScopedID(scopedID)
			if err != nil {
				t.Fatalf("ParseScopedID() error = %v", err)
			}

			if gotNS != tc.namespace {
				t.Errorf("Round trip namespace = %q, want %q", gotNS, tc.namespace)
			}
			if gotName != tc.name {
				t.Errorf("Round trip name = %q, want %q", gotName, tc.name)
			}
		})
	}
}

func TestDifferentPrefixes(t *testing.T) {
	// Test with different prefix configurations
	configs := []struct {
		globalNS string
		prefix   string
	}{
		{"lissto-global", "lissto-"},
		{"global", "dev-"},
		{"prod-global", "prod-"},
	}

	for _, cfg := range configs {
		t.Run(cfg.prefix, func(t *testing.T) {
			m := NewManager(cfg.globalNS, cfg.prefix)

			// Test developer namespace
			devNS := m.GetDeveloperNamespace("testuser")
			if devNS != cfg.prefix+"testuser" {
				t.Errorf("GetDeveloperNamespace() = %q, want %q", devNS, cfg.prefix+"testuser")
			}

			// Test round trip
			scopedID, err := m.GenerateScopedID(devNS, "resource-123")
			if err != nil {
				t.Fatalf("GenerateScopedID() error = %v", err)
			}

			gotNS, gotName, err := m.ParseScopedID(scopedID)
			if err != nil {
				t.Fatalf("ParseScopedID() error = %v", err)
			}

			if gotNS != devNS {
				t.Errorf("Round trip namespace = %q, want %q", gotNS, devNS)
			}
			if gotName != "resource-123" {
				t.Errorf("Round trip name = %q, want %q", gotName, "resource-123")
			}
		})
	}
}
