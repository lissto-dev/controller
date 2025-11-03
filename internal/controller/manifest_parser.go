package controller

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// parseManifests parses multi-document YAML into unstructured objects
func parseManifests(manifestsYAML string) ([]*unstructured.Unstructured, error) {
	// Split by YAML document separator
	docs := strings.Split(manifestsYAML, "---\n")

	var objects []*unstructured.Unstructured
	for i, doc := range docs {
		doc = strings.TrimSpace(doc)
		if doc == "" {
			continue
		}

		obj := &unstructured.Unstructured{}
		if err := yaml.Unmarshal([]byte(doc), obj); err != nil {
			return nil, fmt.Errorf("failed to parse manifest document %d: %w", i, err)
		}

		// Skip empty objects
		if obj.GetKind() == "" {
			continue
		}

		objects = append(objects, obj)
	}

	return objects, nil
}
