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

var _ = Describe("Config Types", func() {
	Describe("IsReservedEnvVar", func() {
		DescribeTable("should correctly identify reserved variables",
			func(envVar string, expected bool) {
				Expect(config.IsReservedEnvVar(envVar)).To(Equal(expected))
			},
			Entry("LISSTO_ENV is reserved", "LISSTO_ENV", true),
			Entry("LISSTO_STACK is reserved", "LISSTO_STACK", true),
			Entry("LISSTO_USER is reserved", "LISSTO_USER", true),
			Entry("LISSTO_CUSTOM is reserved", "LISSTO_CUSTOM", true),
			Entry("DATABASE_URL is not reserved", "DATABASE_URL", false),
			Entry("API_KEY is not reserved", "API_KEY", false),
			Entry("LISSTO without underscore is not reserved", "LISSTO", false),
			Entry("lowercase lissto_env is not reserved", "lissto_env", false),
		)
	})

	Describe("ScopePriority", func() {
		It("should return correct priorities", func() {
			Expect(config.ScopePriority(config.ScopeEnv)).To(Equal(1))
			Expect(config.ScopePriority(config.ScopeRepo)).To(Equal(2))
			Expect(config.ScopePriority(config.ScopeGlobal)).To(Equal(3))
			Expect(config.ScopePriority("unknown")).To(Equal(99))
			Expect(config.ScopePriority("")).To(Equal(99))
		})

		It("should have env > repo > global priority ordering", func() {
			envPriority := config.ScopePriority(config.ScopeEnv)
			repoPriority := config.ScopePriority(config.ScopeRepo)
			globalPriority := config.ScopePriority(config.ScopeGlobal)

			Expect(envPriority).To(BeNumerically("<", repoPriority))
			Expect(repoPriority).To(BeNumerically("<", globalPriority))
		})
	})

	Describe("ExtractStackMetadata", func() {
		Context("with full metadata", func() {
			It("should extract all fields", func() {
				stack := &envv1alpha1.Stack{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-stack",
						Annotations: map[string]string{
							"lissto.dev/created-by": "john",
						},
					},
					Spec: envv1alpha1.StackSpec{
						Env: "dev-john",
					},
				}

				metadata := config.ExtractStackMetadata(stack)

				Expect(metadata.Env).To(Equal("dev-john"))
				Expect(metadata.StackName).To(Equal("my-stack"))
				Expect(metadata.User).To(Equal("john"))
			})
		})

		Context("with missing created-by annotation", func() {
			It("should default user to unknown", func() {
				stack := &envv1alpha1.Stack{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "my-stack",
						Annotations: map[string]string{},
					},
					Spec: envv1alpha1.StackSpec{
						Env: "staging",
					},
				}

				metadata := config.ExtractStackMetadata(stack)

				Expect(metadata.User).To(Equal("unknown"))
			})
		})

		Context("with nil annotations", func() {
			It("should default user to unknown", func() {
				stack := &envv1alpha1.Stack{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-stack",
					},
					Spec: envv1alpha1.StackSpec{
						Env: "prod",
					},
				}

				metadata := config.ExtractStackMetadata(stack)

				Expect(metadata.User).To(Equal("unknown"))
			})
		})

		Context("with empty created-by annotation", func() {
			It("should default user to unknown", func() {
				stack := &envv1alpha1.Stack{
					ObjectMeta: metav1.ObjectMeta{
						Name: "my-stack",
						Annotations: map[string]string{
							"lissto.dev/created-by": "",
						},
					},
					Spec: envv1alpha1.StackSpec{
						Env: "dev",
					},
				}

				metadata := config.ExtractStackMetadata(stack)

				Expect(metadata.User).To(Equal("unknown"))
			})
		})
	})
})
