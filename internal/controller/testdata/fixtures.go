package testdata

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	envv1alpha1 "github.com/lissto-dev/controller/api/v1alpha1"
)

// Common test constants
const (
	TestBlueprintHash = "abc123def456789012345678901234567890123456789012345678901234"
	TestShortHash     = "abc12345"
)

// NewBlueprint creates a Blueprint with realistic metadata for testing
func NewBlueprint(namespace, name string) *envv1alpha1.Blueprint {
	return &envv1alpha1.Blueprint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"hash": TestShortHash,
			},
			Annotations: map[string]string{
				"lissto.dev/repository": "test/repo",
				"lissto.dev/title":      "Test Application",
			},
		},
		Spec: envv1alpha1.BlueprintSpec{
			DockerCompose: BasicDockerCompose,
			Hash:          TestBlueprintHash,
		},
	}
}

// NewBlueprintWithCompose creates a Blueprint with custom docker-compose content
func NewBlueprintWithCompose(namespace, name, dockerCompose string) *envv1alpha1.Blueprint {
	bp := NewBlueprint(namespace, name)
	bp.Spec.DockerCompose = dockerCompose
	return bp
}

// NewManifestsConfigMap creates a ConfigMap with Kubernetes manifests for testing
func NewManifestsConfigMap(namespace, name string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by": "lissto",
			},
		},
		Data: map[string]string{
			"manifests.yaml": BasicManifest,
		},
	}
}

// NewManifestsConfigMapWithContent creates a ConfigMap with custom manifest content
func NewManifestsConfigMapWithContent(namespace, name, manifestContent string) *corev1.ConfigMap {
	cm := NewManifestsConfigMap(namespace, name)
	cm.Data["manifests.yaml"] = manifestContent
	return cm
}

// NewDeploymentAndPodManifestsConfigMap creates a ConfigMap with both Deployment and Pod manifests
// Used for integration tests that verify both resource types
func NewDeploymentAndPodManifestsConfigMap(namespace, name string) *corev1.ConfigMap {
	manifest := NewDeploymentAndPodManifest("web", "migrate", nil, nil) // Empty env vars
	return NewManifestsConfigMapWithContent(namespace, name, manifest)
}

// NewStack creates a Stack without images (for basic reconciliation tests)
func NewStack(namespace, name, blueprintRef, configMapName string) *envv1alpha1.Stack {
	return &envv1alpha1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.StackSpec{
			Env:                   "test",
			BlueprintReference:    blueprintRef,
			ManifestsConfigMapRef: configMapName,
			Images:                map[string]envv1alpha1.ImageInfo{},
		},
	}
}

// NewStackWithImages creates a Stack with pre-populated image information
// Useful for testing image injection functionality
func NewStackWithImages(namespace, name, blueprintRef, configMapName string) *envv1alpha1.Stack {
	return &envv1alpha1.Stack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.StackSpec{
			Env:                   "test",
			BlueprintReference:    blueprintRef,
			ManifestsConfigMapRef: configMapName,
			Images: map[string]envv1alpha1.ImageInfo{
				"web": {
					Image:         "nginx:latest",
					Digest:        "nginx@sha256:abc123def456",
					ContainerName: "web",
				},
				"migrate": {
					Image:         "migrate:latest",
					Digest:        "migrate@sha256:xyz789uvw012",
					ContainerName: "migrate",
				},
			},
		},
	}
}

// NewLisstoVariable creates a LisstoVariable with specified scope
// scope: "env", "repo", or "global"
// scopeValue: env name for "env" scope, repository for "repo" scope, empty for "global"
func NewLisstoVariable(namespace, name, scope, scopeValue string, data map[string]string) *envv1alpha1.LisstoVariable {
	spec := envv1alpha1.LisstoVariableSpec{Scope: scope, Data: data}
	switch scope {
	case "env":
		spec.Env = scopeValue
	case "repo":
		spec.Repository = scopeValue
	}
	return &envv1alpha1.LisstoVariable{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       spec,
	}
}

// NewLisstoSecret creates a LisstoSecret with specified scope
// scope: "env", "repo", or "global"
// scopeValue: env name for "env" scope, repository for "repo" scope, empty for "global"
func NewLisstoSecret(namespace, name, scope, scopeValue string, keys []string, secretRef string) *envv1alpha1.LisstoSecret {
	spec := envv1alpha1.LisstoSecretSpec{Scope: scope, Keys: keys, SecretRef: secretRef}
	switch scope {
	case "env":
		spec.Env = scopeValue
	case "repo":
		spec.Repository = scopeValue
	}
	return &envv1alpha1.LisstoSecret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       spec,
	}
}

// Backward compatibility wrappers (can be removed if you update all tests)
func NewEnvScopedVariable(namespace, name, env string, data map[string]string) *envv1alpha1.LisstoVariable {
	return NewLisstoVariable(namespace, name, "env", env, data)
}

func NewRepoScopedVariable(namespace, name, repository string, data map[string]string) *envv1alpha1.LisstoVariable {
	return NewLisstoVariable(namespace, name, "repo", repository, data)
}

func NewGlobalScopedVariable(namespace, name string, data map[string]string) *envv1alpha1.LisstoVariable {
	return NewLisstoVariable(namespace, name, "global", "", data)
}

func NewEnvScopedSecret(namespace, name, env string, keys []string, secretRef string) *envv1alpha1.LisstoSecret {
	return NewLisstoSecret(namespace, name, "env", env, keys, secretRef)
}

func NewRepoScopedSecret(namespace, name, repository string, keys []string, secretRef string) *envv1alpha1.LisstoSecret {
	return NewLisstoSecret(namespace, name, "repo", repository, keys, secretRef)
}

func NewGlobalScopedSecret(namespace, name string, keys []string, secretRef string) *envv1alpha1.LisstoSecret {
	return NewLisstoSecret(namespace, name, "global", "", keys, secretRef)
}

// NewKubernetesSecret creates a K8s Secret for testing LisstoSecret resolution
func NewKubernetesSecret(namespace, name string, data map[string]string) *corev1.Secret {
	secretData := make(map[string][]byte)
	for k, v := range data {
		secretData[k] = []byte(v)
	}
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Data: secretData,
	}
}

// NewDeploymentConfigMap creates a ConfigMap with a dynamically generated Deployment manifest
func NewDeploymentConfigMap(namespace, name, deploymentName string, envVars []EnvVar) *corev1.ConfigMap {
	manifest := NewDeploymentManifest(deploymentName, envVars)
	return NewManifestsConfigMapWithContent(namespace, name, manifest)
}

// NewDeploymentConfigMapWithEnvNames creates a ConfigMap with a Deployment that has named env vars (empty values)
func NewDeploymentConfigMapWithEnvNames(namespace, name, deploymentName string, envNames ...string) *corev1.ConfigMap {
	manifest := NewDeploymentManifestWithEnvNames(deploymentName, envNames...)
	return NewManifestsConfigMapWithContent(namespace, name, manifest)
}
