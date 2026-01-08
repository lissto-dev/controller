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

package image_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/stack/image"
)

var _ = Describe("Image Injector", func() {
	var (
		injector *image.Injector
		ctx      context.Context
	)

	BeforeEach(func() {
		injector = image.NewInjector()
		ctx = context.Background()
	})

	Describe("Inject", func() {
		Context("when injecting into a deployment", func() {
			It("should inject the image successfully", func() {
				objects := []*unstructured.Unstructured{
					makeDeployment("web", "web"),
				}
				images := map[string]envv1alpha1.ImageInfo{
					"web": {Digest: "sha256:abc123"},
				}

				warnings := injector.Inject(ctx, objects, images)

				Expect(warnings).To(BeEmpty())
				Expect(getContainerImage(objects[0])).To(Equal("sha256:abc123"))
			})
		})

		Context("when deployment is missing service label", func() {
			It("should return a warning", func() {
				objects := []*unstructured.Unstructured{
					makeDeploymentWithoutLabel("web"),
				}
				images := map[string]envv1alpha1.ImageInfo{
					"web": {Digest: "sha256:abc123"},
				}

				warnings := injector.Inject(ctx, objects, images)

				Expect(warnings).To(HaveLen(1))
				Expect(warnings[0].Message).To(ContainSubstring("missing io.kompose.service label"))
			})
		})

		Context("when no image is specified for service", func() {
			It("should return a warning", func() {
				objects := []*unstructured.Unstructured{
					makeDeployment("web", "web"),
				}
				images := map[string]envv1alpha1.ImageInfo{}

				warnings := injector.Inject(ctx, objects, images)

				Expect(warnings).To(HaveLen(1))
				Expect(warnings[0].Message).To(ContainSubstring("No image specification found"))
			})
		})

		Context("when using custom container name", func() {
			It("should inject into the correct container", func() {
				objects := []*unstructured.Unstructured{
					makeDeploymentWithContainer("web", "web", "custom-container"),
				}
				images := map[string]envv1alpha1.ImageInfo{
					"web": {Digest: "sha256:abc123", ContainerName: "custom-container"},
				}

				warnings := injector.Inject(ctx, objects, images)

				Expect(warnings).To(BeEmpty())
				Expect(getContainerImage(objects[0])).To(Equal("sha256:abc123"))
			})
		})

		Context("when container name doesn't match", func() {
			It("should return a warning", func() {
				objects := []*unstructured.Unstructured{
					makeDeploymentWithContainer("web", "web", "other-container"),
				}
				images := map[string]envv1alpha1.ImageInfo{
					"web": {Digest: "sha256:abc123", ContainerName: "custom-container"},
				}

				warnings := injector.Inject(ctx, objects, images)

				Expect(warnings).To(HaveLen(1))
				Expect(warnings[0].Message).To(ContainSubstring("No container named"))
			})
		})

		Context("when processing non-workload resources", func() {
			It("should skip them without warnings", func() {
				objects := []*unstructured.Unstructured{
					makeService("web"),
				}
				images := map[string]envv1alpha1.ImageInfo{
					"web": {Digest: "sha256:abc123"},
				}

				warnings := injector.Inject(ctx, objects, images)

				Expect(warnings).To(BeEmpty())
			})
		})

		Context("when injecting into a pod", func() {
			It("should inject the image successfully", func() {
				objects := []*unstructured.Unstructured{
					makePod("web", "web"),
				}
				images := map[string]envv1alpha1.ImageInfo{
					"web": {Digest: "sha256:pod123"},
				}

				warnings := injector.Inject(ctx, objects, images)

				Expect(warnings).To(BeEmpty())
				Expect(getContainerImage(objects[0])).To(Equal("sha256:pod123"))
			})
		})
	})

	Describe("Warning", func() {
		Describe("String", func() {
			Context("with service name", func() {
				It("should include service in the message", func() {
					warning := image.Warning{
						Service:    "web",
						Deployment: "web-deployment",
						Message:    "test message",
					}
					Expect(warning.String()).To(Equal("Service 'web' (deployment 'web-deployment'): test message"))
				})
			})

			Context("without service name", func() {
				It("should only include deployment in the message", func() {
					warning := image.Warning{
						Deployment: "web-deployment",
						Message:    "test message",
					}
					Expect(warning.String()).To(Equal("Deployment 'web-deployment': test message"))
				})
			})
		})
	})
})

// Helper functions

func makeDeployment(name, serviceName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					"io.kompose.service": serviceName,
				},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  serviceName,
								"image": "placeholder:latest",
							},
						},
					},
				},
			},
		},
	}
}

func makeDeploymentWithoutLabel(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name":   name,
				"labels": map[string]interface{}{},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  name,
								"image": "placeholder:latest",
							},
						},
					},
				},
			},
		},
	}
}

func makeDeploymentWithContainer(name, serviceName, containerName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					"io.kompose.service": serviceName,
				},
			},
			"spec": map[string]interface{}{
				"template": map[string]interface{}{
					"spec": map[string]interface{}{
						"containers": []interface{}{
							map[string]interface{}{
								"name":  containerName,
								"image": "placeholder:latest",
							},
						},
					},
				},
			},
		},
	}
}

func makePod(name, serviceName string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Pod",
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					"io.kompose.service": serviceName,
				},
			},
			"spec": map[string]interface{}{
				"containers": []interface{}{
					map[string]interface{}{
						"name":  serviceName,
						"image": "placeholder:latest",
					},
				},
			},
		},
	}
}

func makeService(name string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       "Service",
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
}

func getContainerImage(obj *unstructured.Unstructured) string {
	var containersPath []string
	if obj.GetKind() == "Deployment" {
		containersPath = []string{"spec", "template", "spec", "containers"}
	} else {
		containersPath = []string{"spec", "containers"}
	}

	containers, _, _ := unstructured.NestedSlice(obj.Object, containersPath...)
	if len(containers) == 0 {
		return ""
	}

	containerMap, ok := containers[0].(map[string]interface{})
	if !ok {
		return ""
	}

	image, _, _ := unstructured.NestedString(containerMap, "image")
	return image
}
