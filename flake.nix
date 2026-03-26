{
  description = "Consolidated monorepo flake for RSS microservices";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";

    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    git-hooks = {
      url = "github:cachix/pre-commit-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    go-overlay = {
      url = "github:purpleclay/go-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      rust-overlay,
      git-hooks,
      go-overlay,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        # Base pkgs without overlays
        pkgs = nixpkgs.legacyPackages.${system};

        # Pkgs with rust overlay for rss_locator
        pkgsWithRust = import nixpkgs {
          inherit system;
          overlays = [ (import rust-overlay) ];
        };

        # Pkgs with go-overlay for pinned Go versions and buildGoWorkspace
        pkgsWithGo = import nixpkgs {
          inherit system;
          overlays = [ go-overlay.overlays.default ];
        };

        callPackage = pkgs.callPackage;

        # govendor CLI — generates govendor.toml for workspace builds
        govendor = go-overlay.packages.${system}.govendor;

        # On Darwin, used to pull linux/aarch64 container runtime deps (e.g. nodejs)
        # from the nixpkgs binary cache rather than compiling them.
        linuxPkgs = if pkgs.stdenv.isDarwin then pkgs.pkgsCross.aarch64-multiplatform else pkgs;

        mkGoTest =
          {
            name,
            src,
          }:
          pkgs.stdenvNoCC.mkDerivation {
            name = "${name}-test";
            inherit src;
            dontBuild = true;
            __noChroot = true;
            doCheck = true;
            nativeBuildInputs = with pkgs; [
              go
              writableTmpDirAsHomeHook
            ];
            checkPhase = ''
              go test -v ./...
            '';
            installPhase = ''
              mkdir "$out"
            '';
          };

        # Create a Go lint check derivation
        mkGoLint =
          {
            name,
            src,
          }:
          pkgs.stdenvNoCC.mkDerivation {
            name = "${name}-lint";
            inherit src;
            dontBuild = true;
            doCheck = true;
            nativeBuildInputs = with pkgs; [
              golangci-lint
              go
              writableTmpDirAsHomeHook
            ];
            checkPhase = ''
              golangci-lint run
            '';
            installPhase = ''
              mkdir "$out"
            '';
          };

        # Create a docker build-and-load app
        mkDockerApp =
          {
            name,
            imageName,
          }:
          {
            type = "app";
            program = "${pkgs.writeShellScriptBin "build-and-load-${name}" ''
              nix build .#${name}-docker
              docker load < result
              echo "Docker image loaded as ${imageName}:latest"
            ''}/bin/build-and-load-${name}";
          };

        pre-commit-check = git-hooks.lib.${system}.run {
          src = ./.;
          hooks = {
            # Nix
            nixfmt-rfc-style.enable = true;

            # Git
            commitizen.enable = true;
            convco.enable = true;

            # Go
            # rss_poller and rss_notify each have their own go.mod and are separate
            # modules — exclude them from the root-module hooks.
            # They are covered by nix flake check (rss-*-test / rss-*-lint checks).
            gofmt.enable = true;
            gotest = {
              enable = true;
              excludes = [
                "^rss_poller/"
                "^rss_notify/"
              ];
            };
            golangci-lint = {
              enable = true;
              excludes = [
                "^rss_poller/"
                "^rss_notify/"
              ];
            };
          };
        };

        # Derive the pinned Go toolchain for each service from its go.mod.
        # go-overlay resolves the latest patch of the declared go directive.
        pollerGo = pkgsWithGo.go-bin.fromGoMod ./rss_poller/go.mod;
        notifyGo = pkgsWithGo.go-bin.fromGoMod ./rss_notify/go.mod;

        # Use pkgsWithGo.callPackage so buildGoWorkspace is auto-injected from the overlay
        rss-poller-out = pkgsWithGo.callPackage ./rss_poller { go = pollerGo; };
        rss-poller = rss-poller-out.package;
        rss-poller-docker = rss-poller-out.dockerImage;

        rss-notify-out = pkgsWithGo.callPackage ./rss_notify { go = notifyGo; };
        rss-notify = rss-notify-out.package;
        rss-notify-docker = rss-notify-out.dockerImage;

        rustToolchain = pkgsWithRust.rust-bin.stable.latest.default.override {
          extensions = [
            "rust-src"
            "rust-analyzer"
          ];
        };

        rss-locator-out = callPackage ./rss_locator {
          inherit pkgsWithRust;
          rustPlatform = pkgsWithRust.rustPlatform;
        };
        rss-locator = rss-locator-out.package;
        rss-locator-docker = rss-locator-out.dockerImage;

        rss-frontend-out = callPackage ./rss_frontend { inherit linuxPkgs; };
        rss-frontend = rss-frontend-out.package;
        rss-frontend-docker = rss-frontend-out.dockerImage;

        install_astro = pkgs.writeShellApplication {
          name = "install_astro";
          runtimeInputs = [ pkgs.nodejs ];
          text = ''
            npm create astro@latest
          '';
        };

        run_dev = pkgs.writeShellApplication {
          name = "run_dev";
          runtimeInputs = [ pkgs.nodejs ];
          text = ''
            npm run dev
          '';
        };

        # Context-aware DevHelp script generator
        mkDevHelp =
          {
            serviceName,
            serviceType,
          }:
          let
            header = ''
              echo ""
              echo "RSS Microservices - ${serviceName} Development Commands"
              echo "========================================="
              echo ""
            '';

            goCommands = ''
              echo "Go Commands:"
              echo "  go build ./...              Build the project"
              echo "  go test ./...               Run tests"
              echo "  go test -v ./...            Run tests (verbose)"
              echo "  go mod tidy                 Tidy go.mod"
              echo "  go work sync                Sync go.work.sum"
              echo "  golangci-lint run           Run linter"
              echo "  govendor                    Regenerate govendor.toml"
              echo ""
            '';

            rustCommands = ''
              echo "Rust/Cargo Commands:"
              echo "  cargo build                 Build the project"
              echo "  cargo build --release       Build release binary"
              echo "  cargo test                  Run tests"
              echo "  cargo clippy                Run linter"
              echo "  cargo fmt                   Format code"
              echo "  cargo fmt --check           Check formatting"
              echo "  cargo audit                 Check for vulnerabilities"
              echo "  cargo outdated              Check for outdated deps"
              echo ""
            '';

            nodejsCommands = ''
              echo "Node.js/npm Commands:"
              echo "  npm install                 Install dependencies"
              echo "  npm run dev                 Start development server"
              echo "  npm run build               Build for production"
              echo "  npm run preview             Preview production build"
              echo "  install_astro               Create new Astro project"
              echo "  run_dev                     Start dev server (helper)"
              echo ""
            '';

            rootCommands = ''
              echo "Orchestration:"
              echo "  arion up                    Start all services with arion"
              echo "  arion down                  Stop all services"
              echo "  arion up -d                 Start services in background"
              echo "  arion logs -f               Follow service logs"
              echo ""
              echo "Workspace:"
              echo "  go work sync                Sync go.work.sum"
              echo "  govendor                    Regenerate govendor.toml"
              echo ""
              echo "Build All Packages:"
              echo "  nix build .#rss-poller      Build the poller service"
              echo "  nix build .#rss-notify      Build the notify service"
              echo "  nix build .#rss-locator     Build the locator service (Rust)"
              echo "  nix build .#rss-frontend    Build the frontend (Node.js)"
              echo "  nix build                   Build all services"
              echo ""
              echo "Build All Docker Images:"
              echo "  nix build .#rss-poller-docker     Build poller container"
              echo "  nix build .#rss-notify-docker     Build notify container"
              echo "  nix build .#rss-locator-docker    Build locator container"
              echo "  nix build .#rss-frontend-docker   Build frontend container"
              echo ""
              echo "Build & Load All Docker Images:"
              echo "  nix run .#build-and-load-all      Build & load ALL images"
              echo ""
            '';

            serviceNixCommands =
              name:
              let
                nixName = builtins.replaceStrings [ "_" ] [ "-" ] name;
              in
              ''
                echo "Nix Build Commands:"
                echo "  nix build .#rss-${nixName}           Build the ${name} service"
                echo "  nix build .#rss-${nixName}-docker    Build ${name} container image"
                echo "  nix run .#build-and-load-${nixName}  Build & load docker image"
                echo ""
              '';

            goChecksCommands =
              name:
              let
                nixName = builtins.replaceStrings [ "_" ] [ "-" ] name;
              in
              ''
                echo "Nix Checks (Tests & Linting):"
                echo "  nix build .#checks.${system}.rss-${nixName}-test   Run ${name} tests"
                echo "  nix build .#checks.${system}.rss-${nixName}-lint   Run ${name} linter"
                echo "  nix flake check             Run all checks"
                echo ""
              '';

            switchShellsCommands = currentShell: ''
              echo "Switch Development Shells:"
              ${
                if currentShell != "poller" then
                  ''echo "  nix develop .#rss_poller    Enter poller dev environment (Go)"''
                else
                  ""
              }
              ${
                if currentShell != "notify" then
                  ''echo "  nix develop .#rss_notify    Enter notify dev environment (Go)"''
                else
                  ""
              }
              ${
                if currentShell != "locator" then
                  ''echo "  nix develop .#rss_locator   Enter locator dev environment (Rust)"''
                else
                  ""
              }
              ${
                if currentShell != "frontend" then
                  ''echo "  nix develop .#rss_frontend  Enter frontend dev environment (Node.js)"''
                else
                  ""
              }
              ${
                if currentShell != "root" then
                  ''echo "  nix develop                 Enter root dev environment (arion)"''
                else
                  ""
              }
              echo ""
            '';

            preCommitCommands = ''
              echo "Pre-commit:"
              echo "  nix build .#checks.${system}.pre-commit-check  Run pre-commit hooks"
              echo ""
            '';

            typeCommands =
              if serviceType == "go" then
                goCommands
              else if serviceType == "rust" then
                rustCommands
              else if serviceType == "nodejs" then
                nodejsCommands
              else if serviceType == "root" then
                rootCommands
              else
                "";

            nixCommands = if serviceType == "root" then "" else serviceNixCommands serviceName;

            checksCommands =
              if serviceType == "go" then
                goChecksCommands serviceName
              else if serviceType == "root" then
                ''
                  echo "Checks:"
                  echo "  nix flake check             Run all checks"
                  echo ""
                ''
              else
                "";

            shellName = if serviceType == "root" then "root" else serviceName;
          in
          pkgs.writeShellScriptBin "devhelp" ''
            ${header}
            ${typeCommands}
            ${nixCommands}
            ${checksCommands}
            ${switchShellsCommands shellName}
            ${preCommitCommands}
          '';

        # Custom shellHook that includes pre-commit and devhelp reminder
        mkShellHook = preCommitHook: ''
          ${preCommitHook}
          echo ""
          echo "Type 'devhelp' for available commands"
        '';

      in
      {
        packages = {
          inherit
            rss-poller
            rss-notify
            rss-locator
            rss-frontend
            ;

          # Docker images
          rss-poller-docker = rss-poller-docker;
          rss-notify-docker = rss-notify-docker;
          rss-locator-docker = rss-locator-docker;
          rss-frontend-docker = rss-frontend-docker;

          # Default: all packages
          default = pkgs.symlinkJoin {
            name = "all-services";
            paths = [
              rss-poller
              rss-notify
              rss-locator
              rss-frontend
            ];
          };
        };

        devShells = {
          # Go service shells — Go version is derived from each service's go.mod
          rss_poller = pkgsWithGo.callPackage ./rss_poller/shell.nix {
            go = pollerGo;
            shellHook = mkShellHook pre-commit-check.shellHook;
            enabledPackages = pre-commit-check.enabledPackages ++ [
              govendor
              (mkDevHelp {
                serviceName = "poller";
                serviceType = "go";
              })
            ];
          };

          rss_notify = pkgsWithGo.callPackage ./rss_notify/shell.nix {
            go = notifyGo;
            shellHook = mkShellHook pre-commit-check.shellHook;
            enabledPackages = pre-commit-check.enabledPackages ++ [
              govendor
              (mkDevHelp {
                serviceName = "notify";
                serviceType = "go";
              })
            ];
          };

          # Rust service shell
          rss_locator = pkgsWithRust.mkShell {
            shellHook = mkShellHook pre-commit-check.shellHook;
            buildInputs =
              with pkgsWithRust;
              [
                rustToolchain
                cargo
                cargo-edit
                cargo-outdated
                cargo-audit
                trivy
                dive
                (mkDevHelp {
                  serviceName = "locator";
                  serviceType = "rust";
                })
              ]
              ++ pre-commit-check.enabledPackages;
            RUST_BACKTRACE = "1";
          };

          # Node.js frontend shell
          rss_frontend = pkgs.mkShell {
            shellHook = mkShellHook pre-commit-check.shellHook;
            buildInputs =
              with pkgs;
              [
                install_astro
                nodejs
                run_dev
                (mkDevHelp {
                  serviceName = "frontend";
                  serviceType = "nodejs";
                })
              ]
              ++ pre-commit-check.enabledPackages;
          };

          # Root dev shell with arion, pre-commit, and govendor for workspace management
          default = pkgs.mkShell {
            shellHook = mkShellHook pre-commit-check.shellHook;
            buildInputs = [
              pkgs.arion
              pkgs.docker-compose
              pkgs.nixfmt-rfc-style
              govendor
              (mkDevHelp {
                serviceName = "root";
                serviceType = "root";
              })
            ]
            ++ pre-commit-check.enabledPackages;
          };
        };

        checks = {
          inherit pre-commit-check;

          # Go tests
          rss-poller-test = mkGoTest {
            name = "rss-poller";
            src = ./rss_poller;
          };
          rss-notify-test = mkGoTest {
            name = "rss-notify";
            src = ./rss_notify;
          };

          # Go lints
          rss-poller-lint = mkGoLint {
            name = "rss-poller";
            src = ./rss_poller;
          };
          rss-notify-lint = mkGoLint {
            name = "rss-notify";
            src = ./rss_notify;
          };
        };

        apps = {
          build-and-load-poller = mkDockerApp {
            name = "rss-poller";
            imageName = "rss_poller";
          };
          build-and-load-notify = mkDockerApp {
            name = "rss-notify";
            imageName = "rss_notify";
          };
          build-and-load-locator = mkDockerApp {
            name = "rss-locator";
            imageName = "rss_locator";
          };
          build-and-load-frontend = mkDockerApp {
            name = "rss-frontend";
            imageName = "rss_frontend";
          };

          build-and-load-all = {
            type = "app";
            program = "${pkgs.writeShellScriptBin "build-and-load-all" ''
              echo "Building and loading all Docker images..."
              nix build .#rss-poller-docker && docker load < result && echo "Loaded rss_poller:latest"
              nix build .#rss-notify-docker && docker load < result && echo "Loaded rss_notify:latest"
              nix build .#rss-locator-docker && docker load < result && echo "Loaded rss_locator:latest"
              nix build .#rss-frontend-docker && docker load < result && echo "Loaded rss_frontend:latest"
              echo "All Docker images loaded successfully!"
            ''}/bin/build-and-load-all";
          };
        };
      }
    );
}
