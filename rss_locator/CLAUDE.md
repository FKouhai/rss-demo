# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this service does

`rss_locator` is a service registry (service locator) written in Rust. Other microservices in the monorepo register their FQDNs with it, and can query it to discover each other's addresses. It is part of a larger monorepo at the parent directory (`cloudnative_nix/`) which also contains `rss_poller`, `rss_notify`, and `rss_frontend` (Go/Astro services).

## Commands

### Development shell
```sh
nix develop
```

### Build
```sh
cargo build
# or via Nix
nix build
```

### Run tests
```sh
cargo test -- --test-threads=1
```

### Run a single test
```sh
cargo test <test_name> -- --test-threads=1
```

### Lint / format
```sh
cargo clippy
cargo fmt
```

### Build and load Docker image
```sh
nix run .#build-and-load-docker
```

## Architecture

The service exposes an HTTP API on port `3000` using [actix-web](https://actix.rs/).

**Key modules:**

- `src/main.rs` — Entry point. Initializes `env_logger`, sets up OpenTelemetry tracing, creates the shared `PhoneBook` behind an `Arc<Mutex<>>`, and mounts all routes.
- `src/handlers.rs` — Three actix-web handlers:
  - `POST /register` — Register a service name → FQDN mapping
  - `POST /services` — Look up a service name to get its FQDN
  - `GET /health` — Health check
- `src/phonebook/phonebook.rs` — In-memory `HashMap<String, String>` store (service name → FQDN). The `register()` method enforces that a given FQDN cannot be claimed by two different services, but allows re-registration (idempotent update) for the same service name.
- `src/models.rs` — Serde request/response structs (`RegisterRequest`, `ServiceRequest`, `FqdnResponse`, `SuccessResponse`, `ErrorResponse`).
- `src/telemetry.rs` — Initializes an OTLP gRPC trace exporter pointed at `OTEL_EP` env var (default `localhost:4317`).

**Tests:**

- Unit tests: `src/phonebook/phonebook_test.rs` — tests `PhoneBook` logic directly.
- Integration tests: `tests/integration_tests.rs` — spins up actix-web test services to test handler behaviour end-to-end (no real network).

**Environment variables:**

| Variable | Default | Purpose |
|---|---|---|
| `OTEL_EP` | `localhost:4317` | OpenTelemetry collector gRPC endpoint |
| `SERVICE_VERSION` | `0.1.0` | Reported in trace resource attributes |
| `ENV` | `development` | Deployment environment tag |
| `RUST_LOG` | `info` | Log level (via `env_logger`) |

## Nix build

The `flake.nix` provides:
- `packages.default` — release build of the binary
- `dockerImage` — layered OCI image (`rss_locator:latest`)
- `devShells.default` — shell with `rustToolchain`, `cargo-edit`, `cargo-outdated`, `cargo-audit`, `trivy`, `dive`
- `apps.build-and-load-docker` — builds Docker image and loads it into the local Docker daemon
