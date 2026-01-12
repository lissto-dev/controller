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

package manifest_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/lissto-dev/controller/internal/stack/manifest"
)

var _ = Describe("Manifest Parser", func() {
	Describe("Parse", func() {
		Context("with empty YAML", func() {
			It("should return empty slice", func() {
				objects, err := manifest.Parse("")
				Expect(err).NotTo(HaveOccurred())
				Expect(objects).To(BeEmpty())
			})
		})

		Context("with single document", func() {
			It("should parse successfully", func() {
				yaml := `apiVersion: v1
kind: Service
metadata:
  name: web
`
				objects, err := manifest.Parse(yaml)
				Expect(err).NotTo(HaveOccurred())
				Expect(objects).To(HaveLen(1))
				Expect(objects[0].GetKind()).To(Equal("Service"))
				Expect(objects[0].GetName()).To(Equal("web"))
			})
		})

		Context("with multiple documents", func() {
			It("should parse all documents", func() {
				yaml := `apiVersion: v1
kind: Service
metadata:
  name: web
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data
`
				objects, err := manifest.Parse(yaml)
				Expect(err).NotTo(HaveOccurred())
				Expect(objects).To(HaveLen(3))
				Expect(objects[0].GetKind()).To(Equal("Service"))
				Expect(objects[1].GetKind()).To(Equal("Deployment"))
				Expect(objects[2].GetKind()).To(Equal("PersistentVolumeClaim"))
			})
		})

		Context("with empty documents between valid ones", func() {
			It("should skip empty documents", func() {
				yaml := `---
apiVersion: v1
kind: Service
metadata:
  name: web
---
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
---
`
				objects, err := manifest.Parse(yaml)
				Expect(err).NotTo(HaveOccurred())
				Expect(objects).To(HaveLen(2))
			})
		})

		Context("with invalid YAML", func() {
			It("should return an error", func() {
				yaml := `apiVersion: v1
kind: Service
  invalid: indentation
`
				_, err := manifest.Parse(yaml)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with document missing kind", func() {
			It("should return an error", func() {
				yaml := `apiVersion: v1
metadata:
  name: web
`
				_, err := manifest.Parse(yaml)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Kind"))
			})
		})
	})

	Describe("Parse preserves metadata", func() {
		It("should preserve all metadata fields", func() {
			yaml := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
  labels:
    app: web
    io.kompose.service: web
  annotations:
    lissto.dev/class: workload
spec:
  replicas: 1
`
			objects, err := manifest.Parse(yaml)
			Expect(err).NotTo(HaveOccurred())
			Expect(objects).To(HaveLen(1))

			obj := objects[0]
			Expect(obj.GetName()).To(Equal("web"))
			Expect(obj.GetNamespace()).To(Equal("default"))
			Expect(obj.GetLabels()).To(HaveKeyWithValue("app", "web"))
			Expect(obj.GetLabels()).To(HaveKeyWithValue("io.kompose.service", "web"))
			Expect(obj.GetAnnotations()).To(HaveKeyWithValue("lissto.dev/class", "workload"))
		})
	})
})
