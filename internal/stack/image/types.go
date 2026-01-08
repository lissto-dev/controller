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

import "fmt"

// Warning tracks image injection issues
type Warning struct {
	Service         string
	Deployment      string
	TargetContainer string
	Message         string
}

// String returns a formatted warning message
func (w Warning) String() string {
	if w.Service != "" {
		return fmt.Sprintf("Service '%s' (deployment '%s'): %s", w.Service, w.Deployment, w.Message)
	}
	return fmt.Sprintf("Deployment '%s': %s", w.Deployment, w.Message)
}

// FormatWarnings converts warnings to string messages
func FormatWarnings(warnings []Warning) []string {
	messages := make([]string, 0, len(warnings))
	for _, w := range warnings {
		messages = append(messages, w.String())
	}
	return messages
}
