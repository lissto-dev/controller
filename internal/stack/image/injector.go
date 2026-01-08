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

package image

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

const (
	kindDeployment = "Deployment"
	kindPod        = "Pod"
	serviceLabel   = "io.kompose.service"
)

// Injector handles image injection into workloads
type Injector struct{}

// NewInjector creates a new image injector
func NewInjector() *Injector {
	return &Injector{}
}

// Inject updates deployment and pod container images from Stack spec
func (i *Injector) Inject(ctx context.Context, objects []*unstructured.Unstructured, images map[string]envv1alpha1.ImageInfo) []Warning {
	log := logf.FromContext(ctx)
	var warnings []Warning

	for _, obj := range objects {
		if obj.GetKind() != kindDeployment && obj.GetKind() != kindPod {
			continue
		}

		resourceKind := obj.GetKind()
		resourceName := obj.GetName()
		containersPath := getContainersPath(resourceKind)

		labels := obj.GetLabels()
		serviceName, ok := labels[serviceLabel]
		if !ok {
			log.V(1).Info("Resource missing io.kompose.service label, skipping image injection",
				"kind", resourceKind, "name", resourceName)
			warnings = append(warnings, Warning{
				Deployment: resourceName,
				Message:    fmt.Sprintf("%s missing io.kompose.service label", resourceKind),
			})
			continue
		}

		imageInfo, exists := images[serviceName]
		if !exists {
			log.V(1).Info("No image found for service",
				"kind", resourceKind, "name", resourceName, "service", serviceName)
			warnings = append(warnings, Warning{
				Service:    serviceName,
				Deployment: resourceName,
				Message:    "No image specification found for service",
			})
			continue
		}

		containers, found, err := unstructured.NestedSlice(obj.Object, containersPath...)
		if err != nil || !found {
			log.Error(err, "Failed to get containers", "kind", resourceKind, "name", resourceName)
			warnings = append(warnings, Warning{
				Service:    serviceName,
				Deployment: resourceName,
				Message:    fmt.Sprintf("Failed to get containers: %v", err),
			})
			continue
		}

		targetContainerName := imageInfo.ContainerName
		if targetContainerName == "" {
			targetContainerName = serviceName
		}

		updated := i.updateContainerImages(containers, targetContainerName, imageInfo.Digest)

		if !updated {
			log.Info("No matching container found for image injection",
				"kind", resourceKind, "name", resourceName,
				"service", serviceName, "targetContainer", targetContainerName)
			warnings = append(warnings, Warning{
				Service:         serviceName,
				Deployment:      resourceName,
				TargetContainer: targetContainerName,
				Message:         fmt.Sprintf("No container named '%s' found in %s", targetContainerName, resourceKind),
			})
			continue
		}

		if err := unstructured.SetNestedSlice(obj.Object, containers, containersPath...); err != nil {
			log.Error(err, "Failed to set containers", "kind", resourceKind, "name", resourceName)
			warnings = append(warnings, Warning{
				Service:    serviceName,
				Deployment: resourceName,
				Message:    fmt.Sprintf("Failed to update containers: %v", err),
			})
			continue
		}

		log.Info("Injected image",
			"kind", resourceKind, "name", resourceName,
			"service", serviceName, "container", targetContainerName,
			"image", imageInfo.Digest)
	}

	return warnings
}

// updateContainerImages updates the image for matching containers
func (i *Injector) updateContainerImages(containers []interface{}, targetName, digest string) bool {
	updated := false
	for idx, container := range containers {
		containerMap, ok := container.(map[string]interface{})
		if !ok {
			continue
		}

		containerName, _, _ := unstructured.NestedString(containerMap, "name")
		if containerName == targetName {
			containerMap["image"] = digest
			containers[idx] = containerMap
			updated = true
		}
	}
	return updated
}

// getContainersPath returns the path to containers based on resource kind
func getContainersPath(resourceKind string) []string {
	if resourceKind == kindDeployment {
		return []string{"spec", "template", "spec", "containers"}
	}
	return []string{"spec", "containers"}
}
