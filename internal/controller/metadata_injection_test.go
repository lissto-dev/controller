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

package controller

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

const testStackName = "test-stack"

var _ = Describe("Reserved Environment Variables", func() {
	Context("isReservedEnvVar", func() {
		It("should identify LISSTO_ENV as reserved", func() {
			Expect(isReservedEnvVar("LISSTO_ENV")).To(BeTrue())
		})

		It("should identify LISSTO_STACK as reserved", func() {
			Expect(isReservedEnvVar("LISSTO_STACK")).To(BeTrue())
		})

		It("should identify LISSTO_USER as reserved", func() {
			Expect(isReservedEnvVar("LISSTO_USER")).To(BeTrue())
		})

		It("should identify any LISSTO_* variable as reserved", func() {
			Expect(isReservedEnvVar("LISSTO_CUSTOM")).To(BeTrue())
			Expect(isReservedEnvVar("LISSTO_ANYTHING")).To(BeTrue())
			Expect(isReservedEnvVar("LISSTO_FOO_BAR")).To(BeTrue())
		})

		It("should not identify regular variables as reserved", func() {
			Expect(isReservedEnvVar("DATABASE_URL")).To(BeFalse())
			Expect(isReservedEnvVar("MY_VAR")).To(BeFalse())
			Expect(isReservedEnvVar("LISSTO")).To(BeFalse()) // partial match doesn't count
		})
	})

	Context("extractStackMetadata", func() {
		It("should extract metadata from Stack with all fields", func() {
			stack := &envv1alpha1.Stack{}
			stack.Name = "test-stack-20251130"
			stack.Spec.Env = "dev-alice"
			stack.Annotations = map[string]string{
				"lissto.dev/created-by": "alice",
			}

			metadata := extractStackMetadata(stack)

			Expect(metadata.Env).To(Equal("dev-alice"))
			Expect(metadata.StackName).To(Equal("test-stack-20251130"))
			Expect(metadata.User).To(Equal("alice"))
		})

		It("should use 'unknown' when annotation is missing", func() {
			stack := &envv1alpha1.Stack{}
			stack.Name = testStackName
			stack.Spec.Env = "staging"
			stack.Annotations = map[string]string{}

			metadata := extractStackMetadata(stack)

			Expect(metadata.User).To(Equal("unknown"))
		})

		It("should use 'unknown' when annotation is empty", func() {
			stack := &envv1alpha1.Stack{}
			stack.Name = testStackName
			stack.Spec.Env = "prod"
			stack.Annotations = map[string]string{
				"lissto.dev/created-by": "",
			}

			metadata := extractStackMetadata(stack)

			Expect(metadata.User).To(Equal("unknown"))
		})

		It("should handle nil annotations", func() {
			stack := &envv1alpha1.Stack{}
			stack.Name = testStackName
			stack.Spec.Env = "dev"
			stack.Annotations = nil

			metadata := extractStackMetadata(stack)

			Expect(metadata.User).To(Equal("unknown"))
			Expect(metadata.Env).To(Equal("dev"))
			Expect(metadata.StackName).To(Equal(testStackName))
		})
	})

	Context("resolveVariables with reserved names", func() {
		It("should filter out LISSTO_ENV from variables", func() {
			reconciler := &StackReconciler{}
			variables := []envv1alpha1.LisstoVariable{
				{
					Spec: envv1alpha1.LisstoVariableSpec{
						Scope: "env",
						Data: map[string]string{
							"DATABASE_URL": "postgres://localhost",
							"LISSTO_ENV":   "hacker-env", // Should be filtered
						},
					},
				},
			}

			result := reconciler.resolveVariables(variables)

			Expect(result).To(HaveKey("DATABASE_URL"))
			Expect(result).NotTo(HaveKey("LISSTO_ENV"))
		})

		It("should filter out all reserved variables", func() {
			reconciler := &StackReconciler{}
			variables := []envv1alpha1.LisstoVariable{
				{
					Spec: envv1alpha1.LisstoVariableSpec{
						Scope: "env",
						Data: map[string]string{
							"VALID_VAR":    "value1",
							"LISSTO_ENV":   "fake-env",
							"LISSTO_STACK": "fake-stack",
							"LISSTO_USER":  "fake-user",
							"ANOTHER_VAR":  "value2",
						},
					},
				},
			}

			result := reconciler.resolveVariables(variables)

			Expect(result).To(HaveKey("VALID_VAR"))
			Expect(result).To(HaveKey("ANOTHER_VAR"))
			Expect(result).NotTo(HaveKey("LISSTO_ENV"))
			Expect(result).NotTo(HaveKey("LISSTO_STACK"))
			Expect(result).NotTo(HaveKey("LISSTO_USER"))
		})

		It("should preserve variable hierarchy while filtering reserved names", func() {
			reconciler := &StackReconciler{}
			variables := []envv1alpha1.LisstoVariable{
				{
					Spec: envv1alpha1.LisstoVariableSpec{
						Scope: "global",
						Data: map[string]string{
							"DATABASE_URL": "global-db",
							"LISSTO_ENV":   "global-env", // Filtered
						},
					},
				},
				{
					Spec: envv1alpha1.LisstoVariableSpec{
						Scope: "env",
						Data: map[string]string{
							"DATABASE_URL": "env-db",   // Should override
							"LISSTO_USER":  "attacker", // Filtered
						},
					},
				},
			}

			result := reconciler.resolveVariables(variables)

			Expect(result["DATABASE_URL"]).To(Equal("env-db"))
			Expect(result).NotTo(HaveKey("LISSTO_ENV"))
			Expect(result).NotTo(HaveKey("LISSTO_USER"))
		})
	})

	Context("resolveSecretKeys with reserved names", func() {
		It("should filter out LISSTO_STACK from secret keys", func() {
			reconciler := &StackReconciler{}
			secrets := []envv1alpha1.LisstoSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-secret",
					},
					Spec: envv1alpha1.LisstoSecretSpec{
						Scope:     "env",
						SecretRef: "k8s-secret",
						Keys: []string{
							"API_KEY",
							"LISSTO_STACK", // Should be filtered
							"DB_PASSWORD",
						},
					},
				},
			}

			result := reconciler.resolveSecretKeys(secrets)

			Expect(result).To(HaveKey("API_KEY"))
			Expect(result).To(HaveKey("DB_PASSWORD"))
			Expect(result).NotTo(HaveKey("LISSTO_STACK"))
		})

		It("should filter all reserved names from secrets", func() {
			reconciler := &StackReconciler{}
			secrets := []envv1alpha1.LisstoSecret{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-secret",
					},
					Spec: envv1alpha1.LisstoSecretSpec{
						Scope:     "env",
						SecretRef: "k8s-secret",
						Keys: []string{
							"VALID_SECRET",
							"LISSTO_ENV",
							"LISSTO_STACK",
							"LISSTO_USER",
						},
					},
				},
			}

			result := reconciler.resolveSecretKeys(secrets)

			Expect(result).To(HaveKey("VALID_SECRET"))
			Expect(result).NotTo(HaveKey("LISSTO_ENV"))
			Expect(result).NotTo(HaveKey("LISSTO_STACK"))
			Expect(result).NotTo(HaveKey("LISSTO_USER"))
		})
	})

	Context("ConfigInjectionResult", func() {
		It("should have MetadataInjected field", func() {
			result := &ConfigInjectionResult{
				VariablesInjected: 5,
				SecretsInjected:   3,
				MetadataInjected:  3,
			}

			Expect(result.MetadataInjected).To(Equal(3))
		})
	})

	Context("StackMetadata struct", func() {
		It("should hold all required metadata fields", func() {
			metadata := StackMetadata{
				Env:       "dev-bob",
				StackName: "stack-123",
				User:      "bob",
			}

			Expect(metadata.Env).To(Equal("dev-bob"))
			Expect(metadata.StackName).To(Equal("stack-123"))
			Expect(metadata.User).To(Equal("bob"))
		})
	})
})
