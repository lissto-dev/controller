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

package resource_test

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/lissto-dev/controller/internal/stack/resource"
)

var _ = Describe("Resource Types", func() {
	Describe("HasFailures", func() {
		Context("with empty results", func() {
			It("should return false", func() {
				results := []resource.Result{}
				Expect(resource.HasFailures(results)).To(BeFalse())
			})
		})

		Context("with all successful results", func() {
			It("should return false", func() {
				results := []resource.Result{
					{Kind: "Deployment", Name: "web", Applied: true},
					{Kind: "Service", Name: "web", Applied: true},
				}
				Expect(resource.HasFailures(results)).To(BeFalse())
			})
		})

		Context("with one failure", func() {
			It("should return true", func() {
				results := []resource.Result{
					{Kind: "Deployment", Name: "web", Applied: true},
					{Kind: "Service", Name: "web", Applied: false, Error: errors.New("failed")},
				}
				Expect(resource.HasFailures(results)).To(BeTrue())
			})
		})

		Context("with all failures", func() {
			It("should return true", func() {
				results := []resource.Result{
					{Kind: "Deployment", Name: "web", Applied: false, Error: errors.New("failed")},
					{Kind: "Service", Name: "web", Applied: false, Error: errors.New("failed")},
				}
				Expect(resource.HasFailures(results)).To(BeTrue())
			})
		})
	})

	Describe("CountApplied", func() {
		Context("with empty results", func() {
			It("should return 0", func() {
				results := []resource.Result{}
				Expect(resource.CountApplied(results)).To(Equal(0))
			})
		})

		Context("with all successful results", func() {
			It("should return the total count", func() {
				results := []resource.Result{
					{Kind: "Deployment", Name: "web", Applied: true},
					{Kind: "Service", Name: "web", Applied: true},
					{Kind: "PVC", Name: "data", Applied: true},
				}
				Expect(resource.CountApplied(results)).To(Equal(3))
			})
		})

		Context("with mixed results", func() {
			It("should return only the successful count", func() {
				results := []resource.Result{
					{Kind: "Deployment", Name: "web", Applied: true},
					{Kind: "Service", Name: "web", Applied: false, Error: errors.New("failed")},
					{Kind: "PVC", Name: "data", Applied: true},
				}
				Expect(resource.CountApplied(results)).To(Equal(2))
			})
		})

		Context("with no successful results", func() {
			It("should return 0", func() {
				results := []resource.Result{
					{Kind: "Deployment", Name: "web", Applied: false, Error: errors.New("failed")},
					{Kind: "Service", Name: "web", Applied: false, Error: errors.New("failed")},
				}
				Expect(resource.CountApplied(results)).To(Equal(0))
			})
		})
	})
})
