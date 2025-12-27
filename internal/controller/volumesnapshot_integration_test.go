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
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
	"github.com/lissto-dev/controller/pkg/config"
)

var _ = Describe("VolumeSnapshot Integration Tests", func() {
	var (
		testNamespace string
		reconciler    *VolumeSnapshotReconciler
	)

	BeforeEach(func() {
		// Generate unique namespace for each test
		testNamespace = fmt.Sprintf("test-snapshot-%d", time.Now().UnixNano())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Initialize reconciler
		reconciler = &VolumeSnapshotReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(100),
			Config: &config.Config{
				ObjectStorage: config.ObjectStorageConfig{
					Enabled: true,
				},
			},
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

	Context("VolumeSnapshot CRD Operations", func() {
		It("should create a VolumeSnapshot with valid spec", func() {
			vs := &envv1alpha1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-snapshot",
					Namespace: testNamespace,
					Labels: map[string]string{
						"env.lissto.dev/stack": "test-stack",
					},
					Annotations: map[string]string{
						"env.lissto.dev/source-blueprint": "test-blueprint",
					},
				},
				Spec: envv1alpha1.VolumeSnapshotSpec{
					VolumeIdentifier: envv1alpha1.VolumeIdentifier{
						Repo:        "github.com/org/repo",
						ServiceName: "postgres",
						Image:       "postgres:15",
						MountPath:   "/var/lib/postgresql/data",
					},
				},
			}
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())

			// Verify VolumeSnapshot was created
			created := &envv1alpha1.VolumeSnapshot{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-snapshot",
				Namespace: testNamespace,
			}, created)).To(Succeed())

			Expect(created.Spec.VolumeIdentifier.Repo).To(Equal("github.com/org/repo"))
			Expect(created.Spec.VolumeIdentifier.ServiceName).To(Equal("postgres"))
			Expect(created.Spec.VolumeIdentifier.Image).To(Equal("postgres:15"))
			Expect(created.Spec.VolumeIdentifier.MountPath).To(Equal("/var/lib/postgresql/data"))

			// Cleanup
			_ = k8sClient.Delete(ctx, vs)
		})

		It("should update VolumeSnapshot status", func() {
			vs := &envv1alpha1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "status-test-snapshot",
					Namespace: testNamespace,
				},
				Spec: envv1alpha1.VolumeSnapshotSpec{
					VolumeIdentifier: envv1alpha1.VolumeIdentifier{
						Repo:        "github.com/org/repo",
						ServiceName: "postgres",
						Image:       "postgres:15",
						MountPath:   "/var/lib/postgresql/data",
					},
				},
			}
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())

			// Update status
			vs.Status.Phase = envv1alpha1.VolumeSnapshotPhaseInProgress
			vs.Status.StartTime = &metav1.Time{Time: time.Now()}
			vs.Status.JobName = "snapshot-job-123"
			Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

			// Verify status was updated
			updated := &envv1alpha1.VolumeSnapshot{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "status-test-snapshot",
				Namespace: testNamespace,
			}, updated)).To(Succeed())

			Expect(updated.Status.Phase).To(Equal(envv1alpha1.VolumeSnapshotPhaseInProgress))
			Expect(updated.Status.JobName).To(Equal("snapshot-job-123"))
			Expect(updated.Status.StartTime).NotTo(BeNil())

			// Cleanup
			_ = k8sClient.Delete(ctx, vs)
		})
	})

	Context("VolumeSnapshot Lifecycle Deletion", func() {
		It("should delete VolumeSnapshots older than specified duration", func() {
			// Create a VolumeSnapshot
			vs := &envv1alpha1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "old-snapshot",
					Namespace: testNamespace,
					Labels: map[string]string{
						"cleanup": "true",
					},
				},
				Spec: envv1alpha1.VolumeSnapshotSpec{
					VolumeIdentifier: envv1alpha1.VolumeIdentifier{
						Repo:        "github.com/org/repo",
						ServiceName: "postgres",
						Image:       "postgres:15",
						MountPath:   "/var/lib/postgresql/data",
					},
				},
			}
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())

			// Create Lifecycle targeting VolumeSnapshots
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-snapshot-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "VolumeSnapshot",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"cleanup": "true",
						},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "delete-old-snapshots",
							Delete: &envv1alpha1.DeleteTask{
								OlderThan: metav1.Duration{Duration: 1 * time.Millisecond},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Initialize lifecycle reconciler
			lifecycleReconciler := &LifecycleReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(100),
				workers:  make(map[string]workerHandle),
			}

			// Trigger reconciliation
			_, err := lifecycleReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-snapshot-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for snapshot to become "old enough"
			time.Sleep(10 * time.Millisecond)

			// Execute tasks
			lifecycleReconciler.executeTasks(ctx, "test-snapshot-lifecycle")

			// Verify VolumeSnapshot was deleted
			Eventually(func() bool {
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "old-snapshot",
					Namespace: testNamespace,
				}, &envv1alpha1.VolumeSnapshot{})
				return errors.IsNotFound(err)
			}, 5*time.Second, 100*time.Millisecond).Should(BeTrue(),
				"VolumeSnapshot should be deleted by lifecycle")

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
		})
	})

	Context("VolumeSnapshot Reconciler", func() {
		It("should set initial phase to Pending", func() {
			vs := &envv1alpha1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pending-snapshot",
					Namespace: testNamespace,
				},
				Spec: envv1alpha1.VolumeSnapshotSpec{
					VolumeIdentifier: envv1alpha1.VolumeIdentifier{
						Repo:        "github.com/org/repo",
						ServiceName: "postgres",
						Image:       "postgres:15",
						MountPath:   "/var/lib/postgresql/data",
					},
				},
			}
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())

			// Reconcile
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "pending-snapshot",
					Namespace: testNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify phase was set
			updated := &envv1alpha1.VolumeSnapshot{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "pending-snapshot",
				Namespace: testNamespace,
			}, updated)).To(Succeed())

			Expect(updated.Status.Phase).To(Equal(envv1alpha1.VolumeSnapshotPhasePending))

			// Cleanup
			_ = k8sClient.Delete(ctx, vs)
		})
	})
})

var _ = Describe("Lifecycle ScaleDown/ScaleUp Integration Tests", func() {
	var (
		testNamespace string
		reconciler    *LifecycleReconciler
	)

	BeforeEach(func() {
		// Generate unique namespace for each test
		testNamespace = fmt.Sprintf("test-scale-%d", time.Now().UnixNano())

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
			Config: &config.Config{
				ObjectStorage: config.ObjectStorageConfig{
					Enabled: true,
				},
			},
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

	Context("ScaleDown Task", func() {
		It("should scale down deployments to 0 replicas", func() {
			// Create a Stack with label
			stack := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scale-test-stack",
					Namespace: testNamespace,
					Labels: map[string]string{
						"scale-test": "true",
					},
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "test-blueprint",
					ManifestsConfigMapRef: "test-manifests",
				},
			}
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Create a Deployment owned by the Stack
			replicas := int32(3)
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: testNamespace,
					Labels: map[string]string{
						"env.lissto.dev/stack": "scale-test-stack",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "env.lissto.dev/v1alpha1",
							Kind:       "Stack",
							Name:       "scale-test-stack",
							UID:        stack.UID,
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Create Lifecycle with ScaleDown task
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-scaledown-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"scale-test": "true",
						},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name: "scale-down",
							ScaleDown: &envv1alpha1.ScaleDownTask{
								Timeout: metav1.Duration{Duration: 5 * time.Minute},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-scaledown-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Execute tasks
			reconciler.executeTasks(ctx, "test-scaledown-lifecycle")

			// Verify deployment was scaled down
			Eventually(func() int32 {
				updated := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "test-deployment",
					Namespace: testNamespace,
				}, updated)
				if err != nil {
					return -1
				}
				return *updated.Spec.Replicas
			}, 5*time.Second, 100*time.Millisecond).Should(Equal(int32(0)),
				"Deployment should be scaled to 0 replicas")

			// Verify original replicas annotation was set
			updated := &appsv1.Deployment{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "test-deployment",
				Namespace: testNamespace,
			}, updated)).To(Succeed())
			Expect(updated.Annotations).To(HaveKey("env.lissto.dev/original-replicas"))
			Expect(updated.Annotations["env.lissto.dev/original-replicas"]).To(Equal("3"))

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
			_ = k8sClient.Delete(ctx, deployment)
			_ = k8sClient.Delete(ctx, stack)
		})
	})

	Context("ScaleUp Task", func() {
		It("should restore deployments to original replica counts", func() {
			// Create a Stack with label
			stack := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scaleup-test-stack",
					Namespace: testNamespace,
					Labels: map[string]string{
						"scaleup-test": "true",
					},
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "test-blueprint",
					ManifestsConfigMapRef: "test-manifests",
				},
			}
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Create a Deployment that was previously scaled down
			replicas := int32(0)
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "scaleup-deployment",
					Namespace: testNamespace,
					Labels: map[string]string{
						"env.lissto.dev/stack": "scaleup-test-stack",
					},
					Annotations: map[string]string{
						"env.lissto.dev/original-replicas": "5",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "env.lissto.dev/v1alpha1",
							Kind:       "Stack",
							Name:       "scaleup-test-stack",
							UID:        stack.UID,
						},
					},
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &replicas,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "scaleup-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "scaleup-test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "test",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

			// Create Lifecycle with ScaleUp task
			lifecycle := &envv1alpha1.Lifecycle{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-scaleup-lifecycle",
				},
				Spec: envv1alpha1.LifecycleSpec{
					TargetKind: "Stack",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"scaleup-test": "true",
						},
					},
					Interval: metav1.Duration{Duration: 1 * time.Hour},
					Tasks: []envv1alpha1.LifecycleTask{
						{
							Name:    "scale-up",
							ScaleUp: &envv1alpha1.ScaleUpTask{},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

			// Trigger reconciliation
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-scaleup-lifecycle"},
			})
			Expect(err).NotTo(HaveOccurred())

			// Execute tasks
			reconciler.executeTasks(ctx, "test-scaleup-lifecycle")

			// Verify deployment was scaled up
			Eventually(func() int32 {
				updated := &appsv1.Deployment{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      "scaleup-deployment",
					Namespace: testNamespace,
				}, updated)
				if err != nil {
					return -1
				}
				return *updated.Spec.Replicas
			}, 5*time.Second, 100*time.Millisecond).Should(Equal(int32(5)),
				"Deployment should be scaled back to 5 replicas")

			// Cleanup
			_ = k8sClient.Delete(ctx, lifecycle)
			_ = k8sClient.Delete(ctx, deployment)
			_ = k8sClient.Delete(ctx, stack)
		})
	})
})

var _ = Describe("Stack Volume Restore Integration Tests", func() {
	var (
		testNamespace   string
		stackReconciler *StackReconciler
	)

	BeforeEach(func() {
		// Generate unique namespace for each test
		testNamespace = fmt.Sprintf("test-restore-%d", time.Now().UnixNano())

		// Create test namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())

		// Initialize reconciler
		stackReconciler = &StackReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(100),
			Config: &config.Config{
				ObjectStorage: config.ObjectStorageConfig{
					Enabled: true,
				},
			},
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

	Context("Stack with Volumes Field", func() {
		It("should identify stacks that need volume restore", func() {
			// Stack without volumes
			stackNoVolumes := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-volumes-stack",
					Namespace: testNamespace,
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "test-blueprint",
					ManifestsConfigMapRef: "test-manifests",
				},
			}
			Expect(stackReconciler.needsVolumeRestore(stackNoVolumes)).To(BeFalse())

			// Stack with volumes
			stackWithVolumes := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "with-volumes-stack",
					Namespace: testNamespace,
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "test-blueprint",
					ManifestsConfigMapRef: "test-manifests",
					Volumes: map[string]envv1alpha1.ServiceVolumes{
						"postgres": {
							SnapshotRefs: []string{"vs-postgres-123"},
						},
					},
				},
			}
			Expect(stackReconciler.needsVolumeRestore(stackWithVolumes)).To(BeTrue())
		})

		It("should create restore job when VolumeSnapshot exists", func() {
			// Create a completed VolumeSnapshot
			vs := &envv1alpha1.VolumeSnapshot{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vs-postgres-restore",
					Namespace: testNamespace,
				},
				Spec: envv1alpha1.VolumeSnapshotSpec{
					VolumeIdentifier: envv1alpha1.VolumeIdentifier{
						Repo:        "github.com/org/repo",
						ServiceName: "postgres",
						Image:       "postgres:15",
						MountPath:   "/var/lib/postgresql/data",
					},
				},
			}
			Expect(k8sClient.Create(ctx, vs)).To(Succeed())

			// Update status to completed
			vs.Status.Phase = envv1alpha1.VolumeSnapshotPhaseCompleted
			vs.Status.StoragePath = "user/dev/abc123/postgres/def456/vs-postgres-restore.tar.gz"
			Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

			// Create a Stack with volumes field
			stack := &envv1alpha1.Stack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "restore-test-stack",
					Namespace: testNamespace,
				},
				Spec: envv1alpha1.StackSpec{
					BlueprintReference:    "test-blueprint",
					ManifestsConfigMapRef: "test-manifests",
					Volumes: map[string]envv1alpha1.ServiceVolumes{
						"postgres": {
							SnapshotRefs: []string{"vs-postgres-restore"},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, stack)).To(Succeed())

			// Create a PVC for the service
			pvc := &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "postgres-data",
					Namespace: testNamespace,
					Labels: map[string]string{
						"io.kompose.service": "postgres",
					},
					Annotations: map[string]string{
						"env.lissto.dev/mount-path": "/var/lib/postgresql/data",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes: []corev1.PersistentVolumeAccessMode{
						corev1.ReadWriteOnce,
					},
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse("1Gi"),
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, pvc)).To(Succeed())

			// Create unstructured PVC objects for restore
			pvcObjects := []*unstructuredPVC{
				{
					name:      "postgres-data",
					namespace: testNamespace,
					labels: map[string]string{
						"io.kompose.service": "postgres",
					},
					annotations: map[string]string{
						"env.lissto.dev/mount-path": "/var/lib/postgresql/data",
					},
				},
			}

			// Convert to unstructured
			var unstructuredPVCs []*unstructured.Unstructured
			for _, p := range pvcObjects {
				u := &unstructured.Unstructured{}
				u.SetKind("PersistentVolumeClaim")
				u.SetName(p.name)
				u.SetNamespace(p.namespace)
				u.SetLabels(p.labels)
				u.SetAnnotations(p.annotations)
				unstructuredPVCs = append(unstructuredPVCs, u)
			}

			// Call restoreVolumes
			complete, results, err := stackReconciler.restoreVolumes(ctx, stack, unstructuredPVCs)
			Expect(err).NotTo(HaveOccurred())
			Expect(complete).To(BeFalse()) // Job was just created, not complete yet
			Expect(results).To(HaveLen(1))
			Expect(results[0].Status).To(Equal("pending"))
			Expect(results[0].PVCName).To(Equal("postgres-data"))

			// Verify restore job was created
			jobList := &batchv1.JobList{}
			Expect(k8sClient.List(ctx, jobList, client.InNamespace(testNamespace))).To(Succeed())
			Expect(jobList.Items).To(HaveLen(1))
			Expect(jobList.Items[0].Name).To(ContainSubstring("restore"))

			// Cleanup
			_ = k8sClient.Delete(ctx, vs)
			_ = k8sClient.Delete(ctx, stack)
			_ = k8sClient.Delete(ctx, pvc)
			for _, job := range jobList.Items {
				_ = k8sClient.Delete(ctx, &job)
			}
		})
	})
})

// Helper struct for test
type unstructuredPVC struct {
	name        string
	namespace   string
	labels      map[string]string
	annotations map[string]string
}
