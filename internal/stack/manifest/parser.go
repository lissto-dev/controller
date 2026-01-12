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
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// Parse parses multi-document YAML into unstructured objects
func Parse(manifestsYAML string) ([]*unstructured.Unstructured, error) {
	docs := strings.Split(manifestsYAML, "---\n")

	objects := make([]*unstructured.Unstructured, 0, len(docs))
	for i, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), obj); err != nil {
			return nil, fmt.Errorf("failed to parse manifest document %d: %w", i, err)
		}

		if obj.GetKind() == "" {
			continue
		}

		objects = append(objects, obj)
	}

	return objects, nil
}

// FetchAndParse combines fetching and parsing manifests
func (f *Fetcher) FetchAndParse(ctx context.Context, namespace, configMapName string) ([]*unstructured.Unstructured, error) {
	manifests, err := f.Fetch(ctx, namespace, configMapName)
	if err != nil {
		return nil, err
	}
	return Parse(manifests)
}
