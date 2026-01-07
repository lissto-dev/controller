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
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

func TestGetResourceClassification(t *testing.T) {
	tests := []struct {
		name        string
		obj         *unstructured.Unstructured
		wantClass   string
		wantService string
		wantErr     bool
	}{
		{
			name: "valid state resource",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "v1",
					"kind":       "PersistentVolumeClaim",
					"metadata": map[string]interface{}{
						"name": "postgres-data",
						"annotations": map[string]interface{}{
							"lissto.dev/class": "state",
						},
						"labels": map[string]interface{}{
							"io.kompose.service": "postgres",
						},
					},
				},
			},
			wantClass:   "state",
			wantService: "postgres",
			wantErr:     false,
		},
		{
			name: "valid workload resource",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name": "web",
						"annotations": map[string]interface{}{
							"lissto.dev/class": "workload",
						},
						"labels": map[string]interface{}{
							"io.kompose.service": "web",
						},
					},
				},
			},
			wantClass:   "workload",
			wantService: "web",
			wantErr:     false,
		},
		{
			name: "missing annotation",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name": "web",
						"labels": map[string]interface{}{
							"io.kompose.service": "web",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid class value",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name": "web",
						"annotations": map[string]interface{}{
							"lissto.dev/class": "invalid",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "no annotations at all",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name": "web",
					},
				},
			},
			wantErr: true,
		},
		{
			name: "missing service label",
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"name": "web",
						"annotations": map[string]interface{}{
							"lissto.dev/class": "workload",
						},
					},
				},
			},
			wantClass:   "workload",
			wantService: "", // Empty service name is allowed
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := getResourceClassification(tt.obj)
			if (err != nil) != tt.wantErr {
				t.Errorf("getResourceClassification() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got.Class != tt.wantClass {
				t.Errorf("getResourceClassification() class = %v, want %v", got.Class, tt.wantClass)
			}
			if got.ServiceName != tt.wantService {
				t.Errorf("getResourceClassification() serviceName = %v, want %v", got.ServiceName, tt.wantService)
			}
		})
	}
}

func TestShouldSuspendService(t *testing.T) {
	tests := []struct {
		name        string
		stack       *envv1alpha1.Stack
		serviceName string
		want        bool
	}{
		{
			name: "stack-wide suspension",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended: true,
				},
			},
			serviceName: "web",
			want:        true,
		},
		{
			name: "no suspension",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended: false,
				},
			},
			serviceName: "web",
			want:        false,
		},
		{
			name: "service in suspended list",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended:         false,
					SuspendedServices: []string{"worker", "batch"},
				},
			},
			serviceName: "worker",
			want:        true,
		},
		{
			name: "service not in suspended list",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended:         false,
					SuspendedServices: []string{"worker", "batch"},
				},
			},
			serviceName: "web",
			want:        false,
		},
		{
			name: "stack-wide overrides per-service",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended:         true,
					SuspendedServices: []string{"worker"}, // This is ignored when Suspended=true
				},
			},
			serviceName: "web", // Not in list, but Suspended=true
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSuspendService(tt.stack, tt.serviceName); got != tt.want {
				t.Errorf("shouldSuspendService() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShouldSuspendResource(t *testing.T) {
	tests := []struct {
		name           string
		stack          *envv1alpha1.Stack
		classification *ResourceClassification
		want           bool
	}{
		{
			name: "state resource never suspended",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended: true,
				},
			},
			classification: &ResourceClassification{
				Class:       envv1alpha1.ResourceClassState,
				ServiceName: "postgres",
			},
			want: false,
		},
		{
			name: "workload resource suspended when stack suspended",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended: true,
				},
			},
			classification: &ResourceClassification{
				Class:       envv1alpha1.ResourceClassWorkload,
				ServiceName: "web",
			},
			want: true,
		},
		{
			name: "workload resource not suspended when stack running",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended: false,
				},
			},
			classification: &ResourceClassification{
				Class:       envv1alpha1.ResourceClassWorkload,
				ServiceName: "web",
			},
			want: false,
		},
		{
			name: "workload resource suspended when service in list",
			stack: &envv1alpha1.Stack{
				Spec: envv1alpha1.StackSpec{
					Suspended:         false,
					SuspendedServices: []string{"worker"},
				},
			},
			classification: &ResourceClassification{
				Class:       envv1alpha1.ResourceClassWorkload,
				ServiceName: "worker",
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSuspendResource(tt.stack, tt.classification); got != tt.want {
				t.Errorf("shouldSuspendResource() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateResourceAnnotations(t *testing.T) {
	tests := []struct {
		name    string
		objects []*unstructured.Unstructured
		wantErr bool
	}{
		{
			name: "all resources have annotation",
			objects: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": "Deployment",
						"metadata": map[string]interface{}{
							"name": "web",
							"annotations": map[string]interface{}{
								"lissto.dev/class": "workload",
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"kind": "PersistentVolumeClaim",
						"metadata": map[string]interface{}{
							"name": "data",
							"annotations": map[string]interface{}{
								"lissto.dev/class": "state",
							},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "one resource missing annotation",
			objects: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": "Deployment",
						"metadata": map[string]interface{}{
							"name": "web",
							"annotations": map[string]interface{}{
								"lissto.dev/class": "workload",
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"kind": "Service",
						"metadata": map[string]interface{}{
							"name": "web",
							// Missing annotation
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name:    "empty list",
			objects: []*unstructured.Unstructured{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateResourceAnnotations(tt.objects)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateResourceAnnotations() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCollectServiceNames(t *testing.T) {
	tests := []struct {
		name    string
		objects []*unstructured.Unstructured
		want    []string
	}{
		{
			name: "multiple services",
			objects: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": "Deployment",
						"metadata": map[string]interface{}{
							"name": "web",
							"labels": map[string]interface{}{
								"io.kompose.service": "web",
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"kind": "Service",
						"metadata": map[string]interface{}{
							"name": "web-svc",
							"labels": map[string]interface{}{
								"io.kompose.service": "web",
							},
						},
					},
				},
				{
					Object: map[string]interface{}{
						"kind": "Deployment",
						"metadata": map[string]interface{}{
							"name": "postgres",
							"labels": map[string]interface{}{
								"io.kompose.service": "postgres",
							},
						},
					},
				},
			},
			want: []string{"web", "postgres"},
		},
		{
			name: "no labels",
			objects: []*unstructured.Unstructured{
				{
					Object: map[string]interface{}{
						"kind": "ConfigMap",
						"metadata": map[string]interface{}{
							"name": "config",
						},
					},
				},
			},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collectServiceNames(tt.objects)
			// Check that all expected services are present (order doesn't matter)
			if len(got) != len(tt.want) {
				t.Errorf("collectServiceNames() returned %d services, want %d", len(got), len(tt.want))
				return
			}
			gotMap := make(map[string]bool)
			for _, s := range got {
				gotMap[s] = true
			}
			for _, s := range tt.want {
				if !gotMap[s] {
					t.Errorf("collectServiceNames() missing service %s", s)
				}
			}
		})
	}
}

func TestPhaseHistoryLimit(t *testing.T) {
	// Test that phase history is limited to MaxPhaseHistoryLength
	if envv1alpha1.MaxPhaseHistoryLength != 10 {
		t.Errorf("MaxPhaseHistoryLength = %d, want 10", envv1alpha1.MaxPhaseHistoryLength)
	}
}
