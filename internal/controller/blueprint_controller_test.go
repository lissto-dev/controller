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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/pkg/config"
)

var _ = Describe("Blueprint Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	var testConfig *config.Config

	BeforeEach(func() {
		testConfig = &config.Config{
			Namespaces: config.NamespacesConfig{
				Global:          "lissto-global",
				DeveloperPrefix: "dev-",
			},
		}
	})

	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		blueprint := &envv1alpha1.Blueprint{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Blueprint")
			err := k8sClient.Get(ctx, typeNamespacedName, blueprint)
			if err != nil && errors.IsNotFound(err) {
				resource := &envv1alpha1.Blueprint{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: envv1alpha1.BlueprintSpec{
						DockerCompose: "version: '3'",
						Hash:          "abc123",
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &envv1alpha1.Blueprint{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				// Remove finalizer if present to allow deletion
				if controllerutil.ContainsFinalizer(resource, blueprintFinalizerName) {
					controllerutil.RemoveFinalizer(resource, blueprintFinalizerName)
					Expect(k8sClient.Update(ctx, resource)).To(Succeed())
				}
				By("Cleanup the specific resource instance Blueprint")
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should successfully reconcile the resource and add finalizer", func() {
			By("Reconciling the created resource")
			controllerReconciler := &BlueprintReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: testConfig,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking that the finalizer was added")
			updatedBlueprint := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, updatedBlueprint)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(updatedBlueprint, blueprintFinalizerName)).To(BeTrue())
		})
	})

	Context("Blueprint deletion protection - Global namespace", func() {
		var (
			ctx                  context.Context
			globalNamespace      *corev1.Namespace
			devNamespace         *corev1.Namespace
			globalBlueprint      *envv1alpha1.Blueprint
			stackInDevNamespace  *envv1alpha1.Stack
			controllerReconciler *BlueprintReconciler
		)

		BeforeEach(func() {
			ctx = context.Background()

			// Create global namespace
			globalNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "lissto-global",
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: globalNamespace.Name}, globalNamespace)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, globalNamespace)).To(Succeed())
			}

			// Create developer namespace
			devNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev-user1",
				},
			}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: devNamespace.Name}, devNamespace)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, devNamespace)).To(Succeed())
			}

			// Create global Blueprint
			globalBlueprint = &envv1alpha1.Blueprint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "global-blueprint",
					Namespace: "lissto-global",
				},
				Spec: envv1alpha1.BlueprintSpec{
					DockerCompose: "version: '3'\nservices:\n  web:\n    image: nginx",
					Hash:          "hash123",
				},
			}
			Expect(k8sClient.Create(ctx, globalBlueprint)).To(Succeed())

			controllerReconciler = &BlueprintReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: testConfig,
			}

			// Reconcile to add finalizer
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      globalBlueprint.Name,
					Namespace: globalBlueprint.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			// Clean up Stack if exists
			if stackInDevNamespace != nil {
				err := k8sClient.Delete(ctx, stackInDevNamespace)
				if err != nil && !errors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
				stackInDevNamespace = nil
			}

			// Clean up Blueprint
			if globalBlueprint != nil {
				bp := &envv1alpha1.Blueprint{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      globalBlueprint.Name,
					Namespace: globalBlueprint.Namespace,
				}, bp)
				if err == nil {
					// Remove finalizer to allow deletion
					if controllerutil.ContainsFinalizer(bp, blueprintFinalizerName) {
						controllerutil.RemoveFinalizer(bp, blueprintFinalizerName)
						_ = k8sClient.Update(ctx, bp)
					}
					_ = k8sClient.Delete(ctx, bp)
				}
				globalBlueprint = nil
			}
		})

		It("should block deletion when Stack in another namespace references global Blueprint", func() {
			By("Creating a Stack in dev namespace that references the global Blueprint")
			stackInDevNamespace = &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-stack",
					Namespace: "dev-user1",
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "global/global-blueprint",
					ManifestsConfigMapRef: "test-manifests",
					Env:                   "test-env",
					Images: map[string]envv1alpha1.ImageInfo{
						"web": {Digest: "nginx:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, stackInDevNamespace)).To(Succeed())

			By("Attempting to delete the global Blueprint")
			bp := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      globalBlueprint.Name,
				Namespace: globalBlueprint.Namespace,
			}, bp)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bp)).To(Succeed())

			By("Reconciling the Blueprint deletion")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      globalBlueprint.Name,
					Namespace: globalBlueprint.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "Should requeue when deletion is blocked")

			By("Verifying the Blueprint still exists with finalizer")
			updatedBp := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      globalBlueprint.Name,
				Namespace: globalBlueprint.Namespace,
			}, updatedBp)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(updatedBp, blueprintFinalizerName)).To(BeTrue())
		})

		It("should allow deletion when no Stacks reference the global Blueprint", func() {
			By("Attempting to delete the global Blueprint without any referencing Stacks")
			bp := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      globalBlueprint.Name,
				Namespace: globalBlueprint.Namespace,
			}, bp)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bp)).To(Succeed())

			By("Reconciling the Blueprint deletion")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      globalBlueprint.Name,
					Namespace: globalBlueprint.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the finalizer was removed")
			Eventually(func() bool {
				updatedBp := &envv1alpha1.Blueprint{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      globalBlueprint.Name,
					Namespace: globalBlueprint.Namespace,
				}, updatedBp)
				if errors.IsNotFound(err) {
					return true // Blueprint was deleted
				}
				if err != nil {
					return false
				}
				return !controllerutil.ContainsFinalizer(updatedBp, blueprintFinalizerName)
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Blueprint deletion protection - Developer namespace", func() {
		var (
			ctx                  context.Context
			devNamespace         *corev1.Namespace
			otherDevNamespace    *corev1.Namespace
			devBlueprint         *envv1alpha1.Blueprint
			stackInSameNamespace *envv1alpha1.Stack
			controllerReconciler *BlueprintReconciler
		)

		BeforeEach(func() {
			ctx = context.Background()

			// Create developer namespace
			devNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev-user2",
				},
			}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: devNamespace.Name}, devNamespace)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, devNamespace)).To(Succeed())
			}

			// Create another developer namespace
			otherDevNamespace = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: "dev-user3",
				},
			}
			err = k8sClient.Get(ctx, types.NamespacedName{Name: otherDevNamespace.Name}, otherDevNamespace)
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, otherDevNamespace)).To(Succeed())
			}

			// Create developer Blueprint
			devBlueprint = &envv1alpha1.Blueprint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dev-blueprint",
					Namespace: "dev-user2",
				},
				Spec: envv1alpha1.BlueprintSpec{
					DockerCompose: "version: '3'\nservices:\n  app:\n    image: myapp",
					Hash:          "devhash123",
				},
			}
			Expect(k8sClient.Create(ctx, devBlueprint)).To(Succeed())

			controllerReconciler = &BlueprintReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
				Config: testConfig,
			}

			// Reconcile to add finalizer
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      devBlueprint.Name,
					Namespace: devBlueprint.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		AfterEach(func() {
			// Clean up Stack if exists
			if stackInSameNamespace != nil {
				err := k8sClient.Delete(ctx, stackInSameNamespace)
				if err != nil && !errors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
				stackInSameNamespace = nil
			}

			// Clean up Blueprint
			if devBlueprint != nil {
				bp := &envv1alpha1.Blueprint{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      devBlueprint.Name,
					Namespace: devBlueprint.Namespace,
				}, bp)
				if err == nil {
					// Remove finalizer to allow deletion
					if controllerutil.ContainsFinalizer(bp, blueprintFinalizerName) {
						controllerutil.RemoveFinalizer(bp, blueprintFinalizerName)
						_ = k8sClient.Update(ctx, bp)
					}
					_ = k8sClient.Delete(ctx, bp)
				}
				devBlueprint = nil
			}
		})

		It("should block deletion when Stack in same namespace references developer Blueprint", func() {
			By("Creating a Stack in the same namespace that references the developer Blueprint")
			stackInSameNamespace = &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local-stack",
					Namespace: "dev-user2",
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "dev-blueprint", // Local reference (no global/ prefix)
					ManifestsConfigMapRef: "test-manifests",
					Env:                   "test-env",
					Images: map[string]envv1alpha1.ImageInfo{
						"app": {Digest: "myapp:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, stackInSameNamespace)).To(Succeed())

			By("Attempting to delete the developer Blueprint")
			bp := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      devBlueprint.Name,
				Namespace: devBlueprint.Namespace,
			}, bp)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bp)).To(Succeed())

			By("Reconciling the Blueprint deletion")
			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      devBlueprint.Name,
					Namespace: devBlueprint.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(BeNumerically(">", 0), "Should requeue when deletion is blocked")

			By("Verifying the Blueprint still exists with finalizer")
			updatedBp := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      devBlueprint.Name,
				Namespace: devBlueprint.Namespace,
			}, updatedBp)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(updatedBp, blueprintFinalizerName)).To(BeTrue())
		})

		It("should allow deletion when Stack in different namespace does not affect developer Blueprint", func() {
			By("Creating a Stack in a different namespace with same blueprint name")
			stackInOtherNamespace := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "other-stack",
					Namespace: "dev-user3",
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "dev-blueprint", // Same name but different namespace
					ManifestsConfigMapRef: "test-manifests",
					Env:                   "test-env",
					Images: map[string]envv1alpha1.ImageInfo{
						"app": {Digest: "myapp:latest"},
					},
				},
			}
			Expect(k8sClient.Create(ctx, stackInOtherNamespace)).To(Succeed())
			defer func() {
				err := k8sClient.Delete(ctx, stackInOtherNamespace)
				if err != nil && !errors.IsNotFound(err) {
					Expect(err).NotTo(HaveOccurred())
				}
			}()

			By("Attempting to delete the developer Blueprint")
			bp := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      devBlueprint.Name,
				Namespace: devBlueprint.Namespace,
			}, bp)).To(Succeed())
			Expect(k8sClient.Delete(ctx, bp)).To(Succeed())

			By("Reconciling the Blueprint deletion")
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      devBlueprint.Name,
					Namespace: devBlueprint.Namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the finalizer was removed (deletion allowed)")
			Eventually(func() bool {
				updatedBp := &envv1alpha1.Blueprint{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      devBlueprint.Name,
					Namespace: devBlueprint.Namespace,
				}, updatedBp)
				if errors.IsNotFound(err) {
					return true // Blueprint was deleted
				}
				if err != nil {
					return false
				}
				return !controllerutil.ContainsFinalizer(updatedBp, blueprintFinalizerName)
			}, timeout, interval).Should(BeTrue())
		})
	})

	Context("Helper function tests", func() {
		It("should correctly identify global namespace", func() {
			reconciler := &BlueprintReconciler{
				Config: testConfig,
			}

			Expect(reconciler.isGlobalNamespace("lissto-global")).To(BeTrue())
			Expect(reconciler.isGlobalNamespace("dev-user1")).To(BeFalse())
			Expect(reconciler.isGlobalNamespace("default")).To(BeFalse())
		})

		It("should correctly identify developer namespace", func() {
			reconciler := &BlueprintReconciler{
				Config: testConfig,
			}

			Expect(reconciler.isDeveloperNamespace("dev-user1")).To(BeTrue())
			Expect(reconciler.isDeveloperNamespace("dev-")).To(BeTrue())
			Expect(reconciler.isDeveloperNamespace("lissto-global")).To(BeFalse())
			Expect(reconciler.isDeveloperNamespace("default")).To(BeFalse())
		})

		It("should correctly check if stack references blueprint", func() {
			reconciler := &BlueprintReconciler{
				Config: testConfig,
			}

			// Global blueprint reference
			globalBlueprint := &envv1alpha1.Blueprint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-blueprint",
					Namespace: "lissto-global",
				},
			}

			stackWithGlobalRef := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-stack",
					Namespace: "dev-user1",
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference: "global/my-blueprint",
				},
			}
			Expect(reconciler.stackReferencesBlueprint(stackWithGlobalRef, globalBlueprint)).To(BeTrue())

			// Local blueprint reference
			localBlueprint := &envv1alpha1.Blueprint{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "local-blueprint",
					Namespace: "dev-user1",
				},
			}

			stackWithLocalRef := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-stack",
					Namespace: "dev-user1",
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference: "local-blueprint",
				},
			}
			Expect(reconciler.stackReferencesBlueprint(stackWithLocalRef, localBlueprint)).To(BeTrue())

			// Non-matching reference
			Expect(reconciler.stackReferencesBlueprint(stackWithLocalRef, globalBlueprint)).To(BeFalse())
		})
	})
})
