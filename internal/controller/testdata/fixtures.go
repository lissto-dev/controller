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
	return NewManifestsConfigMapWithContent(namespace, name, DeploymentAndPodManifest)
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

// NewLisstoVariable creates a LisstoVariable for testing config injection
func NewLisstoVariable(namespace, name string, data map[string]string) *envv1alpha1.LisstoVariable {
	return &envv1alpha1.LisstoVariable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.LisstoVariableSpec{
			Data: data,
		},
	}
}

// NewLisstoSecret creates a LisstoSecret for testing secret injection
func NewLisstoSecret(namespace, name string, keys []string) *envv1alpha1.LisstoSecret {
	return &envv1alpha1.LisstoSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.LisstoSecretSpec{
			Keys: keys,
		},
	}
}

// NewEnvScopedVariable creates a LisstoVariable with env scope
func NewEnvScopedVariable(namespace, name, env string, data map[string]string) *envv1alpha1.LisstoVariable {
	return &envv1alpha1.LisstoVariable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.LisstoVariableSpec{
			Scope: "env",
			Env:   env,
			Data:  data,
		},
	}
}

// NewRepoScopedVariable creates a LisstoVariable with repo scope
func NewRepoScopedVariable(namespace, name, repository string, data map[string]string) *envv1alpha1.LisstoVariable {
	return &envv1alpha1.LisstoVariable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.LisstoVariableSpec{
			Scope:      "repo",
			Repository: repository,
			Data:       data,
		},
	}
}

// NewGlobalScopedVariable creates a LisstoVariable with global scope
func NewGlobalScopedVariable(namespace, name string, data map[string]string) *envv1alpha1.LisstoVariable {
	return &envv1alpha1.LisstoVariable{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.LisstoVariableSpec{
			Scope: "global",
			Data:  data,
		},
	}
}

// NewEnvScopedSecret creates a LisstoSecret with env scope
func NewEnvScopedSecret(namespace, name, env string, keys []string, secretRef string) *envv1alpha1.LisstoSecret {
	return &envv1alpha1.LisstoSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.LisstoSecretSpec{
			Scope:     "env",
			Env:       env,
			Keys:      keys,
			SecretRef: secretRef,
		},
	}
}

// NewRepoScopedSecret creates a LisstoSecret with repo scope
func NewRepoScopedSecret(namespace, name, repository string, keys []string, secretRef string) *envv1alpha1.LisstoSecret {
	return &envv1alpha1.LisstoSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.LisstoSecretSpec{
			Scope:      "repo",
			Repository: repository,
			Keys:       keys,
			SecretRef:  secretRef,
		},
	}
}

// NewGlobalScopedSecret creates a LisstoSecret with global scope
func NewGlobalScopedSecret(namespace, name string, keys []string, secretRef string) *envv1alpha1.LisstoSecret {
	return &envv1alpha1.LisstoSecret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: envv1alpha1.LisstoSecretSpec{
			Scope:     "global",
			Keys:      keys,
			SecretRef: secretRef,
		},
	}
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
