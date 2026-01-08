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

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	stackconfig "github.com/lissto-dev/controller/internal/stack/config"
)

const testStackName = "test-stack"

var _ = Describe("Reserved Environment Variables", func() {
	Context("IsReservedEnvVar", func() {
		It("should identify LISSTO_ENV as reserved", func() {
			Expect(stackconfig.IsReservedEnvVar("LISSTO_ENV")).To(BeTrue())
		})

		It("should identify LISSTO_STACK as reserved", func() {
			Expect(stackconfig.IsReservedEnvVar("LISSTO_STACK")).To(BeTrue())
		})

		It("should identify LISSTO_USER as reserved", func() {
			Expect(stackconfig.IsReservedEnvVar("LISSTO_USER")).To(BeTrue())
		})

		It("should identify any LISSTO_* variable as reserved", func() {
			Expect(stackconfig.IsReservedEnvVar("LISSTO_CUSTOM")).To(BeTrue())
			Expect(stackconfig.IsReservedEnvVar("LISSTO_ANYTHING")).To(BeTrue())
			Expect(stackconfig.IsReservedEnvVar("LISSTO_FOO_BAR")).To(BeTrue())
		})

		It("should not identify regular variables as reserved", func() {
			Expect(stackconfig.IsReservedEnvVar("DATABASE_URL")).To(BeFalse())
			Expect(stackconfig.IsReservedEnvVar("MY_VAR")).To(BeFalse())
			Expect(stackconfig.IsReservedEnvVar("LISSTO")).To(BeFalse()) // partial match doesn't count
		})
	})

	Context("ExtractStackMetadata", func() {
		It("should extract metadata from Stack with all fields", func() {
			stack := &envv1alpha1.Stack{}
			stack.Name = "test-stack-20251130"
			stack.Spec.Env = "dev-alice"
			stack.Annotations = map[string]string{
				"lissto.dev/created-by": "alice",
			}

			metadata := stackconfig.ExtractStackMetadata(stack)

			Expect(metadata.Env).To(Equal("dev-alice"))
			Expect(metadata.StackName).To(Equal("test-stack-20251130"))
			Expect(metadata.User).To(Equal("alice"))
		})

		It("should use 'unknown' when annotation is missing", func() {
			stack := &envv1alpha1.Stack{}
			stack.Name = testStackName
			stack.Spec.Env = "staging"
			stack.Annotations = map[string]string{}

			metadata := stackconfig.ExtractStackMetadata(stack)

			Expect(metadata.User).To(Equal("unknown"))
		})

		It("should use 'unknown' when annotation is empty", func() {
			stack := &envv1alpha1.Stack{}
			stack.Name = testStackName
			stack.Spec.Env = "prod"
			stack.Annotations = map[string]string{
				"lissto.dev/created-by": "",
			}

			metadata := stackconfig.ExtractStackMetadata(stack)

			Expect(metadata.User).To(Equal("unknown"))
		})

		It("should handle nil annotations", func() {
			stack := &envv1alpha1.Stack{}
			stack.Name = testStackName
			stack.Spec.Env = "dev"
			stack.Annotations = nil

			metadata := stackconfig.ExtractStackMetadata(stack)

			Expect(metadata.User).To(Equal("unknown"))
			Expect(metadata.Env).To(Equal("dev"))
			Expect(metadata.StackName).To(Equal(testStackName))
		})
	})

	// Note: The following tests have been moved to internal/stack/config package tests.
	// See internal/stack/config/resolver_test.go and internal/stack/config/types_test.go
})
