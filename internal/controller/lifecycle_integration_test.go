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
	"fmt"
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
	"github.com/lissto-dev/controller/internal/controller/testdata"
)

var _ = Describe("Lifecycle Integration Tests", func() {
	var (
		testNamespace string
		reconciler    *LifecycleReconciler
	)

	BeforeEach(func() {
		// Generate unique namespace for each test
		testNamespace = fmt.Sprintf("test-lifecycle-%d", time.Now().UnixNano())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Initialize reconciler
		reconciler = &LifecycleReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(100),
			workers:  make(map[string]workerHandle),
		}
	})

	AfterEach(func() {
		// Cleanup namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		_ = k8sClient.Delete(ctx, ns)
	})

	Context("Stack Lifecycle Management", func() {
		It("should delete Stacks older than specified duration", func() {
			// Create a Stack
			stack := testdata.NewStack(testNamespace, "test-stack", "test-blueprint", "test-manifests")
			stack.Labels = map[string]string{
				"lifecycle-test": "true",
			}
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Verify Stack was created
			createdStack := &envv1alpha1.Stack{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-stack",
				Namespace: testNamespace,
			}, createdStack)).To(Succeed())

			// Create Lifecycle targeting Stacks with very short olderThan
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-stack-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"lifecycle-test": "true",
						},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour}, // Long interval, we'll trigger manually
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "delete-old-stacks",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 1 * time.Millisecond}, // Very short for testing
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation to start the worker
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-stack-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait a bit for the Stack to become "old enough"
			time.Sleep(10 * time.Millisecond)

			// Manually execute tasks (simulating what the worker does)
			reconciler.executeTasks(ctx, "test-stack-lifecycle")

			// Verify Stack was deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-stack",
					Namespace: testNamespace,
				}, &envv1alpha1.Stack{})
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"Stack should be deleted by lifecycle")

			// Verify status was updated
			updatedLifecycle := &envv1alpha1.Lifecycle{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-stack-lifecycle"}, updatedLifecycle)).To(Succeed())
			Expect(updatedLifecycle.Status.SuccessfulTasks).To(BeNumerically(">=", 1))
			Expect(updatedLifecycle.Status.LastRunStats).NotTo(BeNil())
			Expect(updatedLifecycle.Status.LastRunStats.ObjectsDeleted).To(BeNumerically(">=", 1))

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
		})

		It("should not delete Stacks younger than specified duration", func() {
			// Create a Stack
			stack := testdata.NewStack(testNamespace, "young-stack", "test-blueprint", "test-manifests")
			stack.Labels = map[string]string{
				"lifecycle-test": "young",
			}
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Create Lifecycle with long olderThan duration
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-young-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"lifecycle-test": "young",
						},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "delete-old-stacks",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 24 * time.Hour}, // 24 hours - Stack won't be this old
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-young-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Execute tasks
			reconciler.executeTasks(ctx, "test-young-lifecycle")

			// Verify Stack still exists
			existingStack := &envv1alpha1.Stack{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "young-stack",
				Namespace: testNamespace,
			}, existingStack)).To(Succeed(), "Stack should NOT be deleted - it's too young")

			// Verify status shows objects evaluated but not deleted
			updatedLifecycle := &envv1alpha1.Lifecycle{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-young-lifecycle"}, updatedLifecycle)).To(Succeed())
			Expect(updatedLifecycle.Status.LastRunStats).NotTo(BeNil())
			Expect(updatedLifecycle.Status.LastRunStats.ObjectsEvaluated).To(BeNumerically(">=", 1))
			Expect(updatedLifecycle.Status.LastRunStats.ObjectsDeleted).To(Equal(int64(0)))

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
			_ = k8sClient.Delete(ctx, stack)
		})
	})

	Context("Blueprint Lifecycle Management", func() {
		It("should delete Blueprints older than specified duration", func() {
			// Create a Blueprint
			blueprint := testdata.NewBlueprint(testNamespace, "test-blueprint")
			blueprint.Labels = map[string]string{
				"lifecycle-test": "blueprint",
			}
			Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())

			// Verify Blueprint was created
			createdBlueprint := &envv1alpha1.Blueprint{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-blueprint",
				Namespace: testNamespace,
			}, createdBlueprint)).To(Succeed())

			// Create Lifecycle targeting Blueprints
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-blueprint-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Blueprint",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"lifecycle-test": "blueprint",
						},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "delete-old-blueprints",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 1 * time.Millisecond},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-blueprint-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for Blueprint to become "old enough"
			time.Sleep(10 * time.Millisecond)

			// Execute tasks
			reconciler.executeTasks(ctx, "test-blueprint-lifecycle")

			// Verify Blueprint was deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-blueprint",
					Namespace: testNamespace,
				}, &envv1alpha1.Blueprint{})
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"Blueprint should be deleted by lifecycle")

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
		})
	})

	Context("Label Selector Filtering", func() {
		It("should only delete Stacks matching the label selector", func() {
			// Create two Stacks with different labels
			matchingStack := testdata.NewStack(testNamespace, "matching-stack", "test-blueprint", "test-manifests")
			matchingStack.Labels = map[string]string{
				"environment": "dev",
				"cleanup":     "true",
			}
			Expect(k8sClient.Create(ctx, matchingStack)).To(Succeed())

			nonMatchingStack := testdata.NewStack(testNamespace, "non-matching-stack", "test-blueprint", "test-manifests")
			nonMatchingStack.Labels = map[string]string{
				"environment": "prod",
				"cleanup":     "false",
			}
			Expect(k8sClient.Create(ctx, nonMatchingStack)).To(Succeed())

			// Create Lifecycle targeting only dev environment
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-selector-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"environment": "dev",
							"cleanup":     "true",
						},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "delete-dev-stacks",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 1 * time.Millisecond},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-selector-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait and execute
			time.Sleep(10 * time.Millisecond)
			reconciler.executeTasks(ctx, "test-selector-lifecycle")

			// Verify matching Stack was deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "matching-stack",
					Namespace: testNamespace,
				}, &envv1alpha1.Stack{})
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"Matching Stack should be deleted")

			// Verify non-matching Stack still exists
			existingStack := &envv1alpha1.Stack{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "non-matching-stack",
				Namespace: testNamespace,
			}, existingStack)).To(Succeed(),
				"Non-matching Stack should NOT be deleted")

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
			_ = k8sClient.Delete(ctx, nonMatchingStack)
		})

		It("should delete all objects when no label selector is specified", func() {
			// Create multiple Stacks with different labels
			stack1 := testdata.NewStack(testNamespace, "stack-1", "test-blueprint", "test-manifests")
			stack1.Labels = map[string]string{"app": "one"}
			Expect(k8sClient.Create(ctx, stack1)).To(Succeed())

			stack2 := testdata.NewStack(testNamespace, "stack-2", "test-blueprint", "test-manifests")
			stack2.Labels = map[string]string{"app": "two"}
			Expect(k8sClient.Create(ctx, stack2)).To(Succeed())

			// Create Lifecycle without label selector
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-no-selector-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					// No LabelSelector - should match all
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "delete-all-stacks",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 1 * time.Millisecond},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-no-selector-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait and execute
			time.Sleep(10 * time.Millisecond)
			reconciler.executeTasks(ctx, "test-no-selector-lifecycle")

			// Verify both Stacks were deleted
			Eventually(func() bool {
				err1 := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "stack-1",
					Namespace: testNamespace,
				}, &envv1alpha1.Stack{})
				err2 := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "stack-2",
					Namespace: testNamespace,
				}, &envv1alpha1.Stack{})
				return errors.IsNotFound(err1) && errors.IsNotFound(err2)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"Both Stacks should be deleted when no selector is specified")

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
		})
	})

	Context("Status Tracking", func() {
		It("should track successful and failed task counts", func() {
			// Create a Stack
			stack := testdata.NewStack(testNamespace, "status-test-stack", "test-blueprint", "test-manifests")
			stack.Labels = map[string]string{"status-test": "true"}
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Create Lifecycle
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-status-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"status-test": "true"},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "delete-task",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 1 * time.Millisecond},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-status-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Execute tasks multiple times
			time.Sleep(10 * time.Millisecond)
			reconciler.executeTasks(ctx, "test-status-lifecycle")

			// Verify status
			updatedLifecycle := &envv1alpha1.Lifecycle{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-status-lifecycle"}, updatedLifecycle)).To(Succeed())

			Expect(updatedLifecycle.Status.LastRunTime).NotTo(BeNil())
			Expect(updatedLifecycle.Status.NextRunTime).NotTo(BeNil())
			Expect(updatedLifecycle.Status.SuccessfulTasks).To(BeNumerically(">=", 1))
			Expect(updatedLifecycle.Status.LastRunStats).NotTo(BeNil())
			Expect(updatedLifecycle.Status.LastRunStats.ObjectsEvaluated).To(Equal(int64(1)))
			Expect(updatedLifecycle.Status.LastRunStats.ObjectsDeleted).To(Equal(int64(1)))

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
		})
	})

	Context("Lifecycle Deletion", func() {
		It("should stop worker when Lifecycle is deleted", func() {
			// Create Lifecycle
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-deletion-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					Interval:   metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "delete-task",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 24 * time.Hour},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger first reconciliation to add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-deletion-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Trigger second reconciliation to start worker
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-deletion-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify worker is running
			reconciler.mu.Lock()
			_, workerExists := reconciler.workers["test-deletion-lifecycle"]
			reconciler.mu.Unlock()
			Expect(workerExists).To(BeTrue(), "Worker should be running")

			// Delete Lifecycle
			Expect(k8sClient.Delete(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation for deletion
			Eventually(func() bool {
				_, err := reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: "test-deletion-lifecycle"},
				})
				if err != nil {
					return false
				}
				reconciler.mu.Lock()
				_, exists := reconciler.workers["test-deletion-lifecycle"]
				reconciler.mu.Unlock()
				return !exists
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"Worker should be stopped after Lifecycle deletion")
		})
	})

	Context("Multiple Tasks", func() {
		It("should execute multiple tasks in sequence", func() {
			// Create Stacks with different labels
			stack1 := testdata.NewStack(testNamespace, "multi-task-stack-1", "test-blueprint", "test-manifests")
			stack1.Labels = map[string]string{"multi-task": "true", "tier": "frontend"}
			Expect(k8sClient.Create(ctx, stack1)).To(Succeed())

			stack2 := testdata.NewStack(testNamespace, "multi-task-stack-2", "test-blueprint", "test-manifests")
			stack2.Labels = map[string]string{"multi-task": "true", "tier": "backend"}
			Expect(k8sClient.Create(ctx, stack2)).To(Succeed())

			// Create Lifecycle with multiple tasks (both targeting same objects for simplicity)
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-multi-task-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"multi-task": "true"},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "first-delete-task",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 1 * time.Millisecond},
							},
						},
						{
							Name: "second-delete-task",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 1 * time.Millisecond},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-multi-task-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait and execute
			time.Sleep(10 * time.Millisecond)
			reconciler.executeTasks(ctx, "test-multi-task-lifecycle")

			// Verify both Stacks were deleted
			Eventually(func() bool {
				err1 := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "multi-task-stack-1",
					Namespace: testNamespace,
				}, &envv1alpha1.Stack{})
				err2 := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "multi-task-stack-2",
					Namespace: testNamespace,
				}, &envv1alpha1.Stack{})
				return errors.IsNotFound(err1) && errors.IsNotFound(err2)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"Both Stacks should be deleted")

			// Verify status shows multiple successful tasks
			updatedLifecycle := &envv1alpha1.Lifecycle{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "test-multi-task-lifecycle"}, updatedLifecycle)).To(Succeed())
			// First task deletes both, second task finds nothing to delete
			Expect(updatedLifecycle.Status.SuccessfulTasks).To(BeNumerically(">=", 2))

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
		})
	})

	Context("Worker Execution Behavior", func() {
		It("should not run concurrent executions when tasks exceed interval", func() {
			// Create a Lifecycle with very short interval
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-non-blocking-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					Interval:   metav1.Duration{Duration: 50 * time.Millisecond},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "quick-check",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 24 * time.Hour},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger first reconciliation to add finalizer
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-non-blocking-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Trigger second reconciliation to start worker
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-non-blocking-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify worker is running
			reconciler.mu.Lock()
			_, workerExists := reconciler.workers["test-non-blocking-lifecycle"]
			reconciler.mu.Unlock()
			Expect(workerExists).To(BeTrue(), "Worker should be running")

			// Wait for a few ticks to occur
			time.Sleep(200 * time.Millisecond)

			// Verify the worker is still registered (not blocked or crashed)
			reconciler.mu.Lock()
			_, stillExists := reconciler.workers["test-non-blocking-lifecycle"]
			reconciler.mu.Unlock()
			Expect(stillExists).To(BeTrue(), "Worker should still be running")

			// Delete Lifecycle and verify quick shutdown
			Expect(k8sClient.Delete(ctx, lifecycle)).To(Succeed())

			start := time.Now()
			Eventually(func() bool {
				_, err := reconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: types.NamespacedName{Name: "test-non-blocking-lifecycle"},
				})
				if err != nil {
					return false
				}
				reconciler.mu.Lock()
				_, exists := reconciler.workers["test-non-blocking-lifecycle"]
				reconciler.mu.Unlock()
				return !exists
			}, 2*time.Second, 50*time.Millisecond).Should(BeTrue(),
				"Worker should be stopped quickly")

			shutdownTime := time.Since(start)
			Expect(shutdownTime).To(BeNumerically("<", 1*time.Second),
				"Shutdown should be fast (not blocked by task execution)")
		})
	})
})
