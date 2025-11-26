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

package util

import "strings"

// ParseBlueprintReference parses blueprint reference in format:
// - "name" -> uses defaultNamespace
// - "global/name" -> uses globalNamespace
// - "scope/name" -> resolves scope to namespace
//
// Returns the resolved namespace and blueprint name.
func ParseBlueprintReference(ref, defaultNamespace, globalNamespace string) (namespace, name string) {
	namespace = defaultNamespace
	name = ref

	// Look for scope/name format
	if idx := strings.IndexByte(ref, '/'); idx != -1 {
		scope := ref[:idx]
		name = ref[idx+1:]

		if scope == "global" {
			namespace = globalNamespace
		}
		// Can be extended for other scopes in the future
	}

	return namespace, name
}
