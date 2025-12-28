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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Namespace Manager", func() {
	Describe("NewManager", func() {
		It("should create a manager with correct configuration", func() {
			m := NewManager("lissto-global", "lissto-")

			Expect(m.GetGlobalNamespace()).To(Equal("lissto-global"))
			Expect(m.GetDeveloperPrefix()).To(Equal("lissto-"))
		})
	})

	Describe("GetDeveloperNamespace", func() {
		DescribeTable("should generate correct developer namespace",
			func(prefix, username, expected string) {
				m := NewManager("global", prefix)
				Expect(m.GetDeveloperNamespace(username)).To(Equal(expected))
			},
			Entry("standard prefix", "lissto-", "daniel", "lissto-daniel"),
			Entry("dev prefix", "dev-", "alice", "dev-alice"),
			Entry("empty username", "lissto-", "", "lissto-"),
			Entry("complex username", "ns-", "user.name", "ns-user.name"),
		)
	})

	Describe("IsGlobalNamespace", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		DescribeTable("should correctly identify global namespace",
			func(namespace string, expected bool) {
				Expect(m.IsGlobalNamespace(namespace)).To(Equal(expected))
			},
			Entry("exact match", "lissto-global", true),
			Entry("developer namespace", "lissto-daniel", false),
			Entry("other namespace", "other-namespace", false),
			Entry("empty string", "", false),
			Entry("global with suffix", "lissto-global-extra", false),
		)
	})

	Describe("IsDeveloperNamespace", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		DescribeTable("should correctly identify developer namespace",
			func(namespace string, expected bool) {
				Expect(m.IsDeveloperNamespace(namespace)).To(Equal(expected))
			},
			Entry("developer namespace daniel", "lissto-daniel", true),
			Entry("developer namespace alice", "lissto-alice", true),
			Entry("just prefix (edge case)", "lissto-", true),
			Entry("global namespace should not be developer", "lissto-global", false),
			Entry("different prefix", "dev-daniel", false),
			Entry("other namespace", "other-namespace", false),
			Entry("empty string", "", false),
		)
	})

	Describe("GetOwnerFromNamespace", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		Context("with valid developer namespaces", func() {
			DescribeTable("should extract owner correctly",
				func(namespace, expectedOwner string) {
					owner, err := m.GetOwnerFromNamespace(namespace)
					Expect(err).NotTo(HaveOccurred())
					Expect(owner).To(Equal(expectedOwner))
				},
				Entry("daniel", "lissto-daniel", "daniel"),
				Entry("alice", "lissto-alice", "alice"),
				Entry("user.name", "lissto-user.name", "user.name"),
				Entry("just prefix (edge case)", "lissto-", ""),
			)
		})

		Context("with invalid namespaces", func() {
			DescribeTable("should return error",
				func(namespace string) {
					_, err := m.GetOwnerFromNamespace(namespace)
					Expect(err).To(HaveOccurred())
				},
				Entry("global namespace", "lissto-global"),
				Entry("different prefix", "dev-daniel"),
				Entry("other namespace", "other-namespace"),
				Entry("empty string", ""),
			)
		})
	})

	Describe("NormalizeToScope", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		Context("with valid namespaces", func() {
			DescribeTable("should normalize correctly",
				func(namespace, expectedScope string) {
					scope, err := m.NormalizeToScope(namespace)
					Expect(err).NotTo(HaveOccurred())
					Expect(scope).To(Equal(expectedScope))
				},
				Entry("global namespace", "lissto-global", "global"),
				Entry("developer namespace daniel", "lissto-daniel", "daniel"),
				Entry("developer namespace alice", "lissto-alice", "alice"),
			)
		})

		Context("with invalid namespaces", func() {
			DescribeTable("should return error",
				func(namespace string) {
					_, err := m.NormalizeToScope(namespace)
					Expect(err).To(HaveOccurred())
				},
				Entry("other namespace", "other-namespace"),
				Entry("empty string", ""),
			)
		})
	})

	Describe("GenerateScopedID", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		Context("with valid inputs", func() {
			DescribeTable("should generate correct scoped ID",
				func(namespace, name, expected string) {
					scopedID, err := m.GenerateScopedID(namespace, name)
					Expect(err).NotTo(HaveOccurred())
					Expect(scopedID).To(Equal(expected))
				},
				Entry("global namespace", "lissto-global", "bp-123", "global/bp-123"),
				Entry("developer namespace daniel", "lissto-daniel", "bp-456", "daniel/bp-456"),
				Entry("developer namespace alice", "lissto-alice", "stack-789", "alice/stack-789"),
			)
		})

		Context("with invalid inputs", func() {
			DescribeTable("should return error",
				func(namespace, name string) {
					_, err := m.GenerateScopedID(namespace, name)
					Expect(err).To(HaveOccurred())
				},
				Entry("other namespace", "other-namespace", "bp-123"),
				Entry("empty namespace", "", "bp-123"),
			)
		})
	})

	Describe("MustGenerateScopedID", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		It("should return scoped ID for valid input", func() {
			Expect(m.MustGenerateScopedID("lissto-global", "bp-123")).To(Equal("global/bp-123"))
		})

		It("should panic for invalid namespace", func() {
			Expect(func() {
				m.MustGenerateScopedID("invalid-namespace", "bp-123")
			}).To(Panic())
		})
	})

	Describe("ParseScopedID", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		Context("with valid scoped IDs", func() {
			DescribeTable("should parse correctly",
				func(scopedID, expectedNS, expectedName string) {
					ns, name, err := m.ParseScopedID(scopedID)
					Expect(err).NotTo(HaveOccurred())
					Expect(ns).To(Equal(expectedNS))
					Expect(name).To(Equal(expectedName))
				},
				Entry("global scoped", "global/bp-123", "lissto-global", "bp-123"),
				Entry("developer scoped daniel", "daniel/bp-456", "lissto-daniel", "bp-456"),
				Entry("developer scoped alice", "alice/stack-789", "lissto-alice", "stack-789"),
				Entry("legacy format (no slash)", "bp-legacy", "", "bp-legacy"),
				Entry("legacy format (just name)", "just-name", "", "just-name"),
			)
		})

		Context("with invalid scoped IDs", func() {
			DescribeTable("should return error",
				func(scopedID string) {
					_, _, err := m.ParseScopedID(scopedID)
					Expect(err).To(HaveOccurred())
				},
				Entry("empty scope", "/bp-123"),
				Entry("empty name", "daniel/"),
				Entry("both empty", "/"),
			)
		})
	})

	Describe("ParseScopedIDWithDefault", func() {
		var m *Manager
		const defaultNS = "lissto-daniel"

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		Context("with valid scoped IDs", func() {
			DescribeTable("should parse correctly with default",
				func(scopedID, expectedNS, expectedName string) {
					ns, name, err := m.ParseScopedIDWithDefault(scopedID, defaultNS)
					Expect(err).NotTo(HaveOccurred())
					Expect(ns).To(Equal(expectedNS))
					Expect(name).To(Equal(expectedName))
				},
				Entry("global scoped", "global/bp-123", "lissto-global", "bp-123"),
				Entry("developer scoped alice", "alice/bp-456", "lissto-alice", "bp-456"),
				Entry("legacy format uses default", "bp-legacy", "lissto-daniel", "bp-legacy"),
				Entry("just name uses default", "just-name", "lissto-daniel", "just-name"),
			)
		})

		It("should return error for invalid format", func() {
			_, _, err := m.ParseScopedIDWithDefault("/bp-123", defaultNS)
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("IsNamespaceAllowed", func() {
		DescribeTable("should check namespace access correctly",
			func(namespace string, allowed []string, expected bool) {
				Expect(IsNamespaceAllowed(namespace, allowed)).To(Equal(expected))
			},
			Entry("exact match", "lissto-daniel", []string{"lissto-daniel", "lissto-global"}, true),
			Entry("no match", "lissto-alice", []string{"lissto-daniel", "lissto-global"}, false),
			Entry("wildcard", "any-namespace", []string{"*"}, true),
			Entry("empty allowed list", "lissto-daniel", []string{}, false),
			Entry("empty namespace", "", []string{"lissto-daniel"}, false),
			Entry("wildcard not first", "any-namespace", []string{"lissto-daniel", "*"}, false),
		)
	})

	Describe("ResolveNamespaceFromID", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		Context("with standard allowed namespaces", func() {
			allowedNS := []string{"lissto-daniel", "lissto-global"}

			DescribeTable("should resolve correctly",
				func(idParam, expectedTargetNS, expectedName string, expectedSearchAll bool) {
					targetNS, name, searchAll := m.ResolveNamespaceFromID(idParam, allowedNS)
					Expect(targetNS).To(Equal(expectedTargetNS))
					Expect(name).To(Equal(expectedName))
					Expect(searchAll).To(Equal(expectedSearchAll))
				},
				Entry("scoped global allowed", "global/bp-123", "lissto-global", "bp-123", false),
				Entry("scoped user allowed", "daniel/bp-456", "lissto-daniel", "bp-456", false),
				Entry("scoped user not allowed", "alice/bp-789", "", "bp-789", false),
				Entry("legacy format", "bp-legacy", "", "bp-legacy", true),
				Entry("invalid format", "/bp-123", "", "/bp-123", false),
			)
		})

		Context("with wildcard (admin) access", func() {
			allowedNS := []string{"*"}

			DescribeTable("should resolve correctly with wildcard",
				func(idParam, expectedTargetNS, expectedName string, expectedSearchAll bool) {
					targetNS, name, searchAll := m.ResolveNamespaceFromID(idParam, allowedNS)
					Expect(targetNS).To(Equal(expectedTargetNS))
					Expect(name).To(Equal(expectedName))
					Expect(searchAll).To(Equal(expectedSearchAll))
				},
				Entry("scoped any user", "alice/bp-123", "lissto-alice", "bp-123", false),
				Entry("scoped global", "global/bp-456", "lissto-global", "bp-456", false),
				Entry("legacy format", "bp-legacy", "", "bp-legacy", true),
			)
		})
	})

	Describe("ResolveNamespacesToSearch", func() {
		const userNS = "lissto-daniel"
		const globalNS = "lissto-global"

		DescribeTable("should resolve search namespaces correctly",
			func(targetNS string, searchAll bool, allowedNS []string, expected []string) {
				result := ResolveNamespacesToSearch(targetNS, userNS, globalNS, searchAll, allowedNS)
				Expect(result).To(Equal(expected))
			},
			Entry("scoped ID - specific namespace",
				"lissto-alice", false, []string{"lissto-daniel", "lissto-global"},
				[]string{"lissto-alice"}),
			Entry("legacy ID - user and global allowed",
				"", true, []string{"lissto-daniel", "lissto-global"},
				[]string{"lissto-daniel", "lissto-global"}),
			Entry("legacy ID - only user allowed",
				"", true, []string{"lissto-daniel"},
				[]string{"lissto-daniel"}),
			Entry("not authorized",
				"", false, []string{"lissto-daniel", "lissto-global"},
				[]string{}),
			Entry("wildcard allowed",
				"", true, []string{"*"},
				[]string{"lissto-daniel", "lissto-global"}),
		)
	})

	Describe("Round Trip", func() {
		var m *Manager

		BeforeEach(func() {
			m = NewManager("lissto-global", "lissto-")
		})

		DescribeTable("GenerateScopedID and ParseScopedID should be inverses",
			func(namespace, name string) {
				scopedID, err := m.GenerateScopedID(namespace, name)
				Expect(err).NotTo(HaveOccurred())

				gotNS, gotName, err := m.ParseScopedID(scopedID)
				Expect(err).NotTo(HaveOccurred())
				Expect(gotNS).To(Equal(namespace))
				Expect(gotName).To(Equal(name))
			},
			Entry("global namespace", "lissto-global", "blueprint-123"),
			Entry("developer namespace daniel", "lissto-daniel", "stack-456"),
			Entry("developer namespace alice", "lissto-alice", "env-789"),
		)
	})

	Describe("Different Prefix Configurations", func() {
		DescribeTable("should work with various prefix configurations",
			func(globalNS, prefix string) {
				m := NewManager(globalNS, prefix)

				// Test developer namespace generation
				devNS := m.GetDeveloperNamespace("testuser")
				Expect(devNS).To(Equal(prefix + "testuser"))

				// Test round trip
				scopedID, err := m.GenerateScopedID(devNS, "resource-123")
				Expect(err).NotTo(HaveOccurred())

				gotNS, gotName, err := m.ParseScopedID(scopedID)
				Expect(err).NotTo(HaveOccurred())
				Expect(gotNS).To(Equal(devNS))
				Expect(gotName).To(Equal("resource-123"))
			},
			Entry("lissto prefix", "lissto-global", "lissto-"),
			Entry("dev prefix", "global", "dev-"),
			Entry("prod prefix", "prod-global", "prod-"),
		)
	})
})
