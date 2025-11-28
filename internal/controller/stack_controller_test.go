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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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
			Namespace: "default", // TODO(user):Modify as needed
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
})
