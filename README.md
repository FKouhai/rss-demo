[![build frontend](https://github.com/FKouhai/rss-demo/actions/workflows/build_frontend.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_frontend.yaml)
[![build frontend container](https://github.com/FKouhai/rss-demo/actions/workflows/build_frontend_container.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_frontend_container.yaml)
[![build notify](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify.yaml)[![build notify](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify.yaml)
[![build notify container](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify_container.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_notify_container.yaml)
[![build poller](https://github.com/FKouhai/rss-demo/actions/workflows/build_poller.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_poller.yaml)
[![build poller container](https://github.com/FKouhai/rss-demo/actions/workflows/build_poller_container.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/build_poller_container.yaml)
[![Lint and test PR's for notify](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_notify.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_notify.yaml)
[![Lint and test PR's for poller](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_poller.yaml/badge.svg)](https://github.com/FKouhai/rss-demo/actions/workflows/lint_and_test_poller.yaml)
<h1 align="center">
  <div>
         <img href="https://builtwithnix.org" src="https://builtwithnix.org/badge.svg"/>
    </div>
</h1>

# RSS Microservices Monorepo

## Overview

This repository contains the source code for the microservices responsible for handling RSS notifications, polling, and serving the frontend. The services are written in Go and Astro, and built with Nix.

## Services

1. **rss_notify**
   - **Description**: Notification service written in Go.
   - **Directory**: [rss_notify](./rss_notify)
   - **Key Features**:
     - Handles sending notifications for RSS feeds.
     - Built and tested using Nix and Go modules.

2. **rss_poller**
   - **Description**: Poller service written in Go.
   - **Directory**: [rss_poller](./rss_poller)
   - **Key Features**:
     - Periodically polls RSS feeds to check for updates.
     - Built and tested using Nix and Go modules.

3. **rss_frontend**
   - **Description**: Frontend service built with Astro.
   - **Directory**: [rss_frontend](./rss_frontend)
   - **Key Features**:
     - Provides a user interface for interacting with the RSS services.
     - Built using Nix and Astro, a modern frontend framework.

## Technologies Used

- **Programming Languages**: Go, JavaScript (Astro)
- **Build System**: Nix
- **Containerization**: Docker

## Installation and Usage

### Prerequisites

- [Nix](https://nixos.org/download.html) installed on your system.
- Basic knowledge of Nix, Go, and Astro.

### Local Development with Arion

For easier local development and testing, this repository includes Arion configuration files that allow you to run all services together with their dependencies.

#### Prerequisites for Arion

- [Nix](https://nixos.org/download.html) installed on your system
- Docker installed and running
- Arion installed (automatically available through Nix)

#### Quick Start with Arion

1. Build and load Docker images for all services:
   ```bash
   # In the rss_notify directory
   nix run .#build-and-load-docker
   
   # In the rss_poller directory
   nix run .#build-and-load-docker
   
   # In the rss_frontend directory
   nix run .#build-and-load-docker
   ```

2. Start all services with dependencies:
   ```bash
   # At the root of the repository
   nix-shell -p arion --run "arion up -d"
   ```

3. Access the services:
   - Frontend: http://localhost:4321
   - RSS Poller API: http://localhost:3000
   - RSS Notification Service: http://localhost:3001
   - Valkey: localhost:6379

4. View logs:
   ```bash
   nix-shell -p arion --run "arion logs -f"
   ```

5. Stop services:
   ```bash
   nix-shell -p arion --run "arion down"
   ```

#### Custom Shell Commands

Each service includes a custom shell command to build and load its Docker image:

- `nix run .#build-and-load-docker` - Build the Docker image and load it into Docker daemon

This command simplifies the process of building and loading images during development.

### Building Services

To build any of the services, navigate to the respective directory and use the following command:

```sh
nix build
```

This will create a development shell with all necessary dependencies.

#### Example: Building `rss_notify`

1. Navigate to the `rss_notify` directory:
   ```sh
   cd rss_notify
   ```

3. Build and test the service:
   ```sh
   nix flake check
   nix build
   ```

#### Example: Building `rss_poller`

1. Navigate to the `rss_frontend` directory:
   ```sh
   cd rss_poller
   ```

3. Build and test the microservice
   ```sh
   nix flake check
   nix build
   ```

#### Example: Building `rss_frontend`

1. Navigate to the `rss_frontend` directory:
   ```sh
   cd rss_frontend
   ```

2. Build the frontend microservice
   ```sh
   nix build
   ```

### Running Services

To run any of the services, you can either build and run a Docker image directly, or use the custom shell command to build and load the image.

#### Option 1: Using Custom Shell Command (Recommended)

Each service includes a custom shell command that builds and loads the Docker image:

1. Build and load the Docker image:
   ```sh
   nix run .#build-and-load-docker
   ```

2. Run the Docker container:
   ```sh
   docker run --rm -p 3001:3000 -e OTEL_EP="localhost:4317" rss_notify:latest
   ```

#### Option 2: Manual Build and Run

1. Build the Docker image:
   ```sh
   nix build .#dockerImage
   docker load < result
   ```

2. Run the Docker container:
   ```sh
   docker run --rm -p 3001:3000 -e OTEL_EP="localhost:4317" rss_notify:latest
   ```
