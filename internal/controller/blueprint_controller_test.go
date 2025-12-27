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
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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
		typeNamespacedName := types.NamespacedName{Name: resourceName, Namespace: "default"}

		BeforeEach(func() {
			err := k8sClient.Get(ctx, typeNamespacedName, &envv1alpha1.Blueprint{})
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &envv1alpha1.Blueprint{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec:       envv1alpha1.BlueprintSpec{DockerCompose: "version: '3'", Hash: "abc123"},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			cleanupBlueprint(ctx, typeNamespacedName)
		})

		It("should successfully reconcile the resource and add finalizer", func() {
			reconciler := &BlueprintReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Config: testConfig}
			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			bp := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, bp)).To(Succeed())
			Expect(controllerutil.ContainsFinalizer(bp, blueprintFinalizerName)).To(BeTrue())
		})
	})

	Context("Blueprint deletion protection", func() {
		var (
			ctx          context.Context
			reconciler   *BlueprintReconciler
			fakeRecorder *record.FakeRecorder
		)

		BeforeEach(func() {
			ctx = context.Background()
			ensureNamespace(ctx, "lissto-global")
			ensureNamespace(ctx, "dev-user1")
			ensureNamespace(ctx, "dev-user2")
			ensureNamespace(ctx, "dev-user3")

			fakeRecorder = record.NewFakeRecorder(10)
			reconciler = &BlueprintReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Config:   testConfig,
				Recorder: fakeRecorder,
			}
		})

		type testCase struct {
			blueprintNs   string
			blueprintName string
			stackNs       string
			stackName     string
			blueprintRef  string
			shouldBlock   bool
			checkEvent    bool
		}

		DescribeTable("deletion scenarios",
			func(tc testCase) {
				bpKey := types.NamespacedName{Name: tc.blueprintName, Namespace: tc.blueprintNs}

				By("Creating Blueprint")
				bp := &envv1alpha1.Blueprint{
					ObjectMeta: metav1.ObjectMeta{Name: tc.blueprintName, Namespace: tc.blueprintNs},
					Spec:       envv1alpha1.BlueprintSpec{DockerCompose: "version: '3'", Hash: "hash123"},
				}
				Expect(k8sClient.Create(ctx, bp)).To(Succeed())
				defer cleanupBlueprint(ctx, bpKey)

				By("Adding finalizer via reconcile")
				_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: bpKey})
				Expect(err).NotTo(HaveOccurred())

				var stack *envv1alpha1.Stack
				if tc.stackNs != "" {
					By("Creating referencing Stack")
					stack = &envv1alpha1.Stack{
						ObjectMeta: metav1.ObjectMeta{Name: tc.stackName, Namespace: tc.stackNs},
						Spec: envv1alpha1.StackSpec{
							BlueprintReference:    tc.blueprintRef,
							ManifestsConfigMapRef: "test-manifests",
							Env:                   "test-env",
							Images:                map[string]envv1alpha1.ImageInfo{"web": {Digest: "nginx:latest"}},
						},
					}
					Expect(k8sClient.Create(ctx, stack)).To(Succeed())
					defer func() { _ = k8sClient.Delete(ctx, stack) }()
				}

				By("Requesting Blueprint deletion")
				Expect(k8sClient.Get(ctx, bpKey, bp)).To(Succeed())
				Expect(k8sClient.Delete(ctx, bp)).To(Succeed())

				By("Reconciling deletion")
				_, err = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: bpKey})
				Expect(err).NotTo(HaveOccurred())

				if tc.shouldBlock {
					By("Verifying deletion was blocked")
					Expect(k8sClient.Get(ctx, bpKey, bp)).To(Succeed())
					Expect(controllerutil.ContainsFinalizer(bp, blueprintFinalizerName)).To(BeTrue())

					if tc.checkEvent {
						By("Verifying DeletionBlocked event")
						Eventually(func() bool {
							select {
							case event := <-fakeRecorder.Events:
								return strings.Contains(event, "DeletionBlocked") &&
									strings.Contains(event, tc.stackNs+"/"+tc.stackName)
							default:
								return false
							}
						}, timeout, interval).Should(BeTrue())
					}
				} else {
					By("Verifying deletion was allowed")
					Eventually(func() bool {
						err := k8sClient.Get(ctx, bpKey, &envv1alpha1.Blueprint{})
						return errors.IsNotFound(err)
					}, timeout, interval).Should(BeTrue())
				}
			},
			Entry("global blueprint with referencing stack blocks deletion", testCase{
				blueprintNs: "lissto-global", blueprintName: "global-bp",
				stackNs: "dev-user1", stackName: "test-stack", blueprintRef: "global/global-bp",
				shouldBlock: true, checkEvent: true,
			}),
			Entry("global blueprint without stacks allows deletion", testCase{
				blueprintNs: "lissto-global", blueprintName: "global-bp-no-stack",
				shouldBlock: false,
			}),
			Entry("dev blueprint with same-namespace stack blocks deletion", testCase{
				blueprintNs: "dev-user2", blueprintName: "dev-bp",
				stackNs: "dev-user2", stackName: "local-stack", blueprintRef: "dev-bp",
				shouldBlock: true, checkEvent: true,
			}),
			Entry("dev blueprint with different-namespace stack allows deletion", testCase{
				blueprintNs: "dev-user2", blueprintName: "dev-bp-isolated",
				stackNs: "dev-user3", stackName: "other-stack", blueprintRef: "dev-bp-isolated",
				shouldBlock: false,
			}),
		)
	})

	Context("Helper functions", func() {
		It("should correctly identify namespaces", func() {
			Expect(testConfig.Namespaces.IsGlobalNamespace("lissto-global")).To(BeTrue())
			Expect(testConfig.Namespaces.IsGlobalNamespace("dev-user1")).To(BeFalse())
			Expect(testConfig.Namespaces.IsDeveloperNamespace("dev-user1")).To(BeTrue())
			Expect(testConfig.Namespaces.IsDeveloperNamespace("lissto-global")).To(BeFalse())
		})

		It("should correctly check blueprint references", func() {
			reconciler := &BlueprintReconciler{Config: testConfig}

			globalBp := &envv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: "my-bp", Namespace: "lissto-global"}}
			localBp := &envv1alpha1.Blueprint{ObjectMeta: metav1.ObjectMeta{Name: "local-bp", Namespace: "dev-user1"}}

			stackGlobalRef := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{Namespace: "dev-user1"},
				Spec:       envv1alpha1.StackSpec{BlueprintReference: "global/my-bp"},
			}
			stackLocalRef := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{Namespace: "dev-user1"},
				Spec:       envv1alpha1.StackSpec{BlueprintReference: "local-bp"},
			}

			Expect(reconciler.stackReferencesBlueprint(stackGlobalRef, globalBp)).To(BeTrue())
			Expect(reconciler.stackReferencesBlueprint(stackLocalRef, localBp)).To(BeTrue())
			Expect(reconciler.stackReferencesBlueprint(stackLocalRef, globalBp)).To(BeFalse())
		})
	})
})

// Helper functions to reduce test duplication
func ensureNamespace(ctx context.Context, name string) {
	ns := &corev1.Namespace{}
	if errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)) {
		ExpectWithOffset(1, k8sClient.Create(ctx, &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		})).To(Succeed())
	}
}

func cleanupBlueprint(ctx context.Context, key types.NamespacedName) {
	bp := &envv1alpha1.Blueprint{}
	if err := k8sClient.Get(ctx, key, bp); err == nil {
		if controllerutil.ContainsFinalizer(bp, blueprintFinalizerName) {
			controllerutil.RemoveFinalizer(bp, blueprintFinalizerName)
			_ = k8sClient.Update(ctx, bp)
		}
		_ = k8sClient.Delete(ctx, bp)
	}
}
