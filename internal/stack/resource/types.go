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

package resource

// Result tracks the outcome of applying a single resource
type Result struct {
	Kind    string
	Name    string
	Applied bool
	Error   error
}

// HasFailures returns true if any result has an error
func HasFailures(results []Result) bool {
	for _, r := range results {
		if !r.Applied {
			return true
		}
	}
	return false
}

// CountApplied returns the number of successfully applied resources
func CountApplied(results []Result) int {
	count := 0
	for _, r := range results {
		if r.Applied {
			count++
		}
	}
	return count
}
