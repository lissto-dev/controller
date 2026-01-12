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

package config_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/stack/config"
)

var _ = Describe("Config Resolver", func() {
	var resolver *config.Resolver

	BeforeEach(func() {
		resolver = config.NewResolver()
	})

	Describe("ResolveVariables", func() {
		Context("with empty variables", func() {
			It("should return empty map", func() {
				result := resolver.ResolveVariables([]envv1alpha1.LisstoVariable{})
				Expect(result).To(BeEmpty())
			})
		})

		Context("with single variable", func() {
			It("should return the variable data", func() {
				variables := []envv1alpha1.LisstoVariable{
					makeVariable("global-vars", config.ScopeGlobal, map[string]string{
						"DATABASE_URL": "postgres://localhost",
					}),
				}

				result := resolver.ResolveVariables(variables)

				Expect(result).To(HaveKeyWithValue("DATABASE_URL", "postgres://localhost"))
			})
		})

		Context("with env overriding global", func() {
			It("should use env value", func() {
				variables := []envv1alpha1.LisstoVariable{
					makeVariable("global-vars", config.ScopeGlobal, map[string]string{
						"DATABASE_URL": "postgres://global",
						"API_KEY":      "global-key",
					}),
					makeVariable("env-vars", config.ScopeEnv, map[string]string{
						"DATABASE_URL": "postgres://env",
					}),
				}

				result := resolver.ResolveVariables(variables)

				Expect(result).To(HaveKeyWithValue("DATABASE_URL", "postgres://env"))
				Expect(result).To(HaveKeyWithValue("API_KEY", "global-key"))
			})
		})

		Context("with repo overriding global", func() {
			It("should use repo value", func() {
				variables := []envv1alpha1.LisstoVariable{
					makeVariable("global-vars", config.ScopeGlobal, map[string]string{
						"DATABASE_URL": "postgres://global",
					}),
					makeVariable("repo-vars", config.ScopeRepo, map[string]string{
						"DATABASE_URL": "postgres://repo",
					}),
				}

				result := resolver.ResolveVariables(variables)

				Expect(result).To(HaveKeyWithValue("DATABASE_URL", "postgres://repo"))
			})
		})

		Context("with full hierarchy", func() {
			It("should apply env > repo > global priority", func() {
				variables := []envv1alpha1.LisstoVariable{
					makeVariable("global-vars", config.ScopeGlobal, map[string]string{
						"VAR1": "global",
						"VAR2": "global",
						"VAR3": "global",
					}),
					makeVariable("repo-vars", config.ScopeRepo, map[string]string{
						"VAR1": "repo",
						"VAR2": "repo",
					}),
					makeVariable("env-vars", config.ScopeEnv, map[string]string{
						"VAR1": "env",
					}),
				}

				result := resolver.ResolveVariables(variables)

				Expect(result).To(HaveKeyWithValue("VAR1", "env"))
				Expect(result).To(HaveKeyWithValue("VAR2", "repo"))
				Expect(result).To(HaveKeyWithValue("VAR3", "global"))
			})
		})

		Context("with reserved variables", func() {
			It("should filter them out", func() {
				variables := []envv1alpha1.LisstoVariable{
					makeVariable("vars", config.ScopeGlobal, map[string]string{
						"DATABASE_URL": "postgres://localhost",
						"LISSTO_ENV":   "should-be-filtered",
						"LISSTO_STACK": "should-be-filtered",
					}),
				}

				result := resolver.ResolveVariables(variables)

				Expect(result).To(HaveKeyWithValue("DATABASE_URL", "postgres://localhost"))
				Expect(result).NotTo(HaveKey("LISSTO_ENV"))
				Expect(result).NotTo(HaveKey("LISSTO_STACK"))
			})
		})
	})

	Describe("ResolveSecretKeys", func() {
		Context("with empty secrets", func() {
			It("should return empty map", func() {
				result := resolver.ResolveSecretKeys([]envv1alpha1.LisstoSecret{})
				Expect(result).To(BeEmpty())
			})
		})

		Context("with single secret", func() {
			It("should return all keys", func() {
				secrets := []envv1alpha1.LisstoSecret{
					makeSecret("global-secrets", config.ScopeGlobal, []string{"API_KEY", "DB_PASSWORD"}),
				}

				result := resolver.ResolveSecretKeys(secrets)

				Expect(result).To(HaveKey("API_KEY"))
				Expect(result).To(HaveKey("DB_PASSWORD"))
				Expect(result["API_KEY"].LisstoSecret.GetScope()).To(Equal(config.ScopeGlobal))
			})
		})

		Context("with env overriding global", func() {
			It("should use env secret for overlapping keys", func() {
				secrets := []envv1alpha1.LisstoSecret{
					makeSecret("global-secrets", config.ScopeGlobal, []string{"API_KEY", "DB_PASSWORD"}),
					makeSecret("env-secrets", config.ScopeEnv, []string{"API_KEY"}),
				}

				result := resolver.ResolveSecretKeys(secrets)

				Expect(result["API_KEY"].LisstoSecret.GetScope()).To(Equal(config.ScopeEnv))
				Expect(result["DB_PASSWORD"].LisstoSecret.GetScope()).To(Equal(config.ScopeGlobal))
			})
		})

		Context("with reserved keys", func() {
			It("should filter them out", func() {
				secrets := []envv1alpha1.LisstoSecret{
					makeSecret("secrets", config.ScopeGlobal, []string{"API_KEY", "LISSTO_SECRET", "DB_PASSWORD"}),
				}

				result := resolver.ResolveSecretKeys(secrets)

				Expect(result).To(HaveKey("API_KEY"))
				Expect(result).To(HaveKey("DB_PASSWORD"))
				Expect(result).NotTo(HaveKey("LISSTO_SECRET"))
			})
		})
	})
})

// Helper functions

func makeVariable(name, scope string, data map[string]string) envv1alpha1.LisstoVariable {
	v := envv1alpha1.LisstoVariable{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: envv1alpha1.LisstoVariableSpec{
			Scope: scope,
			Data:  data,
		},
	}

	switch scope {
	case config.ScopeEnv:
		v.Spec.Env = "test-env"
	case config.ScopeRepo:
		v.Spec.Repository = "test-repo"
	}

	return v
}

func makeSecret(name, scope string, keys []string) envv1alpha1.LisstoSecret {
	s := envv1alpha1.LisstoSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: envv1alpha1.LisstoSecretSpec{
			Scope:     scope,
			SecretRef: name + "-k8s",
			Keys:      keys,
		},
	}

	switch scope {
	case config.ScopeEnv:
		s.Spec.Env = "test-env"
	case config.ScopeRepo:
		s.Spec.Repository = "test-repo"
	}

	return s
}
