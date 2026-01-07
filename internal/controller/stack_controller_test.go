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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/controller/testdata"
	"github.com/lissto-dev/controller/pkg/config"
)

var _ = Describe("Stack Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		stack := &envv1alpha1.Stack{}

		BeforeEach(func() {
			By("creating the required resources")

			// Create Blueprint using testdata fixture
			blueprint := testdata.NewBlueprint("default", "test-blueprint")
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "test-blueprint", Namespace: "default"}, &envv1alpha1.Blueprint{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())
			}

			// Create ConfigMap using testdata fixture
			configMap := testdata.NewManifestsConfigMap("default", "test-manifests")
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-manifests", Namespace: "default"}, &corev1.ConfigMap{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, configMap)).To(Succeed())
			}

			// Create Stack using testdata fixture
			err = k8sClient.Get(ctx, typeNamespacedName, stack)
			if err != nil && errors.IsNotFound(err) {
				resource := testdata.NewStack("default", resourceName, "default/test-blueprint", "test-manifests")
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup the specific resource instances")

			// Delete Stack
			resource := &envv1alpha1.Stack{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}

			// Delete ConfigMap
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-manifests", Namespace: "default"}, cm)
			if err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}

			// Delete Blueprint
			bp := &envv1alpha1.Blueprint{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "test-blueprint", Namespace: "default"}, bp)
			if err == nil {
				_ = k8sClient.Delete(ctx, bp)
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &StackReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: &config.Config{
					Namespaces: config.NamespacesConfig{
						Global: "lissto-global",
					},
				},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the Stack was reconciled
			reconciledStack := &envv1alpha1.Stack{}
			err = k8sClient.Get(ctx, typeNamespacedName, reconciledStack)
			Expect(err).NotTo(HaveOccurred())

			// Verify finalizer was added
			Expect(reconciledStack.Finalizers).To(ContainElement("stack.lissto.dev/config-cleanup"))
		})
	})

	Context("When cleaning up orphan resources", func() {
		const stackName = "cleanup-test-stack"
		const namespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      stackName,
			Namespace: namespace,
		}

		var controllerReconciler *StackReconciler

		BeforeEach(func() {
			controllerReconciler = &StackReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: &config.Config{
					Namespaces: config.NamespacesConfig{
						Global: "lissto-global",
					},
				},
			}

			By("creating the required Blueprint")
			blueprint := testdata.NewBlueprint(namespace, "cleanup-test-blueprint")
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "cleanup-test-blueprint", Namespace: namespace}, &envv1alpha1.Blueprint{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())
			}

			By("creating the required ConfigMap")
			configMap := testdata.NewManifestsConfigMap(namespace, "cleanup-test-manifests")
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "cleanup-test-manifests", Namespace: namespace}, &corev1.ConfigMap{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, configMap)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup test resources")

			// Clean up any orphan resources first
			cleanupOrphanResources(ctx, namespace, stackName)

			// Delete Stack if it exists (remove finalizer first if needed)
			stack := &envv1alpha1.Stack{}
			err := k8sClient.Get(ctx, typeNamespacedName, stack)
			if err == nil {
				// Remove finalizer to allow deletion
				stack.Finalizers = nil
				_ = k8sClient.Update(ctx, stack)
				_ = k8sClient.Delete(ctx, stack)
			}

			// Delete ConfigMap
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "cleanup-test-manifests", Namespace: namespace}, cm)
			if err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}

			// Delete Blueprint (remove finalizer first if needed)
			bp := &envv1alpha1.Blueprint{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "cleanup-test-blueprint", Namespace: namespace}, bp)
			if err == nil {
				bp.Finalizers = nil
				_ = k8sClient.Update(ctx, bp)
				_ = k8sClient.Delete(ctx, bp)
			}
		})

		It("should cleanup orphan secrets with lissto.dev/stack label", func() {
			By("Creating a Stack and reconciling it")
			stack := testdata.NewStack(namespace, stackName, "default/cleanup-test-blueprint", "cleanup-test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Reconcile to add finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Get the stack to have its UID
			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())

			By("Creating an orphan secret with lissto.dev/stack label but no owner reference")
			orphanSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stackName + "-orphan-secret",
					Namespace: namespace,
					Labels: map[string]string{
						"lissto.dev/stack":      stackName,
						"lissto.dev/managed-by": "stack-controller",
					},
				},
				Data: map[string][]byte{
					"test-key": []byte("test-value"),
				},
			}
			Expect(k8sClient.Create(ctx, orphanSecret)).To(Succeed())

			// Verify the orphan secret exists
			createdSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: orphanSecret.Name, Namespace: namespace}, createdSecret)).To(Succeed())

			By("Deleting the Stack")
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())

			By("Reconciling to trigger cleanup and complete deletion")
			// With deletion protection, we may need multiple reconciles
			Eventually(func() bool {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return false
				}
				// Check if stack is gone (finalizer removed)
				err = k8sClient.Get(ctx, typeNamespacedName, stack)
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Stack should be deleted")

			By("Verifying the orphan secret was cleaned up")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: orphanSecret.Name, Namespace: namespace}, &corev1.Secret{})
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Orphan secret should be deleted")
		})

		It("should cleanup orphan services with lissto.dev/stack label", func() {
			By("Creating a Stack and reconciling it")
			stack := testdata.NewStack(namespace, stackName, "default/cleanup-test-blueprint", "cleanup-test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Reconcile to add finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Get the stack to have its UID
			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())

			By("Creating an orphan service with lissto.dev/stack label but no owner reference")
			orphanService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stackName + "-orphan-service",
					Namespace: namespace,
					Labels: map[string]string{
						"lissto.dev/stack": stackName,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{Port: 80, Protocol: corev1.ProtocolTCP},
					},
					Selector: map[string]string{
						"app": "test",
					},
				},
			}
			Expect(k8sClient.Create(ctx, orphanService)).To(Succeed())

			// Verify the orphan service exists
			createdService := &corev1.Service{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: orphanService.Name, Namespace: namespace}, createdService)).To(Succeed())

			By("Deleting the Stack")
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())

			By("Reconciling to trigger cleanup and complete deletion")
			Eventually(func() bool {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return false
				}
				err = k8sClient.Get(ctx, typeNamespacedName, stack)
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Stack should be deleted")

			By("Verifying the orphan service was cleaned up")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: orphanService.Name, Namespace: namespace}, &corev1.Service{})
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Orphan service should be deleted")
		})

		It("should cleanup multiple orphan resources of different types", func() {
			By("Creating a Stack and reconciling it")
			stack := testdata.NewStack(namespace, stackName, "default/cleanup-test-blueprint", "cleanup-test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Reconcile to add finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Get the stack to have its UID
			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())

			By("Creating orphan resources with lissto.dev/stack label but no owner reference")

			// Orphan Secret
			orphanSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stackName + "-multi-orphan-secret",
					Namespace: namespace,
					Labels: map[string]string{
						"lissto.dev/stack":      stackName,
						"lissto.dev/managed-by": "stack-controller",
					},
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			Expect(k8sClient.Create(ctx, orphanSecret)).To(Succeed())

			// Orphan Service
			orphanService := &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stackName + "-multi-orphan-service",
					Namespace: namespace,
					Labels: map[string]string{
						"lissto.dev/stack": stackName,
					},
				},
				Spec: corev1.ServiceSpec{
					Ports:    []corev1.ServicePort{{Port: 8080, Protocol: corev1.ProtocolTCP}},
					Selector: map[string]string{"app": "test"},
				},
			}
			Expect(k8sClient.Create(ctx, orphanService)).To(Succeed())

			// Orphan ConfigMap (instead of PVC which has issues in envtest)
			orphanConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stackName + "-multi-orphan-configmap",
					Namespace: namespace,
					Labels: map[string]string{
						"lissto.dev/stack": stackName,
					},
				},
				Data: map[string]string{"key": "value"},
			}
			Expect(k8sClient.Create(ctx, orphanConfigMap)).To(Succeed())

			// Verify all orphan resources exist
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: orphanSecret.Name, Namespace: namespace}, &corev1.Secret{})).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: orphanService.Name, Namespace: namespace}, &corev1.Service{})).To(Succeed())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: orphanConfigMap.Name, Namespace: namespace}, &corev1.ConfigMap{})).To(Succeed())

			By("Deleting the Stack")
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())

			By("Reconciling to trigger cleanup and complete deletion")
			Eventually(func() bool {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return false
				}
				err = k8sClient.Get(ctx, typeNamespacedName, stack)
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Stack should be deleted")

			By("Verifying orphan secret and service were cleaned up")
			// Note: ConfigMap is not in the cleanup list, so it won't be deleted
			Eventually(func() bool {
				secretErr := k8sClient.Get(ctx, types.NamespacedName{Name: orphanSecret.Name, Namespace: namespace}, &corev1.Secret{})
				serviceErr := k8sClient.Get(ctx, types.NamespacedName{Name: orphanService.Name, Namespace: namespace}, &corev1.Service{})
				return errors.IsNotFound(secretErr) && errors.IsNotFound(serviceErr)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Orphan secret and service should be deleted")

			// Cleanup the ConfigMap manually (it's not in the cleanup list)
			_ = k8sClient.Delete(ctx, orphanConfigMap)
		})

		It("should not cleanup resources belonging to other stacks", func() {
			By("Creating a Stack and reconciling it")
			stack := testdata.NewStack(namespace, stackName, "default/cleanup-test-blueprint", "cleanup-test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Reconcile to add finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			By("Creating a secret belonging to a different stack")
			otherStackSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-stack-secret",
					Namespace: namespace,
					Labels: map[string]string{
						"lissto.dev/stack":      "other-stack-name",
						"lissto.dev/managed-by": "stack-controller",
					},
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			Expect(k8sClient.Create(ctx, otherStackSecret)).To(Succeed())

			By("Deleting the Stack")
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())

			By("Reconciling to trigger cleanup and complete deletion")
			Eventually(func() bool {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return false
				}
				err = k8sClient.Get(ctx, typeNamespacedName, stack)
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Stack should be deleted")

			By("Verifying the other stack's secret was NOT deleted")
			Consistently(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{Name: otherStackSecret.Name, Namespace: namespace}, &corev1.Secret{})
			}, 2*time.Second, 100*time.Millisecond).Should(Succeed(), "Other stack's secret should NOT be deleted")

			// Cleanup the other stack's secret
			_ = k8sClient.Delete(ctx, otherStackSecret)
		})

		It("should cleanup resources with owner reference to the stack", func() {
			By("Creating a Stack and reconciling it")
			stack := testdata.NewStack(namespace, stackName, "default/cleanup-test-blueprint", "cleanup-test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Reconcile to add finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Get the stack to have its UID
			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())

			By("Creating a secret with owner reference to the stack")
			ownedSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      stackName + "-owned-secret",
					Namespace: namespace,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "env.lissto.dev/v1alpha1",
							Kind:       "Stack",
							Name:       stack.Name,
							UID:        stack.UID,
						},
					},
				},
				Data: map[string][]byte{"key": []byte("value")},
			}
			Expect(k8sClient.Create(ctx, ownedSecret)).To(Succeed())

			// Verify the owned secret exists
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: ownedSecret.Name, Namespace: namespace}, &corev1.Secret{})).To(Succeed())

			By("Deleting the Stack")
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())

			By("Reconciling to trigger cleanup and complete deletion")
			Eventually(func() bool {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return false
				}
				err = k8sClient.Get(ctx, typeNamespacedName, stack)
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Stack should be deleted")

			By("Verifying the owned secret was cleaned up")
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{Name: ownedSecret.Name, Namespace: namespace}, &corev1.Secret{})
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Owned secret should be deleted")
		})
	})
})

var _ = Describe("Stack Deletion Protection", func() {
	Context("formatResourceList", func() {
		It("should format resources correctly", func() {
			Expect(formatResourceList([]string{})).To(Equal(""))
			Expect(formatResourceList([]string{"Deployment/web"})).To(Equal("Deployment/web"))
			Expect(formatResourceList([]string{"Deployment/web", "Service/web"})).To(Equal("Deployment/web, Service/web"))
		})

		It("should truncate list with more than 5 resources", func() {
			resources := []string{"A", "B", "C", "D", "E", "F", "G"}
			result := formatResourceList(resources)
			Expect(result).To(ContainSubstring("... and 2 more"))
		})
	})

	Context("getBackoffDuration", func() {
		It("should use exponential backoff from wait.Backoff", func() {
			// Uses standard K8s wait.Backoff - just verify it increases
			b0 := getBackoffDuration(deletionBackoff, 0)
			b1 := getBackoffDuration(deletionBackoff, 1)
			b2 := getBackoffDuration(deletionBackoff, 2)
			Expect(b1).To(BeNumerically(">", b0))
			Expect(b2).To(BeNumerically(">", b1))
			// Verify cap at 5 minutes
			b100 := getBackoffDuration(deletionBackoff, 100)
			Expect(b100).To(Equal(5 * time.Minute))
		})
	})

	Context("When deleting a Stack with child resources", func() {
		const stackName = "deletion-protection-test"
		const namespace = "default"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      stackName,
			Namespace: namespace,
		}

		var controllerReconciler *StackReconciler

		BeforeEach(func() {
			controllerReconciler = &StackReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: &fakeRecorder{},
				Config: &config.Config{
					Namespaces: config.NamespacesConfig{
						Global: "lissto-global",
					},
				},
			}

			By("creating the required Blueprint")
			blueprint := testdata.NewBlueprint(namespace, "deletion-test-blueprint")
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "deletion-test-blueprint", Namespace: namespace}, &envv1alpha1.Blueprint{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())
			}

			By("creating the required ConfigMap")
			configMap := testdata.NewManifestsConfigMap(namespace, "deletion-test-manifests")
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "deletion-test-manifests", Namespace: namespace}, &corev1.ConfigMap{})
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, configMap)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("Cleanup test resources")

			// Delete Stack if it exists (remove finalizer first if needed)
			stack := &envv1alpha1.Stack{}
			err := k8sClient.Get(ctx, typeNamespacedName, stack)
			if err == nil {
				// Remove finalizer to allow deletion
				stack.Finalizers = nil
				_ = k8sClient.Update(ctx, stack)
				_ = k8sClient.Delete(ctx, stack)
			}

			// Delete ConfigMap
			cm := &corev1.ConfigMap{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "deletion-test-manifests", Namespace: namespace}, cm)
			if err == nil {
				_ = k8sClient.Delete(ctx, cm)
			}

			// Delete Blueprint (remove finalizer first if needed)
			bp := &envv1alpha1.Blueprint{}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: "deletion-test-blueprint", Namespace: namespace}, bp)
			if err == nil {
				bp.Finalizers = nil
				_ = k8sClient.Update(ctx, bp)
				_ = k8sClient.Delete(ctx, bp)
			}

			// Clean up any orphan resources
			cleanupOrphanResources(ctx, namespace, stackName)
		})

		It("should wait for child resources before removing finalizer", func() {
			By("Creating a Stack and reconciling it")
			stack := testdata.NewStack(namespace, stackName, "default/deletion-test-blueprint", "deletion-test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Reconcile to add finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Get the stack to have its UID
			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())
			Expect(stack.Finalizers).To(ContainElement(stackFinalizerName))

			By("Creating a child Pod with a finalizer (to prevent immediate deletion)")
			childPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       stackName + "-child-pod",
					Namespace:  namespace,
					Finalizers: []string{"test.lissto.dev/block-deletion"},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "env.lissto.dev/v1alpha1",
							Kind:       "Stack",
							Name:       stack.Name,
							UID:        stack.UID,
						},
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "busybox"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, childPod)).To(Succeed())

			By("Deleting the Stack")
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())

			By("Reconciling - should requeue because child resource exists (has finalizer)")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "Should requeue with backoff")

			By("Verifying Stack still has finalizer")
			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())
			Expect(stack.Finalizers).To(ContainElement(stackFinalizerName))

			By("Verifying Deleting condition is set")
			deletingCondition := findCondition(stack.Status.Conditions, envv1alpha1.StackConditionTypeDeleting)
			Expect(deletingCondition).NotTo(BeNil())
			Expect(deletingCondition.Status).To(Equal(metav1.ConditionTrue))
			Expect(deletingCondition.Reason).To(Equal(envv1alpha1.StackReasonWaitingForChildren))

			By("Removing the finalizer from the child Pod to allow deletion")
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: childPod.Name, Namespace: namespace}, childPod)).To(Succeed())
			childPod.Finalizers = nil
			Expect(k8sClient.Update(ctx, childPod)).To(Succeed())

			By("Reconciling again - should complete deletion")
			Eventually(func() bool {
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
				if err != nil {
					return false
				}
				// Check if stack is gone
				err = k8sClient.Get(ctx, typeNamespacedName, stack)
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(), "Stack should be deleted after child resources are gone")
		})

		It("should update retry count annotation on each reconcile", func() {
			By("Creating a Stack and reconciling it")
			stack := testdata.NewStack(namespace, stackName, "default/deletion-test-blueprint", "deletion-test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Reconcile to add finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Get the stack to have its UID
			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())

			By("Creating a child Pod with a finalizer (to prevent immediate deletion)")
			childPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       stackName + "-retry-test-pod",
					Namespace:  namespace,
					Finalizers: []string{"test.lissto.dev/block-deletion"},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "env.lissto.dev/v1alpha1",
							Kind:       "Stack",
							Name:       stack.Name,
							UID:        stack.UID,
						},
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "busybox"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, childPod)).To(Succeed())

			By("Deleting the Stack")
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())

			By("First reconcile - retry count should be 1")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())
			Expect(stack.Annotations[deletionRetryCountAnnotation]).To(Equal("1"))

			By("Second reconcile - retry count should be 2")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())
			Expect(stack.Annotations[deletionRetryCountAnnotation]).To(Equal("2"))

			// Cleanup - remove finalizer from pod
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: childPod.Name, Namespace: namespace}, childPod)).To(Succeed())
			childPod.Finalizers = nil
			_ = k8sClient.Update(ctx, childPod)
		})

		It("should increase backoff exponentially", func() {
			By("Creating a Stack and reconciling it")
			stack := testdata.NewStack(namespace, stackName, "default/deletion-test-blueprint", "deletion-test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Reconcile to add finalizer
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Get the stack to have its UID
			Expect(k8sClient.Get(ctx, typeNamespacedName, stack)).To(Succeed())

			By("Creating a child Pod with a finalizer (to prevent immediate deletion)")
			childPod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:       stackName + "-backoff-test-pod",
					Namespace:  namespace,
					Finalizers: []string{"test.lissto.dev/block-deletion"},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "env.lissto.dev/v1alpha1",
							Kind:       "Stack",
							Name:       stack.Name,
							UID:        stack.UID,
						},
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "test", Image: "busybox"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, childPod)).To(Succeed())

			By("Deleting the Stack")
			Expect(k8sClient.Delete(ctx, stack)).To(Succeed())

			By("First reconcile - backoff should be 5s")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Second))

			By("Second reconcile - backoff should be 10s")
			result, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(10 * time.Second))

			By("Third reconcile - backoff should be 20s")
			result, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(20 * time.Second))

			// Cleanup - remove finalizer from pod
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: childPod.Name, Namespace: namespace}, childPod)).To(Succeed())
			childPod.Finalizers = nil
			_ = k8sClient.Update(ctx, childPod)
		})
	})
})

// fakeRecorder is a simple fake event recorder for testing
type fakeRecorder struct{}

func (f *fakeRecorder) Event(object runtime.Object, eventtype, reason, message string) {}
func (f *fakeRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
}
func (f *fakeRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
}

// findCondition finds a condition by type in a slice of conditions
func findCondition(conditions []metav1.Condition, conditionType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

// Helper function to cleanup orphan resources after tests
func cleanupOrphanResources(ctx context.Context, namespace, stackName string) {
	listOpts := client.InNamespace(namespace)

	// Cleanup secrets
	secretList := &corev1.SecretList{}
	_ = k8sClient.List(ctx, secretList, listOpts)
	for i := range secretList.Items {
		secret := &secretList.Items[i]
		if labels := secret.GetLabels(); labels != nil && labels["lissto.dev/stack"] == stackName {
			_ = k8sClient.Delete(ctx, secret)
		}
	}

	// Cleanup services
	serviceList := &corev1.ServiceList{}
	_ = k8sClient.List(ctx, serviceList, listOpts)
	for i := range serviceList.Items {
		svc := &serviceList.Items[i]
		if labels := svc.GetLabels(); labels != nil && labels["lissto.dev/stack"] == stackName {
			_ = k8sClient.Delete(ctx, svc)
		}
	}

	// Cleanup PVCs
	pvcList := &corev1.PersistentVolumeClaimList{}
	_ = k8sClient.List(ctx, pvcList, listOpts)
	for i := range pvcList.Items {
		pvc := &pvcList.Items[i]
		if labels := pvc.GetLabels(); labels != nil && labels["lissto.dev/stack"] == stackName {
			_ = k8sClient.Delete(ctx, pvc)
		}
	}

	// Cleanup deployments
	deploymentList := &appsv1.DeploymentList{}
	_ = k8sClient.List(ctx, deploymentList, listOpts)
	for i := range deploymentList.Items {
		dep := &deploymentList.Items[i]
		if labels := dep.GetLabels(); labels != nil && labels["lissto.dev/stack"] == stackName {
			_ = k8sClient.Delete(ctx, dep)
		}
	}
}
