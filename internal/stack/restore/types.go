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

package restore

// Result tracks the result of a volume restore operation
type Result struct {
	Service      string
	PVCName      string
	SnapshotName string
	JobName      string
	Status       string // "pending", "running", "completed", "failed"
	Error        error
}
