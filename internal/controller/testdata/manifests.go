package testdata

// BasicManifest is a simple Service manifest for basic tests
const BasicManifest = `apiVersion: v1
kind: Service
metadata:
  name: test-service
spec:
  selector:
    app: test
  ports:
  - port: 80
`

// DeploymentAndPodManifest contains both a Deployment and a Pod
// Used for testing image and config injection into both resource types
const DeploymentAndPodManifest = `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  labels:
    io.kompose.service: web
spec:
  replicas: 1
  selector:
    matchLabels:
      io.kompose.service: web
  template:
    metadata:
      labels:
        io.kompose.service: web
    spec:
      containers:
      - name: web
        image: nginx:latest
        env: []
---
apiVersion: v1
kind: Pod
metadata:
  name: migrate
  labels:
    io.kompose.service: migrate
spec:
  restartPolicy: Never
  containers:
  - name: migrate
    image: migrate:latest
    env: []
`

// DeploymentManifest is a simple Deployment for testing
const DeploymentManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  labels:
    io.kompose.service: web
spec:
  replicas: 1
  selector:
    matchLabels:
      io.kompose.service: web
  template:
    metadata:
      labels:
        io.kompose.service: web
    spec:
      containers:
      - name: web
        image: nginx:latest
        ports:
        - containerPort: 80
        env: []
`

// DeploymentWithEnvVarsManifest is a Deployment with predefined env vars (from compose)
const DeploymentWithEnvVarsManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  labels:
    io.kompose.service: web
spec:
  replicas: 1
  selector:
    matchLabels:
      io.kompose.service: web
  template:
    metadata:
      labels:
        io.kompose.service: web
    spec:
      containers:
      - name: web
        image: nginx:latest
        env:
        - name: COMPOSE_VAR
          value: from-compose
        - name: PORT
          value: "8080"
        - name: DEBUG
          value: "false"
`

// DeploymentWithConflictingVarsManifest for testing last-one-wins
// Note: Manifests from kompose would have these as initial env vars
// When LisstoVariables are injected, they get appended
// K8s doesn't allow duplicates in Apply, so this test verifies both are added
const DeploymentWithConflictingVarsManifest = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  labels:
    io.kompose.service: web
spec:
  replicas: 1
  selector:
    matchLabels:
      io.kompose.service: web
  template:
    metadata:
      labels:
        io.kompose.service: web
    spec:
      containers:
      - name: web
        image: nginx:latest
        env:
        - name: DATABASE_HOST
          value: localhost
        - name: PORT
          value: "8080"
        - name: DEBUG
          value: "false"
`
