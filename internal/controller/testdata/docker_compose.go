package testdata

// BasicDockerCompose is a minimal single-service compose for basic tests
const BasicDockerCompose = `services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
`

// MultiServiceCompose includes both a Deployment (web) and Pod (migrate with restart: "no")
const MultiServiceCompose = `services:
  web:
    image: nginx:latest
  migrate:
    image: migrate:latest
    restart: "no"
`

// ComplexStackCompose represents a more realistic multi-service application
const ComplexStackCompose = `services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
  postgres:
    image: postgres:15
    ports:
      - "5432"
  redis:
    image: redis:7
    ports:
      - "6379"
`

// ComposeWithEnvVars includes predefined environment variables
const ComposeWithEnvVars = `services:
  web:
    image: nginx:latest
    environment:
      - COMPOSE_VAR=from-compose
      - PORT=8080
      - DEBUG=false
`

// ComposeForHierarchyTest used for testing variable/secret hierarchy
const ComposeForHierarchyTest = `services:
  app:
    image: myapp:latest
    environment:
      - BASE_VAR=should-keep
`

// ComposeWithConflictingVars for testing conflict resolution
const ComposeWithConflictingVars = `services:
  web:
    image: nginx:latest
    environment:
      - DATABASE_HOST=localhost
      - PORT=8080
      - DEBUG=false
`
