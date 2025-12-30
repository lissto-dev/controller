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

	Context("parseAutoInject", func() {
		It("should return true for empty string", func() {
			Expect(parseAutoInject("")).To(BeTrue())
		})

		It("should return true for 'true'", func() {
			Expect(parseAutoInject("true")).To(BeTrue())
		})

		It("should return true for 'TRUE'", func() {
			Expect(parseAutoInject("TRUE")).To(BeTrue())
		})

		It("should return false for 'false'", func() {
			Expect(parseAutoInject("false")).To(BeFalse())
		})

		It("should return false for 'FALSE'", func() {
			Expect(parseAutoInject("FALSE")).To(BeFalse())
		})

		It("should return false for 'False'", func() {
			Expect(parseAutoInject("False")).To(BeFalse())
		})

		It("should return true for any other value", func() {
			Expect(parseAutoInject("yes")).To(BeTrue())
			Expect(parseAutoInject("no")).To(BeTrue())
			Expect(parseAutoInject("invalid")).To(BeTrue())
		})
	})

	Context("parseKeyMapping", func() {
		It("should return empty map for empty string", func() {
			result := parseKeyMapping("")
			Expect(result).To(BeEmpty())
		})

		It("should parse single mapping", func() {
			result := parseKeyMapping("TARGET=SOURCE")
			Expect(result).To(HaveLen(1))
			Expect(result["TARGET"]).To(Equal("SOURCE"))
		})

		It("should parse multiple mappings", func() {
			result := parseKeyMapping("TARGET1=SOURCE1,TARGET2=SOURCE2")
			Expect(result).To(HaveLen(2))
			Expect(result["TARGET1"]).To(Equal("SOURCE1"))
			Expect(result["TARGET2"]).To(Equal("SOURCE2"))
		})

		It("should handle whitespace around mappings", func() {
			result := parseKeyMapping(" TARGET1 = SOURCE1 , TARGET2 = SOURCE2 ")
			Expect(result).To(HaveLen(2))
			Expect(result["TARGET1"]).To(Equal("SOURCE1"))
			Expect(result["TARGET2"]).To(Equal("SOURCE2"))
		})

		It("should skip invalid mappings without equals sign", func() {
			result := parseKeyMapping("VALID=SOURCE,INVALID,ANOTHER=VALUE")
			Expect(result).To(HaveLen(2))
			Expect(result["VALID"]).To(Equal("SOURCE"))
			Expect(result["ANOTHER"]).To(Equal("VALUE"))
		})

		It("should skip mappings with empty target or source", func() {
			result := parseKeyMapping("=SOURCE,TARGET=,VALID=VALUE")
			Expect(result).To(HaveLen(1))
			Expect(result["VALID"]).To(Equal("VALUE"))
		})

		It("should handle equals sign in source value", func() {
			result := parseKeyMapping("TARGET=SOURCE=WITH=EQUALS")
			Expect(result).To(HaveLen(1))
			Expect(result["TARGET"]).To(Equal("SOURCE=WITH=EQUALS"))
		})

		It("should skip empty pairs from trailing comma", func() {
			result := parseKeyMapping("TARGET=SOURCE,")
			Expect(result).To(HaveLen(1))
			Expect(result["TARGET"]).To(Equal("SOURCE"))
		})
	})

	Context("extractInjectionConfig", func() {
		It("should return defaults for nil annotations", func() {
			config := extractInjectionConfig(nil)
			Expect(config.AutoInject).To(BeTrue())
			Expect(config.SecretMap).To(BeEmpty())
			Expect(config.VarMap).To(BeEmpty())
		})

		It("should return defaults for empty annotations", func() {
			config := extractInjectionConfig(map[string]string{})
			Expect(config.AutoInject).To(BeTrue())
			Expect(config.SecretMap).To(BeEmpty())
			Expect(config.VarMap).To(BeEmpty())
		})

		It("should parse auto-inject annotation", func() {
			config := extractInjectionConfig(map[string]string{
				"lissto.dev/auto-inject": "false",
			})
			Expect(config.AutoInject).To(BeFalse())
		})

		It("should parse inject-secrets annotation", func() {
			config := extractInjectionConfig(map[string]string{
				"lissto.dev/inject-secrets": "MY_VAR=SECRET_KEY",
			})
			Expect(config.SecretMap).To(HaveLen(1))
			Expect(config.SecretMap["MY_VAR"]).To(Equal("SECRET_KEY"))
		})

		It("should parse inject-vars annotation", func() {
			config := extractInjectionConfig(map[string]string{
				"lissto.dev/inject-vars": "MY_CONFIG=GLOBAL_VAR",
			})
			Expect(config.VarMap).To(HaveLen(1))
			Expect(config.VarMap["MY_CONFIG"]).To(Equal("GLOBAL_VAR"))
		})

		It("should parse all annotations together", func() {
			config := extractInjectionConfig(map[string]string{
				"lissto.dev/auto-inject":    "false",
				"lissto.dev/inject-secrets": "SECRET_VAR=SECRET_KEY",
				"lissto.dev/inject-vars":    "VAR1=SOURCE1,VAR2=SOURCE2",
			})
			Expect(config.AutoInject).To(BeFalse())
			Expect(config.SecretMap).To(HaveLen(1))
			Expect(config.SecretMap["SECRET_VAR"]).To(Equal("SECRET_KEY"))
			Expect(config.VarMap).To(HaveLen(2))
			Expect(config.VarMap["VAR1"]).To(Equal("SOURCE1"))
			Expect(config.VarMap["VAR2"]).To(Equal("SOURCE2"))
		})
	})

	Context("filterEnvWithConfig", func() {
		var (
			mergedVars   map[string]string
			resolvedKeys map[string]secretKeySource
		)

		BeforeEach(func() {
			mergedVars = map[string]string{
				"VAR1":       "value1",
				"VAR2":       "value2",
				"GLOBAL_VAR": "global_value",
			}
			resolvedKeys = map[string]secretKeySource{
				"SECRET1":       {Key: "SECRET1"},
				"SECRET2":       {Key: "SECRET2"},
				"GLOBAL_SECRET": {Key: "GLOBAL_SECRET"},
			}
		})

		It("should auto-inject matching declared env vars when auto-inject is true", func() {
			existingEnvNames := map[string]bool{"VAR1": true, "SECRET1": true}
			config := InjectionConfig{AutoInject: true, SecretMap: map[string]string{}, VarMap: map[string]string{}}

			vars, secrets := filterEnvWithConfig(mergedVars, resolvedKeys, existingEnvNames, config)

			Expect(vars).To(HaveLen(1))
			Expect(vars[0].TargetEnvName).To(Equal("VAR1"))
			Expect(vars[0].SourceKey).To(Equal("VAR1"))
			Expect(secrets).To(HaveLen(1))
			Expect(secrets[0].TargetEnvName).To(Equal("SECRET1"))
			Expect(secrets[0].SourceKey).To(Equal("SECRET1"))
		})

		It("should not auto-inject when auto-inject is false", func() {
			existingEnvNames := map[string]bool{"VAR1": true, "SECRET1": true}
			config := InjectionConfig{AutoInject: false, SecretMap: map[string]string{}, VarMap: map[string]string{}}

			vars, secrets := filterEnvWithConfig(mergedVars, resolvedKeys, existingEnvNames, config)

			Expect(vars).To(BeEmpty())
			Expect(secrets).To(BeEmpty())
		})

		It("should inject mapped keys when auto-inject is false", func() {
			existingEnvNames := map[string]bool{}
			config := InjectionConfig{
				AutoInject: false,
				SecretMap:  map[string]string{"MY_SECRET": "GLOBAL_SECRET"},
				VarMap:     map[string]string{"MY_VAR": "GLOBAL_VAR"},
			}

			vars, secrets := filterEnvWithConfig(mergedVars, resolvedKeys, existingEnvNames, config)

			Expect(vars).To(HaveLen(1))
			Expect(vars[0].TargetEnvName).To(Equal("MY_VAR"))
			Expect(vars[0].SourceKey).To(Equal("GLOBAL_VAR"))
			Expect(vars[0].Value).To(Equal("global_value"))
			Expect(secrets).To(HaveLen(1))
			Expect(secrets[0].TargetEnvName).To(Equal("MY_SECRET"))
			Expect(secrets[0].SourceKey).To(Equal("GLOBAL_SECRET"))
		})

		It("should combine auto-inject and mapped keys when auto-inject is true", func() {
			existingEnvNames := map[string]bool{"VAR1": true}
			config := InjectionConfig{
				AutoInject: true,
				SecretMap:  map[string]string{"MY_SECRET": "GLOBAL_SECRET"},
				VarMap:     map[string]string{},
			}

			vars, secrets := filterEnvWithConfig(mergedVars, resolvedKeys, existingEnvNames, config)

			Expect(vars).To(HaveLen(1))
			Expect(vars[0].TargetEnvName).To(Equal("VAR1"))
			Expect(secrets).To(HaveLen(1))
			Expect(secrets[0].TargetEnvName).To(Equal("MY_SECRET"))
		})

		It("should skip reserved env var names in mappings", func() {
			existingEnvNames := map[string]bool{}
			config := InjectionConfig{
				AutoInject: false,
				SecretMap:  map[string]string{"LISSTO_SECRET": "GLOBAL_SECRET"},
				VarMap:     map[string]string{"LISSTO_VAR": "GLOBAL_VAR"},
			}

			vars, secrets := filterEnvWithConfig(mergedVars, resolvedKeys, existingEnvNames, config)

			Expect(vars).To(BeEmpty())
			Expect(secrets).To(BeEmpty())
		})

		It("should skip mappings for non-existent source keys", func() {
			existingEnvNames := map[string]bool{}
			config := InjectionConfig{
				AutoInject: false,
				SecretMap:  map[string]string{"MY_SECRET": "NON_EXISTENT"},
				VarMap:     map[string]string{"MY_VAR": "NON_EXISTENT"},
			}

			vars, secrets := filterEnvWithConfig(mergedVars, resolvedKeys, existingEnvNames, config)

			Expect(vars).To(BeEmpty())
			Expect(secrets).To(BeEmpty())
		})

		It("should not duplicate when mapping target equals auto-inject key", func() {
			existingEnvNames := map[string]bool{"VAR1": true}
			config := InjectionConfig{
				AutoInject: true,
				VarMap:     map[string]string{"VAR1": "GLOBAL_VAR"}, // Mapping takes priority
				SecretMap:  map[string]string{},
			}

			vars, _ := filterEnvWithConfig(mergedVars, resolvedKeys, existingEnvNames, config)

			// Should have only one VAR1, from the mapping (not auto-inject)
			Expect(vars).To(HaveLen(1))
			Expect(vars[0].TargetEnvName).To(Equal("VAR1"))
			Expect(vars[0].SourceKey).To(Equal("GLOBAL_VAR"))
			Expect(vars[0].Value).To(Equal("global_value"))
		})
	})
})
