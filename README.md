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

### Local Development

To start local development for any of the services, navigate to the respective service directory and use the following command:
```sh
nix develop
```

Or if using direnv
```sh
direnv allow
```
> [!NOTE]
> In case the development is being done to the backend microservices it is needed to start jaeger by running at the top of the repo
```bash
nix-shell --comand "arion up -d"
```
The jaeger interface will be available at http://localhost:16686/

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

To run any of the services, you can build a Docker image and run it.

#### Example: Running `rss_notify` in Docker

1. Build the Docker image:
   ```sh
   nix build .#dockerImage
   docker load < result
   ```

2. Run the Docker container:
   ```sh
   docker run --rm -p 3001:3000 -e OTEL_EP="localhost:4317" rss_notify:latest
   ```

#### Example: Running `rss_poller` in Docker

1. Build the Docker image:
   ```sh
   nix build .#dockerImage
   docker load < result
   ```

2. Run the Docker container:
   ```sh
   docker run --rm -p 3000:3000 -e OTEL_EP="localhost:4317" -e NOTIFICATION_ENDPOINT="http://127.0.0.1:3001/push" -e NOTIFICATION_SENDER="<discord_webhook>" rss_poller:latest
   ```

#### Example: Running `rss_frontend` in Docker

1. Build the Docker image:
   ```sh
   nix build .#dockerImage
   docker load < result
   ```

2. Run the Docker container:
   ```sh
   docker run --rm -p 4321:4321 -e POLLER_ENDPOINT="http://127.0.0.1:3000/rss" rss_frontend:latest
   ```
