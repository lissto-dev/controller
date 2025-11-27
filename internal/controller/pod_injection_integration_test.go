/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/internal/controller/testdata"
	"github.com/lissto-dev/controller/pkg/config"
)

var _ = Describe("Pod and Deployment Integration Tests", func() {
	var (
		testNamespace string
		stackName     = "test-stack"
		blueprintName = "test-blueprint"
		configMapName = "test-manifests"
	)

	var reconciler *StackReconciler

	BeforeEach(func() {
		// Generate unique namespace for each test
		testNamespace = fmt.Sprintf("test-pod-int-%d", time.Now().UnixNano())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Initialize reconciler with config
		reconciler = &StackReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Config: &config.Config{
				Namespaces: config.NamespacesConfig{
					Global: "lissto-global",
				},
			},
		}
	})

	AfterEach(func() {
		// Cleanup namespace (cascades to all resources)
		// Using a unique namespace per test, so we can delete without waiting
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		_ = k8sClient.Delete(ctx, ns)
		// Don't wait for deletion - each test uses unique namespace
	})

	Context("Image Injection Integration", func() {
		It("should inject images into both Deployment and Pod", func() {
			// Create Blueprint
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, blueprintName, testdata.MultiServiceCompose)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create ConfigMap with manifests
			configMap := testdata.NewDeploymentAndPodManifestsConfigMap(testNamespace, configMapName)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			// Create Stack
			stack := testdata.NewStackWithImages(testNamespace, stackName,
				fmt.Sprintf("%s/%s", testNamespace, blueprintName), configMapName)
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      stackName,
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment was created and has injected image
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			Expect(deployment.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(deployment.Spec.Template.Spec.Containers[0].Image).To(
				Equal("nginx@sha256:abc123def456"),
				"Deployment should have injected image digest")

			// Verify Pod was created and has injected image
			pod := &corev1.Pod{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "migrate",
					Namespace: testNamespace,
				}, pod)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			Expect(pod.Spec.Containers).To(HaveLen(1))
			Expect(pod.Spec.Containers[0].Image).To(
				Equal("migrate@sha256:xyz789uvw012"),
				"Pod should have injected image digest")
			Expect(pod.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
		})
	})

	Context("Config Injection Integration", func() {
		It("should inject variables into both Deployment and Pod", func() {
			// Create LisstoVariable for env-level config
			variable := &envv1alpha1.LisstoVariable{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vars",
					Namespace: testNamespace,
				},
				Spec: envv1alpha1.LisstoVariableSpec{
					Scope: "env",
					Env:   "test",
					Data: map[string]string{
						"ENV":       "production",
						"LOG_LEVEL": "info",
					},
				},
			}
			Expect(k8sClient.Create(ctx, variable)).To(Succeed())

			// Create Blueprint
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, blueprintName, testdata.MultiServiceCompose)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create ConfigMap
			configMap := testdata.NewDeploymentAndPodManifestsConfigMap(testNamespace, configMapName)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			// Create Stack
			stack := testdata.NewStackWithImages(testNamespace, stackName,
				fmt.Sprintf("%s/%s", testNamespace, blueprintName), configMapName)
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      stackName,
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has env vars
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			envVars := deployment.Spec.Template.Spec.Containers[0].Env
			Expect(envVars).To(ContainElement(
				corev1.EnvVar{Name: "ENV", Value: "production"}))
			Expect(envVars).To(ContainElement(
				corev1.EnvVar{Name: "LOG_LEVEL", Value: "info"}))

			// Verify Pod has env vars
			pod := &corev1.Pod{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "migrate",
					Namespace: testNamespace,
				}, pod)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			podEnvVars := pod.Spec.Containers[0].Env
			Expect(podEnvVars).To(ContainElement(
				corev1.EnvVar{Name: "ENV", Value: "production"}))
			Expect(podEnvVars).To(ContainElement(
				corev1.EnvVar{Name: "LOG_LEVEL", Value: "info"}))
		})

		It("should inject secrets into both Deployment and Pod", func() {
			// Create Secret for testing
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secret-data",
					Namespace: testNamespace,
				},
				StringData: map[string]string{
					"API_KEY":     "secret-key-123",
					"DB_PASSWORD": "secure-password",
				},
			}
			Expect(k8sClient.Create(ctx, secret)).To(Succeed())

			// Create LisstoSecret
			lisstoSecret := &envv1alpha1.LisstoSecret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-secrets",
					Namespace: testNamespace,
				},
				Spec: envv1alpha1.LisstoSecretSpec{
					Scope:     "env",
					Env:       "test",
					SecretRef: "test-secret-data",
					Keys:      []string{"API_KEY", "DB_PASSWORD"},
				},
			}
			Expect(k8sClient.Create(ctx, lisstoSecret)).To(Succeed())

			// Create Blueprint
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, blueprintName, testdata.MultiServiceCompose)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create ConfigMap
			configMap := testdata.NewDeploymentAndPodManifestsConfigMap(testNamespace, configMapName)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			// Create Stack
			stack := testdata.NewStackWithImages(testNamespace, stackName,
				fmt.Sprintf("%s/%s", testNamespace, blueprintName), configMapName)
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      stackName,
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has secret refs
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			envVars := deployment.Spec.Template.Spec.Containers[0].Env
			var apiKeyEnv *corev1.EnvVar
			for _, env := range envVars {
				if env.Name == "API_KEY" {
					apiKeyEnv = &env
					break
				}
			}
			Expect(apiKeyEnv).NotTo(BeNil(), "Should have API_KEY env var")
			Expect(apiKeyEnv.ValueFrom).NotTo(BeNil())
			Expect(apiKeyEnv.ValueFrom.SecretKeyRef.Name).To(
				Equal(stackName + "-config-secrets"))
			Expect(apiKeyEnv.ValueFrom.SecretKeyRef.Key).To(Equal("API_KEY"))

			// Verify Pod has secret refs
			pod := &corev1.Pod{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "migrate",
					Namespace: testNamespace,
				}, pod)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			podEnvVars := pod.Spec.Containers[0].Env
			var podApiKeyEnv *corev1.EnvVar
			for _, env := range podEnvVars {
				if env.Name == "API_KEY" {
					podApiKeyEnv = &env
					break
				}
			}
			Expect(podApiKeyEnv).NotTo(BeNil(), "Pod should have API_KEY env var")
			Expect(podApiKeyEnv.ValueFrom).NotTo(BeNil())
			Expect(podApiKeyEnv.ValueFrom.SecretKeyRef.Name).To(
				Equal(stackName + "-config-secrets"))
		})
	})

	Context("Owner References", func() {
		It("should set owner references on both Pod and Deployment", func() {
			// Create Blueprint
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, blueprintName, testdata.MultiServiceCompose)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create ConfigMap
			configMap := testdata.NewDeploymentAndPodManifestsConfigMap(testNamespace, configMapName)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			// Create Stack
			stack := testdata.NewStackWithImages(testNamespace, stackName,
				fmt.Sprintf("%s/%s", testNamespace, blueprintName), configMapName)
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Get Stack UID for verification
			createdStack := &envv1alpha1.Stack{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      stackName,
				Namespace: testNamespace,
			}, createdStack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      stackName,
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has owner reference
			deployment := &appsv1.Deployment{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
				if err != nil {
					return false
				}
				// Check owner references
				for _, ref := range deployment.GetOwnerReferences() {
					if ref.UID == createdStack.UID {
						return true
					}
				}
				return false
			}, 10*time.Second, 500*time.Millisecond).Should(BeTrue(),
				"Deployment should have Stack as owner")

			// Verify Pod has owner reference
			pod := &corev1.Pod{}
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "migrate",
					Namespace: testNamespace,
				}, pod)
				if err != nil {
					return false
				}
				// Check owner references
				for _, ref := range pod.GetOwnerReferences() {
					if ref.UID == createdStack.UID {
						return true
					}
				}
				return false
			}, 10*time.Second, 500*time.Millisecond).Should(BeTrue(),
				"Pod should have Stack as owner")
		})
	})

	Context("Stack Deletion", func() {
		It("should set owner references for cleanup on Stack deletion", func() {
			// Create all resources
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, blueprintName, testdata.MultiServiceCompose)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			configMap := testdata.NewDeploymentAndPodManifestsConfigMap(testNamespace, configMapName)
			Expect(k8sClient.Create(ctx, configMap)).To(Succeed())

			stack := testdata.NewStackWithImages(testNamespace, stackName,
				fmt.Sprintf("%s/%s", testNamespace, blueprintName), configMapName)
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Get Stack UID for verification
			createdStack := &envv1alpha1.Stack{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      stackName,
				Namespace: testNamespace,
			}, createdStack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      stackName,
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify resources exist
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			pod := &corev1.Pod{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "migrate",
					Namespace: testNamespace,
				}, pod)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			// Verify both resources have owner references pointing to Stack
			// This ensures cascade deletion will work when Stack is deleted
			Expect(deployment.GetOwnerReferences()).NotTo(BeEmpty(),
				"Deployment should have owner references")
			hasDeploymentOwner := false
			for _, ref := range deployment.GetOwnerReferences() {
				if ref.UID == createdStack.UID && ref.Kind == "Stack" {
					hasDeploymentOwner = true
					break
				}
			}
			Expect(hasDeploymentOwner).To(BeTrue(),
				"Deployment should have Stack as owner for cascade deletion")

			Expect(pod.GetOwnerReferences()).NotTo(BeEmpty(),
				"Pod should have owner references")
			hasPodOwner := false
			for _, ref := range pod.GetOwnerReferences() {
				if ref.UID == createdStack.UID && ref.Kind == "Stack" {
					hasPodOwner = true
					break
				}
			}
			Expect(hasPodOwner).To(BeTrue(),
				"Pod should have Stack as owner for cascade deletion")
		})
	})
})
