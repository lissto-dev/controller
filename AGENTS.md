# Controller Build Instructions

Always use `make` commands: `make build`, `make test`, `make lint`, `make manifests`

Before opening a PR: `make lint && make test && make build`

## Writing Tests

Tests use **Ginkgo v2 + Gomega** (BDD-style). Create a `*_suite_test.go` file per package, then use `Describe/Context/It` blocks with Gomega matchers (e.g., `Expect(value).To(Equal(expected))`).

