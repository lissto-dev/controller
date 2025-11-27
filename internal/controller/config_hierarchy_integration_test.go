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
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/lissto-dev/controller/internal/controller/testdata"
	"github.com/lissto-dev/controller/pkg/config"
)

var _ = Describe("Config Injection Hierarchy Tests", func() {
	var (
		testNamespace   string
		globalNamespace = "lissto-global"
		reconciler      *StackReconciler
		ctx             = context.Background()
	)

	BeforeEach(func() {
		testNamespace = fmt.Sprintf("test-hierarchy-%d", time.Now().UnixNano())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Create global namespace if not exists
		globalNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: globalNamespace},
		}
		_ = k8sClient.Create(ctx, globalNs) // May already exist

		// Initialize reconciler
		reconciler = &StackReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			Config: &config.Config{
				Namespaces: config.NamespacesConfig{
					Global: globalNamespace,
				},
			},
		}
	})

	AfterEach(func() {
		// Cleanup namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: testNamespace},
		}
		_ = k8sClient.Delete(ctx, ns)
	})

	Context("Variable Hierarchy", func() {
		It("should prioritize env > repo > global variables", func() {
			// Create Blueprint with repository annotation
			blueprint := testdata.NewBlueprint(testNamespace, "test-blueprint")
			blueprint.Annotations["lissto.dev/repository"] = "myorg/myapp"
			blueprint.Spec.DockerCompose = testdata.ComposeForHierarchyTest
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create global-scoped variable with unique name
			globalVarName := fmt.Sprintf("global-vars-%d", time.Now().UnixNano())
			globalVar := testdata.NewGlobalScopedVariable(globalNamespace, globalVarName, map[string]string{
				"VAR1": "global",
				"VAR2": "global",
				"VAR3": "global",
			})
			Expect(k8sClient.Create(ctx, globalVar)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, globalVar)
			}()

			// Create repo-scoped variable
			repoVar := testdata.NewRepoScopedVariable(testNamespace, "repo-vars", "myorg/myapp", map[string]string{
				"VAR1": "repo",
				"VAR2": "repo",
			})
			Expect(k8sClient.Create(ctx, repoVar)).To(Succeed())

			// Create env-scoped variable
			envVar := testdata.NewEnvScopedVariable(testNamespace, "env-vars", "staging", map[string]string{
				"VAR1": "env",
			})
			Expect(k8sClient.Create(ctx, envVar)).To(Succeed())

			// Create Stack
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			stack.Spec.Env = "staging"
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has correct env vars with hierarchy
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env
			envMap := make(map[string]string)
			for _, e := range env {
				if e.Value != "" {
					envMap[e.Name] = e.Value
				}
			}

			// Verify hierarchy: env > repo > global
			Expect(envMap["VAR1"]).To(Equal("env"), "VAR1 should come from env scope (highest priority)")
			Expect(envMap["VAR2"]).To(Equal("repo"), "VAR2 should come from repo scope (medium priority)")
			Expect(envMap["VAR3"]).To(Equal("global"), "VAR3 should come from global scope (lowest priority)")
		})

		It("should only match env-scoped variables when env matches", func() {
			// Create Blueprint
			blueprint := testdata.NewBlueprint(testNamespace, "test-blueprint")
			blueprint.Spec.DockerCompose = testdata.ComposeForHierarchyTest
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create env-scoped variable for "prod"
			envVar := testdata.NewEnvScopedVariable(testNamespace, "prod-vars", "prod", map[string]string{
				"PROD_VAR": "should-not-inject",
			})
			Expect(k8sClient.Create(ctx, envVar)).To(Succeed())

			// Create Stack with env="dev" (different from variable's env)
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			stack.Spec.Env = "dev" // Different env
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment does NOT have the prod variable
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env
			for _, e := range env {
				Expect(e.Name).NotTo(Equal("PROD_VAR"), "Should not inject variable from non-matching env scope")
			}
		})

		It("should only match repo-scoped variables when repository matches", func() {
			// Create Blueprint with repository annotation
			blueprint := testdata.NewBlueprint(testNamespace, "test-blueprint")
			blueprint.Annotations["lissto.dev/repository"] = "myorg/repo2"
			blueprint.Spec.DockerCompose = testdata.ComposeForHierarchyTest
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create repo-scoped variable for different repository
			repoVar := testdata.NewRepoScopedVariable(testNamespace, "repo1-vars", "myorg/repo1", map[string]string{
				"REPO_VAR": "should-not-inject",
			})
			Expect(k8sClient.Create(ctx, repoVar)).To(Succeed())

			// Create Stack
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment does NOT have the repo variable
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env
			for _, e := range env {
				Expect(e.Name).NotTo(Equal("REPO_VAR"), "Should not inject variable from non-matching repo scope")
			}
		})

		It("should inject global variables into all stacks", func() {
			// Create Blueprint
			blueprint := testdata.NewBlueprint(testNamespace, "test-blueprint")
			blueprint.Spec.DockerCompose = testdata.ComposeForHierarchyTest
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create global-scoped variable with unique name
			globalVarName := fmt.Sprintf("global-vars-%d", time.Now().UnixNano())
			globalVar := testdata.NewGlobalScopedVariable(globalNamespace, globalVarName, map[string]string{
				"GLOBAL_VAR": "from-global",
			})
			Expect(k8sClient.Create(ctx, globalVar)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, globalVar)
			}()

			// Create Stack (any env/repo)
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			stack.Spec.Env = "any-env"
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment HAS the global variable
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env
			envMap := make(map[string]string)
			for _, e := range env {
				if e.Value != "" {
					envMap[e.Name] = e.Value
				}
			}

			Expect(envMap["GLOBAL_VAR"]).To(Equal("from-global"), "Should inject global variable into any stack")
		})
	})

	Context("Secret Hierarchy - Key Level", func() {
		It("should resolve keys independently with env > repo > global", func() {
			// Create Blueprint with repository annotation
			blueprint := testdata.NewBlueprint(testNamespace, "test-blueprint")
			blueprint.Annotations["lissto.dev/repository"] = "myorg/myapp"
			blueprint.Spec.DockerCompose = testdata.ComposeForHierarchyTest
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create K8s Secrets with actual data (unique names)
			globalSecretName := fmt.Sprintf("global-secrets-data-%d", time.Now().UnixNano())
			globalK8sSecret := testdata.NewKubernetesSecret(globalNamespace, globalSecretName, map[string]string{
				"KEY1": "global-key1",
				"KEY2": "global-key2",
				"KEY3": "global-key3",
			})
			Expect(k8sClient.Create(ctx, globalK8sSecret)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, globalK8sSecret)
			}()

			repoK8sSecret := testdata.NewKubernetesSecret(testNamespace, "repo-secrets-data", map[string]string{
				"KEY1": "repo-key1",
				"KEY2": "repo-key2",
			})
			Expect(k8sClient.Create(ctx, repoK8sSecret)).To(Succeed())

			envK8sSecret := testdata.NewKubernetesSecret(testNamespace, "env-secrets-data", map[string]string{
				"KEY1": "env-key1",
			})
			Expect(k8sClient.Create(ctx, envK8sSecret)).To(Succeed())

			// Create LisstoSecrets referencing the keys (unique names)
			globalLisstoSecretName := fmt.Sprintf("global-secrets-%d", time.Now().UnixNano())
			globalSecret := testdata.NewGlobalScopedSecret(globalNamespace, globalLisstoSecretName, []string{"KEY1", "KEY2", "KEY3"}, globalSecretName)
			Expect(k8sClient.Create(ctx, globalSecret)).To(Succeed())
			defer func() {
				_ = k8sClient.Delete(ctx, globalSecret)
			}()

			repoSecret := testdata.NewRepoScopedSecret(testNamespace, "repo-secrets", "myorg/myapp", []string{"KEY1", "KEY2"}, "repo-secrets-data")
			Expect(k8sClient.Create(ctx, repoSecret)).To(Succeed())

			envSecret := testdata.NewEnvScopedSecret(testNamespace, "env-secrets", "staging", []string{"KEY1"}, "env-secrets-data")
			Expect(k8sClient.Create(ctx, envSecret)).To(Succeed())

			// Create Stack
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			stack.Spec.Env = "staging"
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has secret references
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env

			// Verify all three keys are present as secretKeyRef
			secretRefs := make(map[string]bool)
			for _, e := range env {
				if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
					secretRefs[e.Name] = true
				}
			}

			Expect(secretRefs["KEY1"]).To(BeTrue(), "KEY1 should be injected from env scope")
			Expect(secretRefs["KEY2"]).To(BeTrue(), "KEY2 should be injected from repo scope")
			Expect(secretRefs["KEY3"]).To(BeTrue(), "KEY3 should be injected from global scope")
		})
	})

	Context("Docker Compose Environment Variables", func() {
		It("should preserve env vars from docker-compose", func() {
			// Create Blueprint with compose that has env vars
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, "test-blueprint", testdata.ComposeWithEnvVars)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap with env vars (simulating kompose output)
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentWithEnvVarsManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create Stack (no LisstoVariables)
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has compose env vars
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env
			envMap := make(map[string]string)
			for _, e := range env {
				if e.Value != "" {
					envMap[e.Name] = e.Value
				}
			}

			Expect(envMap["COMPOSE_VAR"]).To(Equal("from-compose"))
			Expect(envMap["PORT"]).To(Equal("8080"))
			Expect(envMap["DEBUG"]).To(Equal("false"))
		})

		It("should combine compose env vars with LisstoVariables (no conflicts)", func() {
			// Create Blueprint with compose that has env vars
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, "test-blueprint", testdata.ComposeWithEnvVars)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap with env vars (simulating kompose output)
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentWithEnvVarsManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create LisstoVariable with non-conflicting keys
			lisstoVar := testdata.NewEnvScopedVariable(testNamespace, "env-vars", "test", map[string]string{
				"INJECTED_VAR": "from-lissto",
				"ANOTHER_VAR":  "also-from-lissto",
			})
			Expect(k8sClient.Create(ctx, lisstoVar)).To(Succeed())

			// Create Stack
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has both compose and lissto vars
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env
			envMap := make(map[string]string)
			for _, e := range env {
				if e.Value != "" {
					envMap[e.Name] = e.Value
				}
			}

			// Compose vars
			Expect(envMap["COMPOSE_VAR"]).To(Equal("from-compose"))
			Expect(envMap["PORT"]).To(Equal("8080"))
			Expect(envMap["DEBUG"]).To(Equal("false"))

			// Lissto vars
			Expect(envMap["INJECTED_VAR"]).To(Equal("from-lissto"))
			Expect(envMap["ANOTHER_VAR"]).To(Equal("also-from-lissto"))
		})

		It("should let LisstoVariables override compose env vars (last one wins)", func() {
			// Create Blueprint with compose that has env vars
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, "test-blueprint", testdata.ComposeWithConflictingVars)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap with conflicting vars (simulating kompose output)
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentWithConflictingVarsManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create LisstoVariable with conflicting keys
			lisstoVar := testdata.NewEnvScopedVariable(testNamespace, "env-vars", "test", map[string]string{
				"DATABASE_HOST": "db.prod.com", // Conflicts with compose
				"DEBUG":         "true",        // Conflicts with compose
			})
			Expect(k8sClient.Create(ctx, lisstoVar)).To(Succeed())

			// Create Stack
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has deduplicated env vars with LisstoVariable values winning
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env

			// Build env map and count occurrences
			envMap := make(map[string]string)
			nameCount := make(map[string]int)
			for _, e := range env {
				nameCount[e.Name]++
				if e.Value != "" {
					envMap[e.Name] = e.Value
				}
			}

			// Verify deduplication - each key appears only once
			Expect(nameCount["DATABASE_HOST"]).To(Equal(1), "DATABASE_HOST should appear exactly once (deduplicated)")
			Expect(nameCount["DEBUG"]).To(Equal(1), "DEBUG should appear exactly once (deduplicated)")
			Expect(nameCount["PORT"]).To(Equal(1), "PORT should appear exactly once")

			// Verify LisstoVariable values override compose values
			Expect(envMap["DATABASE_HOST"]).To(Equal("db.prod.com"), "DATABASE_HOST should be from LisstoVariable (overrode compose)")
			Expect(envMap["DEBUG"]).To(Equal("true"), "DEBUG should be from LisstoVariable (overrode compose)")
			Expect(envMap["PORT"]).To(Equal("8080"), "PORT should be from compose (no conflict)")
		})
	})

	Context("Combined Variable and Secret Injection", func() {
		It("should let LisstoSecrets override compose env vars", func() {
			// Create Blueprint with compose that has an env var
			blueprint := testdata.NewBlueprintWithCompose(testNamespace, "test-blueprint", testdata.ComposeWithConflictingVars)
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap with DATABASE_HOST from compose
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentWithConflictingVarsManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create K8s Secret with DATABASE_HOST value
			k8sSecret := testdata.NewKubernetesSecret(testNamespace, "test-secret-data", map[string]string{
				"DATABASE_HOST": "db.from-secret.com",
			})
			Expect(k8sClient.Create(ctx, k8sSecret)).To(Succeed())

			// Create LisstoSecret that will override compose's DATABASE_HOST
			lisstoSecret := testdata.NewEnvScopedSecret(testNamespace, "env-secrets", "test", []string{"DATABASE_HOST"}, "test-secret-data")
			Expect(k8sClient.Create(ctx, lisstoSecret)).To(Succeed())

			// Create Stack
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has DATABASE_HOST as secretKeyRef (not compose value)
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env

			// Count occurrences and verify it's a secret reference
			databaseHostCount := 0
			isSecretRef := false
			hasValue := false

			for _, e := range env {
				if e.Name == "DATABASE_HOST" {
					databaseHostCount++
					if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
						isSecretRef = true
					}
					if e.Value != "" {
						hasValue = true
					}
				}
			}

			// Verify secret overrides compose
			Expect(databaseHostCount).To(Equal(1), "DATABASE_HOST should appear exactly once (deduplicated)")
			Expect(isSecretRef).To(BeTrue(), "DATABASE_HOST should be from LisstoSecret (overrode compose)")
			Expect(hasValue).To(BeFalse(), "DATABASE_HOST should NOT have a value (it's a secretKeyRef)")
		})

		It("should follow complete hierarchy: compose < variable < secret", func() {
			// Test the complete override chain:
			// compose has: SECRET_PASSWORD=compose-insecure
			// variable has: SECRET_PASSWORD=variable-password  (overrides compose)
			// secret has: SECRET_PASSWORD key  (overrides both)

			// Create manifests with SECRET_PASSWORD from compose
			manifestWithSecret := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  labels:
    io.kompose.service: web
spec:
  replicas: 1
  selector:
    matchLabels:
      io.kompose.service: web
  template:
    metadata:
      labels:
        io.kompose.service: web
    spec:
      containers:
      - name: web
        image: nginx:latest
        env:
        - name: SECRET_PASSWORD
          value: compose-insecure
        - name: OTHER_VAR
          value: keep-me
`
			blueprint := testdata.NewBlueprint(testNamespace, "test-blueprint")
			blueprint.Spec.DockerCompose = testdata.ComposeForHierarchyTest
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", manifestWithSecret)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create K8s Secret
			k8sSecret := testdata.NewKubernetesSecret(testNamespace, "test-secret-data", map[string]string{
				"SECRET_PASSWORD": "secret-from-vault",
			})
			Expect(k8sClient.Create(ctx, k8sSecret)).To(Succeed())

			// Create LisstoVariable (would override compose if secret didn't exist)
			lisstoVar := testdata.NewEnvScopedVariable(testNamespace, "env-vars", "test", map[string]string{
				"SECRET_PASSWORD": "variable-password",
			})
			Expect(k8sClient.Create(ctx, lisstoVar)).To(Succeed())

			// Create LisstoSecret (overrides both compose and variable)
			lisstoSecret := testdata.NewEnvScopedSecret(testNamespace, "env-secrets", "test", []string{"SECRET_PASSWORD"}, "test-secret-data")
			Expect(k8sClient.Create(ctx, lisstoSecret)).To(Succeed())

			// Create Stack
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env

			// Build map and verify hierarchy
			secretPasswordCount := 0
			otherVarCount := 0
			isSecretRef := false

			for _, e := range env {
				if e.Name == "SECRET_PASSWORD" {
					secretPasswordCount++
					// Should be secretKeyRef, not value
					if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
						isSecretRef = true
					}
				}
				if e.Name == "OTHER_VAR" {
					otherVarCount++
				}
			}

			// Verify complete hierarchy: secret wins
			Expect(secretPasswordCount).To(Equal(1), "SECRET_PASSWORD should appear exactly once (deduplicated)")
			Expect(isSecretRef).To(BeTrue(), "SECRET_PASSWORD should be from LisstoSecret (highest priority)")
			Expect(otherVarCount).To(Equal(1), "OTHER_VAR from compose should be preserved (no conflict)")
		})

		It("should inject both variables and secrets", func() {
			// Create Blueprint
			blueprint := testdata.NewBlueprint(testNamespace, "test-blueprint")
			blueprint.Spec.DockerCompose = testdata.ComposeForHierarchyTest
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Create manifests ConfigMap
			manifests := testdata.NewManifestsConfigMapWithContent(testNamespace, "test-manifests", testdata.DeploymentManifest)
			Expect(k8sClient.Create(ctx, manifests)).To(Succeed())

			// Create K8s Secret
			k8sSecret := testdata.NewKubernetesSecret(testNamespace, "test-secret-data", map[string]string{
				"SECRET_KEY": "secret-value",
			})
			Expect(k8sClient.Create(ctx, k8sSecret)).To(Succeed())

			// Create LisstoVariable
			lisstoVar := testdata.NewEnvScopedVariable(testNamespace, "env-vars", "test", map[string]string{
				"PUBLIC_VAR": "public-value",
			})
			Expect(k8sClient.Create(ctx, lisstoVar)).To(Succeed())

			// Create LisstoSecret
			lisstoSecret := testdata.NewEnvScopedSecret(testNamespace, "env-secrets", "test", []string{"SECRET_KEY"}, "test-secret-data")
			Expect(k8sClient.Create(ctx, lisstoSecret)).To(Succeed())

			// Create Stack
			stack := testdata.NewStack(testNamespace, "test-stack",
				fmt.Sprintf("%s/test-blueprint", testNamespace), "test-manifests")
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify Deployment has both value-based and valueFrom-based entries
			deployment := &appsv1.Deployment{}
			Eventually(func() error {
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      "web",
					Namespace: testNamespace,
				}, deployment)
			}, 5*time.Second, 500*time.Millisecond).Should(Succeed())

			env := deployment.Spec.Template.Spec.Containers[0].Env

			hasPublicVar := false
			hasSecretRef := false

			for _, e := range env {
				if e.Name == "PUBLIC_VAR" && e.Value == "public-value" {
					hasPublicVar = true
				}
				if e.Name == "SECRET_KEY" && e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil {
					hasSecretRef = true
				}
			}

			Expect(hasPublicVar).To(BeTrue(), "Should have LisstoVariable injected as value")
			Expect(hasSecretRef).To(BeTrue(), "Should have LisstoSecret injected as secretKeyRef")
		})
	})
})
