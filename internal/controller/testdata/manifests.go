package testdata

import (
	"bytes"
	"text/template"
)

// BasicManifest is a simple Service manifest for basic tests
// Kept as constant since it's a different resource type (Service, not Deployment/Pod)
const BasicManifest = `apiVersion: v1
kind: Service
metadata:
  name: test-service
  annotations:
    lissto.dev/class: state
spec:
  selector:
    app: test
  ports:
  - port: 80
`

// EnvVar represents an environment variable with name and optional value
type EnvVar struct {
	Name  string
	Value string
}

// Template definitions
var (
	deploymentTemplate = template.Must(template.New("deployment").Parse(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Name }}
  labels:
    io.kompose.service: {{ .Name }}
  annotations:
    lissto.dev/class: workload
spec:
  replicas: 1
  selector:
    matchLabels:
      io.kompose.service: {{ .Name }}
  template:
    metadata:
      labels:
        io.kompose.service: {{ .Name }}
    spec:
      containers:
      - name: {{ .Name }}
        image: nginx:latest
{{- if .EnvVars }}
        env:
{{- range .EnvVars }}
        - name: {{ .Name }}
          value: {{ .Value | printf "%q" }}
{{- end }}
{{- else }}
        env: []
{{- end }}
`))

	podTemplate = template.Must(template.New("pod").Parse(`apiVersion: v1
kind: Pod
metadata:
  name: {{ .Name }}
  labels:
    io.kompose.service: {{ .Name }}
  annotations:
    lissto.dev/class: workload
spec:
  restartPolicy: Never
  containers:
  - name: {{ .Name }}
    image: migrate:latest
{{- if .EnvVars }}
    env:
{{- range .EnvVars }}
    - name: {{ .Name }}
      value: {{ .Value | printf "%q" }}
{{- end }}
{{- else }}
    env: []
{{- end }}
`))
)

// manifestData holds template data for manifest generation
type manifestData struct {
	Name    string
	EnvVars []EnvVar
}

// NewDeploymentManifest creates a Deployment manifest with specified environment variables
// If envVars is empty, creates a deployment with env: []
func NewDeploymentManifest(name string, envVars []EnvVar) string {
	var buf bytes.Buffer
	data := manifestData{Name: name, EnvVars: envVars}
	if err := deploymentTemplate.Execute(&buf, data); err != nil {
		panic(err) // Should never happen with valid template
	}
	return buf.String()
}

// NewDeploymentManifestWithEnvNames creates a Deployment with empty-valued env vars
// Useful for opt-in testing where you just need the names declared
func NewDeploymentManifestWithEnvNames(name string, envNames ...string) string {
	envVars := make([]EnvVar, len(envNames))
	for i, envName := range envNames {
		envVars[i] = EnvVar{Name: envName, Value: ""}
	}
	return NewDeploymentManifest(name, envVars)
}

// NewPodManifest creates a Pod manifest with specified environment variables
func NewPodManifest(name string, envVars []EnvVar) string {
	var buf bytes.Buffer
	data := manifestData{Name: name, EnvVars: envVars}
	if err := podTemplate.Execute(&buf, data); err != nil {
		panic(err) // Should never happen with valid template
	}
	return buf.String()
}

// NewDeploymentAndPodManifest creates both Deployment and Pod with specified env vars
func NewDeploymentAndPodManifest(deploymentName, podName string, deploymentEnv, podEnv []EnvVar) string {
	deployment := NewDeploymentManifest(deploymentName, deploymentEnv)
	pod := NewPodManifest(podName, podEnv)
	return deployment + "---\n" + pod
}

// Helper functions for common patterns

// EmptyEnv creates an empty env var (name with empty value)
func EmptyEnv(name string) EnvVar {
	return EnvVar{Name: name, Value: ""}
}

// Env creates an env var with a value
func Env(name, value string) EnvVar {
	return EnvVar{Name: name, Value: value}
}
