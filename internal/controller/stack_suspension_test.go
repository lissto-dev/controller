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
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/stack/suspension"
	"github.com/lissto-dev/controller/pkg/config"
)

var _ = Describe("Stack Suspension", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("Suspension Logic", func() {
		Describe("CategorizeResources", func() {
			It("should separate state and workload resources correctly", func() {
				stack := &envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{Services: []string{"worker"}},
					},
				}

				objects := makeTestResources()
				state, apply, delete := suspension.CategorizeResources(stack, objects)

				Expect(state).To(HaveLen(1))
				Expect(state[0].GetName()).To(Equal("postgres-data"))

				Expect(apply).To(HaveLen(1))
				Expect(apply[0].GetName()).To(Equal("web"))

				Expect(delete).To(HaveLen(1))
				Expect(delete[0].GetName()).To(Equal("worker"))
			})

			It("should suspend all workloads with wildcard", func() {
				stack := &envv1alpha1.Stack{
					Spec: envv1alpha1.StackSpec{
						Suspension: &envv1alpha1.SuspensionSpec{Services: []string{"*"}},
					},
				}

				objects := makeTestResources()
				state, apply, delete := suspension.CategorizeResources(stack, objects)

				Expect(state).To(HaveLen(1))
				Expect(apply).To(BeEmpty())
				Expect(delete).To(HaveLen(2))
			})
		})

		Describe("DetermineStackPhase", func() {
			DescribeTable("phase transitions",
				func(suspended bool, currentPhase envv1alpha1.StackPhase, terminated bool, expected envv1alpha1.StackPhase) {
					stack := &envv1alpha1.Stack{
						Status: envv1alpha1.StackStatus{Phase: currentPhase},
					}
					if suspended {
						stack.Spec.Suspension = &envv1alpha1.SuspensionSpec{Services: []string{"*"}}
					}
					Expect(suspension.DetermineStackPhase(stack, terminated)).To(Equal(expected))
				},
				Entry("running -> suspending", true, envv1alpha1.StackPhaseRunning, false, envv1alpha1.StackPhaseSuspending),
				Entry("suspending -> suspended", true, envv1alpha1.StackPhaseSuspending, true, envv1alpha1.StackPhaseSuspended),
				Entry("suspended -> resuming", false, envv1alpha1.StackPhaseSuspended, true, envv1alpha1.StackPhaseResuming),
				Entry("resuming -> running", false, envv1alpha1.StackPhaseResuming, true, envv1alpha1.StackPhaseRunning),
			)
		})
	})

	Context("Integration", func() {
		var (
			ctx        context.Context
			reconciler *StackReconciler
			testNs     string
		)

		BeforeEach(func() {
			ctx = context.Background()
			testNs = "test-suspension-" + randString(5)

			// Create test namespace
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNs}}
			Expect(k8sClient.Create(ctx, ns)).To(Succeed())

			reconciler = &StackReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Config:   &config.Config{},
				Recorder: record.NewFakeRecorder(10),
			}
		})

		AfterEach(func() {
			// Cleanup namespace
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: testNs}, ns); err == nil {
				_ = k8sClient.Delete(ctx, ns)
			}
		})

		It("should transition stack through suspension phases", func() {
			// Create a stack
			stack := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-stack",
					Namespace: testNs,
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "test-bp",
					ManifestsConfigMapRef: "test-manifests",
					Env:                   "test",
					Images:                map[string]envv1alpha1.ImageInfo{},
				},
			}
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Verify stack was created
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: stack.Name, Namespace: testNs}, stack)
				return err == nil
			}, timeout, interval).Should(BeTrue())

			// Reconcile to add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: stack.Name, Namespace: testNs},
			})
			Expect(err).NotTo(HaveOccurred())

			// Update stack to suspend
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: stack.Name, Namespace: testNs}, stack)).To(Succeed())
			stack.Spec.Suspension = &envv1alpha1.SuspensionSpec{Services: []string{"*"}}
			Expect(k8sClient.Update(ctx, stack)).To(Succeed())

			// Cleanup
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: stack.Name, Namespace: testNs}, stack)).To(Succeed())
			stack.Finalizers = nil
			_ = k8sClient.Update(ctx, stack)
			_ = k8sClient.Delete(ctx, stack)

			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: stack.Name, Namespace: testNs}, stack)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})
	})
})

func makeTestResources() []*unstructured.Unstructured {
	return []*unstructured.Unstructured{
		makeUnstructured("PersistentVolumeClaim", "postgres-data", "state", "postgres"),
		makeUnstructured("Deployment", "web", "workload", "web"),
		makeUnstructured("Deployment", "worker", "workload", "worker"),
	}
}

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}

// unstructured is imported from k8s.io/apimachinery/pkg/apis/meta/v1/unstructured
type unstructured = struct {
	Object map[string]interface{}
}

func makeUnstructured(kind, name, class, service string) *unstructured {
	obj := &unstructured{
		Object: map[string]interface{}{
			"kind": kind,
			"metadata": map[string]interface{}{
				"name": name,
				"annotations": map[string]interface{}{
					"lissto.dev/class": class,
				},
			},
		},
	}
	if service != "" {
		obj.Object["metadata"].(map[string]interface{})["labels"] = map[string]interface{}{
			"io.kompose.service": service,
		}
	}
	return obj
}
