[![build frontend](https://github.com/FKouhai/rss-demo/actions/workflows/build_frontend.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_frontend.yaml)
[![build frontend container](https://github.com/FKouhai/rss-demo/actions/workflows/build_frontend_container.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_frontend_container.yaml)
[![build notify](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify.yaml)
[![build notify container](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify_container.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify_container.yaml)
[![build poller](https://github.com/FKouhai/rss-demo/actions/workflows/build_poller.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_poller.yaml)
[![build poller container](https://github.com/FKouhai/rss-demo/actions/workflows/build_poller_container.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_poller_container.yaml)
[![build locator](https://github.com/FKouhai/rss-demo/actions/workflows/build_locator.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_locator.yaml)
[![build locator container](https://github.com/FKouhai/rss-demo/actions/workflows/build_locator_container.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_locator_container.yaml)
[![Lint and test PR's for notify](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_notify.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_notify.yaml)
[![Lint and test PR's for poller](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_poller.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_poller.yaml)
[![Lint and test PR's for locator](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_locator.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_locator.yaml)
<h1 align="center">
  <div>
         <img href="https://builtwithnix.org" src="https://builtwithnix.org/badge.svg"/>
    </div>
</h1>

# RSS Microservices Monorepo

## Overview

This repository contains the source code for the microservices responsible for handling RSS notifications, polling, service discovery, and serving the frontend. The services are written in Go, Rust, and Astro, and built with Nix using a consolidated monorepo flake.

## Services

| Service | Language | Description |
|---------|----------|-------------|
| [rss_poller](./rss_poller) | Go | Periodically polls RSS feeds to check for updates |
| [rss_notify](./rss_notify) | Go | Handles sending notifications for RSS feeds |
| [rss_locator](./rss_locator) | Rust | Service registry for microservice discovery |
| [rss_frontend](./rss_frontend) | Astro | User interface for interacting with RSS services |
| [rss_config](./rss_config) | Go | Configuration service (deprecated) |

## Technologies Used

- **Programming Languages**: Go, Rust, JavaScript (Astro)
- **Build System**: Nix (consolidated monorepo flake)
- **Containerization**: Docker (Nix-built layered images)
- **Orchestration**: Arion (docker-compose via Nix)

## Development

### Prerequisites

- [Nix](https://nixos.org/download.html) with flakes enabled
- Docker (for container builds and orchestration)

### Development Shells

The repository provides context-aware development shells for each service. Enter a shell and run `devhelp` to see available commands specific to that service:

```bash
# Root shell (orchestration, all services overview)
nix develop

# Go services
nix develop .#rss_poller
nix develop .#rss_notify
nix develop .#rss_config

# Rust service
nix develop .#rss_locator

# Node.js/Astro frontend
nix develop .#rss_frontend
```

Each shell includes:
- Language-specific tooling (Go, Rust/Cargo, or Node.js/npm)
- Service-specific nix build commands
- Pre-commit hooks
- Commands to switch between shells

### Building Services

Build individual services or all at once from the repository root:

```bash
# Build individual services
nix build .#rss-poller
nix build .#rss-notify
nix build .#rss-locator
nix build .#rss-frontend

# Build all services
nix build
```

### Docker Images

Build Docker images for deployment:

```bash
# Build container images
nix build .#rss-poller-docker
nix build .#rss-notify-docker
nix build .#rss-locator-docker
nix build .#rss-frontend-docker

# Build and load into local Docker daemon
nix run .#build-and-load-poller
nix run .#build-and-load-notify
nix run .#build-and-load-locator
nix run .#build-and-load-frontend

# Build and load ALL images
nix run .#build-and-load-all
```

### Running Tests and Lints

```bash
# Run all checks (tests, lints, pre-commit)
nix flake check

# Run specific checks
nix build .#checks.x86_64-linux.rss-poller-test
nix build .#checks.x86_64-linux.rss-poller-lint
nix build .#checks.x86_64-linux.rss-notify-test
nix build .#checks.x86_64-linux.rss-notify-lint
nix build .#checks.x86_64-linux.pre-commit-check
```

### Local Development with Arion

Run all services together with their dependencies:

```bash
# Enter root dev shell
nix develop

# Start all services
arion up -d

# View logs
arion logs -f

# Stop services
arion down
```

#### Service Endpoints

| Service | URL |
|---------|-----|
| Frontend | http://localhost:4321 |
| Poller API | http://localhost:3000 |
| Notify Service | http://localhost:3001 |
| Locator Service | http://localhost:3002 |
