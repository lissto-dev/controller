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

package suspension_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/stack/suspension"
)

var _ = Describe("Suspension", func() {
	Describe("ShouldSuspendService", func() {
		Context("with nil suspension", func() {
			It("should return false", func() {
				stack := &envv1alpha1.Stack{}
				Expect(suspension.ShouldSuspendService(stack, "web")).To(BeFalse())
			})
		})

		Context("with empty services list", func() {
			It("should return false", func() {
				stack := &envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{
							Services: []string{},
						},
					},
				}
				Expect(suspension.ShouldSuspendService(stack, "web")).To(BeFalse())
			})
		})

		Context("with wildcard", func() {
			It("should suspend all services", func() {
				stack := &envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{
							Services: []string{"*"},
						},
					},
				}
				Expect(suspension.ShouldSuspendService(stack, "web")).To(BeTrue())
				Expect(suspension.ShouldSuspendService(stack, "worker")).To(BeTrue())
				Expect(suspension.ShouldSuspendService(stack, "any-service")).To(BeTrue())
			})
		})

		Context("with specific services", func() {
			It("should only suspend listed services", func() {
				stack := &envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{
							Services: []string{"worker", "batch"},
						},
					},
				}
				Expect(suspension.ShouldSuspendService(stack, "worker")).To(BeTrue())
				Expect(suspension.ShouldSuspendService(stack, "batch")).To(BeTrue())
				Expect(suspension.ShouldSuspendService(stack, "web")).To(BeFalse())
			})
		})
	})

	Describe("IsStackSuspendedAll", func() {
		Context("with nil suspension", func() {
			It("should return false", func() {
				stack := &envv1alpha1.Stack{}
				Expect(suspension.IsStackSuspendedAll(stack)).To(BeFalse())
			})
		})

		Context("with wildcard", func() {
			It("should return true", func() {
				stack := &envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{
							Services: []string{"*"},
						},
					},
				}
				Expect(suspension.IsStackSuspendedAll(stack)).To(BeTrue())
			})
		})

		Context("with specific services only", func() {
			It("should return false", func() {
				stack := &envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{
							Services: []string{"worker", "batch"},
						},
					},
				}
				Expect(suspension.IsStackSuspendedAll(stack)).To(BeFalse())
			})
		})
	})

	Describe("GetResourceClass", func() {
		Context("with state class", func() {
			It("should return state", func() {
				obj := makeObjectWithAnnotations("PVC", "data", map[string]string{
					envv1alpha1.ResourceClassAnnotation: envv1alpha1.ResourceClassState,
				})
				class, err := suspension.GetResourceClass(obj)
				Expect(err).NotTo(HaveOccurred())
				Expect(class).To(Equal(envv1alpha1.ResourceClassState))
			})
		})

		Context("with workload class", func() {
			It("should return workload", func() {
				obj := makeObjectWithAnnotations("Deployment", "web", map[string]string{
					envv1alpha1.ResourceClassAnnotation: envv1alpha1.ResourceClassWorkload,
				})
				class, err := suspension.GetResourceClass(obj)
				Expect(err).NotTo(HaveOccurred())
				Expect(class).To(Equal(envv1alpha1.ResourceClassWorkload))
			})
		})

		Context("with missing annotations", func() {
			It("should return error", func() {
				obj := makeObjectWithAnnotations("Deployment", "web", nil)
				_, err := suspension.GetResourceClass(obj)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with missing class annotation", func() {
			It("should return error", func() {
				obj := makeObjectWithAnnotations("Deployment", "web", map[string]string{
					"other": "annotation",
				})
				_, err := suspension.GetResourceClass(obj)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with invalid class value", func() {
			It("should return error", func() {
				obj := makeObjectWithAnnotations("Deployment", "web", map[string]string{
					envv1alpha1.ResourceClassAnnotation: "invalid",
				})
				_, err := suspension.GetResourceClass(obj)
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("CategorizeResources", func() {
		It("should correctly categorize resources", func() {
			stack := &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspension: &envv1alpha1.SuspensionSpec{
						Services: []string{"worker"},
					},
				},
			}

			objects := []*unstructured.Unstructured{
				makeResourceWithClass("PVC", "data", "web", envv1alpha1.ResourceClassState),
				makeResourceWithClass("Deployment", "web", "web", envv1alpha1.ResourceClassWorkload),
				makeResourceWithClass("Deployment", "worker", "worker", envv1alpha1.ResourceClassWorkload),
				makeResourceWithClass("Service", "web", "web", envv1alpha1.ResourceClassWorkload),
			}

			state, workloadsApply, workloadsDelete := suspension.CategorizeResources(stack, objects)

			Expect(state).To(HaveLen(1))
			Expect(workloadsApply).To(HaveLen(2))
			Expect(workloadsDelete).To(HaveLen(1))
			Expect(workloadsDelete[0].GetName()).To(Equal("worker"))
		})
	})

	Describe("DetermineStackPhase", func() {
		DescribeTable("should determine correct phase",
			func(stack *envv1alpha1.Stack, allTerminated bool, expected envv1alpha1.StackPhase) {
				result := suspension.DetermineStackPhase(stack, allTerminated)
				Expect(result).To(Equal(expected))
			},
			Entry("suspended all, terminated",
				&envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{Services: []string{"*"}},
					},
				},
				true,
				envv1alpha1.StackPhaseSuspended,
			),
			Entry("suspended all, not terminated",
				&envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{Services: []string{"*"}},
					},
				},
				false,
				envv1alpha1.StackPhaseSuspending,
			),
			Entry("resuming from suspended",
				&envv1alpha1.Stack{
					Status: envv1alpha1.StackStatus{
						Phase: envv1alpha1.StackPhaseSuspended,
					},
				},
				true,
				envv1alpha1.StackPhaseResuming,
			),
			Entry("running from resuming",
				&envv1alpha1.Stack{
					Status: envv1alpha1.StackStatus{
						Phase: envv1alpha1.StackPhaseResuming,
					},
				},
				true,
				envv1alpha1.StackPhaseRunning,
			),
			Entry("running from empty phase",
				&envv1alpha1.Stack{},
				true,
				envv1alpha1.StackPhaseRunning,
			),
		)
	})

	Describe("ValidateResourceAnnotations", func() {
		Context("with all valid resources", func() {
			It("should return no error", func() {
				objects := []*unstructured.Unstructured{
					makeResourceWithClass("PVC", "data", "web", envv1alpha1.ResourceClassState),
					makeResourceWithClass("Deployment", "web", "web", envv1alpha1.ResourceClassWorkload),
				}
				err := suspension.ValidateResourceAnnotations(objects)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("with missing annotation", func() {
			It("should return error", func() {
				objects := []*unstructured.Unstructured{
					makeResourceWithClass("PVC", "data", "web", envv1alpha1.ResourceClassState),
					makeObjectWithAnnotations("Deployment", "web", nil),
				}
				err := suspension.ValidateResourceAnnotations(objects)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with invalid class", func() {
			It("should return error", func() {
				objects := []*unstructured.Unstructured{
					makeObjectWithAnnotations("Deployment", "web", map[string]string{
						envv1alpha1.ResourceClassAnnotation: "invalid",
					}),
				}
				err := suspension.ValidateResourceAnnotations(objects)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("with empty objects", func() {
			It("should return no error", func() {
				err := suspension.ValidateResourceAnnotations([]*unstructured.Unstructured{})
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})

	Describe("GetServiceName", func() {
		Context("with service label", func() {
			It("should return the service name", func() {
				obj := makeResourceWithClass("Deployment", "web", "my-service", envv1alpha1.ResourceClassWorkload)
				Expect(suspension.GetServiceName(obj)).To(Equal("my-service"))
			})
		})

		Context("without labels", func() {
			It("should return empty string", func() {
				obj := &unstructured.Unstructured{
					Object: map[string]interface{}{
						"apiVersion": "v1",
						"kind":       "Deployment",
						"metadata": map[string]interface{}{
							"name": "web",
						},
					},
				}
				Expect(suspension.GetServiceName(obj)).To(BeEmpty())
			})
		})
	})
})

// Helper functions

func makeObjectWithAnnotations(kind, name string, annotations map[string]string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name": name,
			},
		},
	}
	if annotations != nil {
		obj.SetAnnotations(annotations)
	}
	return obj
}

func makeResourceWithClass(kind, name, service, class string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "v1",
			"kind":       kind,
			"metadata": map[string]interface{}{
				"name": name,
				"labels": map[string]interface{}{
					"io.kompose.service": service,
				},
				"annotations": map[string]interface{}{
					envv1alpha1.ResourceClassAnnotation: class,
				},
			},
		},
	}
}
