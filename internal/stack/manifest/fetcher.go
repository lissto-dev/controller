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

package manifest

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const manifestsKey = "manifests.yaml"

// Fetcher handles fetching manifests from ConfigMaps
type Fetcher struct {
	client client.Client
}

// NewFetcher creates a new manifest fetcher
func NewFetcher(c client.Client) *Fetcher {
	return &Fetcher{client: c}
}

// Fetch retrieves manifests from a ConfigMap
func (f *Fetcher) Fetch(ctx context.Context, namespace, configMapName string) (string, error) {
	log := logf.FromContext(ctx)

	if configMapName == "" {
		return "", fmt.Errorf("configMapName is empty")
	}

	configMap := &corev1.ConfigMap{}
	if err := f.client.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      configMapName,
	}, configMap); err != nil {
		return "", fmt.Errorf("failed to get ConfigMap %s/%s: %w", namespace, configMapName, err)
	}

	manifests, ok := configMap.Data[manifestsKey]
	if !ok {
		return "", fmt.Errorf("%s not found in ConfigMap %s/%s", manifestsKey, namespace, configMapName)
	}

	log.V(1).Info("Fetched manifests from ConfigMap",
		"configMap", configMapName, "size", len(manifests))

	return manifests, nil
}
