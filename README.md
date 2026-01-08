# What is Lissto

Lissto is a DevEnv and DevEx(perience) platform that simplifies the development of applications on Kubernetes.
It bridges the gap between Compose loved by developers and Kubernetes loved by DevOps and platform engineers.

# Lissto Controller

The Kubernetes controller component of the Lissto platform. Manages Blueprint, Env, and Stack custom resources.

## Requirements

- **Kubernetes 1.28+** (required for ValidatingAdmissionPolicy)

## Quick Start

Build:
```bash
make build
```

Run locally:
```bash
make run
```

Deploy to cluster:
```bash
make install    # Install CRDs
make deploy IMG=<registry>/controller:tag
```

## Development

Run `make help` for all available targets.

## Support

- Issues: [GitHub Issues](https://github.com/lissto-dev/controller/issues)

## License

Copyright 2025 Lissto.

Licensed under the [Sustainable Use License v1.0](LICENSE.md).

For commercial use, please contact: hello@lissto.dev
