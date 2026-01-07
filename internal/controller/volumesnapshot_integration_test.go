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

// Test helpers
func newTestNamespace(prefix string) *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()),
		},
	}
}

func newVolumeSnapshot(name, namespace string) *envv1alpha1.VolumeSnapshot {
	return &envv1alpha1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: envv1alpha1.VolumeSnapshotSpec{
			VolumeIdentifier: envv1alpha1.VolumeIdentifier{
				Repo: "github.com/org/repo", ServiceName: "postgres",
				Image: "postgres:15", MountPath: "/var/lib/postgresql/data",
			},
		},
	}
}

func newStack(name, namespace string, labels map[string]string) *envv1alpha1.Stack {
	return &envv1alpha1.Stack{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels},
		Spec:       envv1alpha1.StackSpec{BlueprintReference: "test-blueprint", ManifestsConfigMapRef: "test-manifests"},
	}
}

func newDeployment(name, namespace, stackName string, replicas int32, stackUID types.UID) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: namespace,
			Labels: map[string]string{"env.lissto.dev/stack": stackName},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "env.lissto.dev/v1alpha1", Kind: "Stack", Name: stackName, UID: stackUID,
			}},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "test", Image: "nginx:latest"}}},
			},
		},
	}
}

func newLifecycle(name, targetKind string, labels map[string]string, tasks []envv1alpha1.LifecycleTask) *envv1alpha1.Lifecycle {
	return &envv1alpha1.Lifecycle{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: envv1alpha1.LifecycleSpec{
			TargetKind:    targetKind,
			LabelSelector: &metav1.LabelSelector{MatchLabels: labels},
			Interval:      metav1.Duration{Duration: time.Hour},
			Tasks:         tasks,
		},
	}
}

func testConfig() *config.Config {
	return &config.Config{ObjectStorage: config.ObjectStorageConfig{Enabled: true}}
}

var _ = Describe("VolumeSnapshot Integration", func() {
	var ns *corev1.Namespace

	BeforeEach(func() {
		ns = newTestNamespace("test-vs")
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})
	AfterEach(func() { _ = k8sClient.Delete(ctx, ns) })

	It("creates and updates VolumeSnapshot", func() {
		vs := newVolumeSnapshot("test-snapshot", ns.Name)
		vs.Labels = map[string]string{"env.lissto.dev/stack": "test-stack"}
		Expect(k8sClient.Create(ctx, vs)).To(Succeed())

		// Verify creation
		created := &envv1alpha1.VolumeSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vs.Name, Namespace: ns.Name}, created)).To(Succeed())
		Expect(created.Spec.VolumeIdentifier.ServiceName).To(Equal("postgres"))

		// Update status
		created.Status.Phase = envv1alpha1.VolumeSnapshotPhaseInProgress
		created.Status.JobName = "snapshot-job-123"
		Expect(k8sClient.Status().Update(ctx, created)).To(Succeed())

		// Verify status update
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vs.Name, Namespace: ns.Name}, created)).To(Succeed())
		Expect(created.Status.Phase).To(Equal(envv1alpha1.VolumeSnapshotPhaseInProgress))
	})

	It("reconciler sets initial phase to Pending", func() {
		vs := newVolumeSnapshot("pending-snapshot", ns.Name)
		Expect(k8sClient.Create(ctx, vs)).To(Succeed())

		reconciler := &VolumeSnapshotReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10), Config: testConfig(),
		}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: vs.Name, Namespace: ns.Name},
		})
		Expect(err).NotTo(HaveOccurred())

		updated := &envv1alpha1.VolumeSnapshot{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: vs.Name, Namespace: ns.Name}, updated)).To(Succeed())
		Expect(updated.Status.Phase).To(Equal(envv1alpha1.VolumeSnapshotPhasePending))
	})

	It("lifecycle deletes old VolumeSnapshots", func() {
		vs := newVolumeSnapshot("old-snapshot", ns.Name)
		vs.Labels = map[string]string{"cleanup": "true"}
		Expect(k8sClient.Create(ctx, vs)).To(Succeed())

		lifecycle := newLifecycle("cleanup-lifecycle", "VolumeSnapshot",
			map[string]string{"cleanup": "true"},
			[]envv1alpha1.LifecycleTask{{Name: "delete", Delete: &envv1alpha1.DeleteTask{
				OlderThan: metav1.Duration{Duration: time.Millisecond},
			}}})
		Expect(k8sClient.Create(ctx, lifecycle)).To(Succeed())

		reconciler := &LifecycleReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10), workers: make(map[string]workerHandle),
		}
		_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: lifecycle.Name}})
		time.Sleep(10 * time.Millisecond)
		reconciler.executeTasks(ctx, lifecycle.Name)

		Eventually(func() bool {
			return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: vs.Name, Namespace: ns.Name}, &envv1alpha1.VolumeSnapshot{}))
		}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
	})
})

var _ = Describe("Lifecycle ScaleDown/ScaleUp", func() {
	var ns *corev1.Namespace

	BeforeEach(func() {
		ns = newTestNamespace("test-scale")
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})
	AfterEach(func() { _ = k8sClient.Delete(ctx, ns) })

	It("scales down and up deployments", func() {
		stack := newStack("scale-stack", ns.Name, map[string]string{"scale-test": "true"})
		Expect(k8sClient.Create(ctx, stack)).To(Succeed())

		deployment := newDeployment("test-deploy", ns.Name, stack.Name, 3, stack.UID)
		Expect(k8sClient.Create(ctx, deployment)).To(Succeed())

		reconciler := &LifecycleReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10), workers: make(map[string]workerHandle),
			Config: testConfig(),
		}

		// ScaleDown
		scaleDownLifecycle := newLifecycle("scaledown", "Stack",
			map[string]string{"scale-test": "true"},
			[]envv1alpha1.LifecycleTask{{Name: "down", ScaleDown: &envv1alpha1.ScaleDownTask{
				Timeout: metav1.Duration{Duration: 5 * time.Minute},
			}}})
		Expect(k8sClient.Create(ctx, scaleDownLifecycle)).To(Succeed())

		_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: scaleDownLifecycle.Name}})
		reconciler.executeTasks(ctx, scaleDownLifecycle.Name)

		Eventually(func() int32 {
			d := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: ns.Name}, d); err != nil {
				return -1
			}
			return *d.Spec.Replicas
		}, 5*time.Second, 100*time.Millisecond).Should(Equal(int32(0)))

		// Verify annotation
		d := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: ns.Name}, d)).To(Succeed())
		Expect(d.Annotations["env.lissto.dev/original-replicas"]).To(Equal("3"))

		// ScaleUp
		scaleUpLifecycle := newLifecycle("scaleup", "Stack",
			map[string]string{"scale-test": "true"},
			[]envv1alpha1.LifecycleTask{{Name: "up", ScaleUp: &envv1alpha1.ScaleUpTask{}}})
		Expect(k8sClient.Create(ctx, scaleUpLifecycle)).To(Succeed())

		_, _ = reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: scaleUpLifecycle.Name}})
		reconciler.executeTasks(ctx, scaleUpLifecycle.Name)

		Eventually(func() int32 {
			d := &appsv1.Deployment{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: deployment.Name, Namespace: ns.Name}, d); err != nil {
				return -1
			}
			return *d.Spec.Replicas
		}, 5*time.Second, 100*time.Millisecond).Should(Equal(int32(3)))
	})
})

var _ = Describe("Stack Volume Restore", func() {
	var ns *corev1.Namespace

	BeforeEach(func() {
		ns = newTestNamespace("test-restore")
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
	})
	AfterEach(func() { _ = k8sClient.Delete(ctx, ns) })

	It("identifies stacks needing restore", func() {
		reconciler := &StackReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Config: testConfig()}

		stackNoVol := newStack("no-vol", ns.Name, nil)
		Expect(reconciler.needsVolumeRestore(stackNoVol)).To(BeFalse())

		stackWithVol := newStack("with-vol", ns.Name, nil)
		stackWithVol.Spec.Volumes = map[string]envv1alpha1.ServiceVolumes{
			"postgres": {SnapshotRefs: []string{"vs-123"}},
		}
		Expect(reconciler.needsVolumeRestore(stackWithVol)).To(BeTrue())
	})

	It("creates restore job for completed snapshot", func() {
		vs := newVolumeSnapshot("vs-restore", ns.Name)
		Expect(k8sClient.Create(ctx, vs)).To(Succeed())
		vs.Status.Phase = envv1alpha1.VolumeSnapshotPhaseCompleted
		vs.Status.StoragePath = "user/dev/abc/postgres/def/vs-restore.tar.gz"
		Expect(k8sClient.Status().Update(ctx, vs)).To(Succeed())

		stack := newStack("restore-stack", ns.Name, nil)
		stack.Spec.Volumes = map[string]envv1alpha1.ServiceVolumes{
			"postgres": {SnapshotRefs: []string{"vs-restore"}},
		}
		Expect(k8sClient.Create(ctx, stack)).To(Succeed())

		pvc := &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "postgres-data", Namespace: ns.Name,
				Labels:      map[string]string{"io.kompose.service": "postgres"},
				Annotations: map[string]string{"env.lissto.dev/mount-path": "/var/lib/postgresql/data"},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources:   corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("1Gi")}},
			},
		}
		Expect(k8sClient.Create(ctx, pvc)).To(Succeed())

		u := &unstructured.Unstructured{}
		u.SetKind("PersistentVolumeClaim")
		u.SetName(pvc.Name)
		u.SetNamespace(ns.Name)
		u.SetLabels(pvc.Labels)
		u.SetAnnotations(pvc.Annotations)

		reconciler := &StackReconciler{
			Client: k8sClient, Scheme: k8sClient.Scheme(),
			Recorder: record.NewFakeRecorder(10), Config: testConfig(),
		}
		complete, results, err := reconciler.restoreVolumes(ctx, stack, []*unstructured.Unstructured{u})
		Expect(err).NotTo(HaveOccurred())
		Expect(complete).To(BeFalse())
		Expect(results).To(HaveLen(1))
		Expect(results[0].PVCName).To(Equal("postgres-data"))

		jobList := &batchv1.JobList{}
		Expect(k8sClient.List(ctx, jobList, client.InNamespace(ns.Name))).To(Succeed())
		Expect(jobList.Items).To(HaveLen(1))
		Expect(jobList.Items[0].Name).To(ContainSubstring("restore"))
	})
})
