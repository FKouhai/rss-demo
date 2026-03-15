{
  description = "Consolidated monorepo flake for RSS microservices";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";

    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };

    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    git-hooks = {
      url = "github:cachix/pre-commit-hooks.nix";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      gomod2nix,
      rust-overlay,
      git-hooks,
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

        # macOS compatibility helper
        callPackage = pkgs.darwin.apple_sdk_11_0.callPackage or pkgs.callPackage;
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

        # Create a Docker image for a package
        mkDockerImage =
          {
            name,
            pkg,
            binName ? name,
            extraContents ? [ ],
            extraConfig ? { },
          }:
          pkgs.dockerTools.buildLayeredImage {
            inherit name;
            tag = "latest";
            created = "now";
            contents = [
              pkgs.cacert
              pkgs.openssl
            ]
            ++ extraContents;
            config = {
              Cmd = [ "${pkg}/bin/${binName}" ];
            }
            // extraConfig;
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
            gofmt.enable = true;
            gotest.enable = true;
            golangci-lint.enable = true;
          };
        };

        rss-poller = callPackage ./rss_poller { };

        rss-config = callPackage ./rss_config { };

        rss-notify = callPackage ./rss_notify { };

        rustToolchain = pkgsWithRust.rust-bin.stable.latest.default.override {
          extensions = [
            "rust-src"
            "rust-analyzer"
          ];
        };

        rss-locator = pkgsWithRust.rustPlatform.buildRustPackage {
          pname = "rss-locator";
          version = "0.1.0";
          src = ./rss_locator;
          cargoLock.lockFile = ./rss_locator/Cargo.lock;
          buildType = "release";
        };

        rss-frontend = pkgs.buildNpmPackage {
          pname = "rss-frontend";
          version = "0.1.0";
          src = ./rss_frontend;
          npmDepsHash = "sha256-bNJ8ExoG2d/vuoC39UZKptrvEORaRGbpEi/rry06qv4=";
          NODE_OPTIONS = "--openssl-legacy-provider";
          buildPhase = ''
            runHook preBuild
            npm run build
            runHook postBuild
          '';
          installPhase = ''
            mkdir -p $out/dist
            cp -r dist/* $out/dist
            mkdir -p $out/node_modules
            cp -r node_modules/* $out/node_modules
          '';
        };

        rss-poller-docker = mkDockerImage {
          name = "rss_poller";
          pkg = rss-poller;
          binName = "rss-poller";
        };

        rss-config-docker = mkDockerImage {
          name = "rss_config";
          pkg = rss-config;
          binName = "rss-config";
        };

        rss-notify-docker = mkDockerImage {
          name = "rss_notify";
          pkg = rss-notify;
          binName = "rss-notify";
        };

        rss-locator-docker = pkgs.dockerTools.buildLayeredImage {
          name = "rss_locator";
          tag = "latest";
          created = "now";
          contents = [ pkgs.cacert ];
          config = {
            Cmd = [ "${rss-locator}/bin/rss_locator" ];
          };
        };

        rss-frontend-docker = pkgs.dockerTools.buildLayeredImage {
          name = "rss_frontend";
          tag = "latest";
          created = "now";
          contents = [
            pkgs.nodejs
            pkgs.cacert
            pkgs.openssl
            rss-frontend
          ];
          config = {
            Cmd = [
              "${pkgs.nodejs}/bin/node"
              "${rss-frontend}/dist/server/entry.mjs"
            ];
            Env = [ "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt" ];
          };
        };

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
              echo "  golangci-lint run           Run linter"
              echo "  gomod2nix                   Update gomod2nix.toml"
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
              echo "Build All Packages:"
              echo "  nix build .#rss-poller      Build the poller service"
              echo "  nix build .#rss-config      Build the config service"
              echo "  nix build .#rss-notify      Build the notify service"
              echo "  nix build .#rss-locator     Build the locator service (Rust)"
              echo "  nix build .#rss-frontend    Build the frontend (Node.js)"
              echo "  nix build                   Build all services"
              echo ""
              echo "Build All Docker Images:"
              echo "  nix build .#rss-poller-docker     Build poller container"
              echo "  nix build .#rss-config-docker     Build config container"
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
                if currentShell != "config" then
                  ''echo "  nix develop .#rss_config    Enter config dev environment (Go)"''
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
            rss-config
            rss-notify
            rss-locator
            rss-frontend
            ;

          # Docker images
          rss-poller-docker = rss-poller-docker;
          rss-config-docker = rss-config-docker;
          rss-notify-docker = rss-notify-docker;
          rss-locator-docker = rss-locator-docker;
          rss-frontend-docker = rss-frontend-docker;

          # Default: all packages
          default = pkgs.symlinkJoin {
            name = "all-services";
            paths = [
              rss-poller
              rss-config
              rss-notify
              rss-locator
              rss-frontend
            ];
          };
        };

        devShells = {
          # Go service shells
          rss_poller = callPackage ./rss_poller/shell.nix {
            inherit (gomod2nix.legacyPackages.${system}) mkGoEnv gomod2nix;
            shellHook = mkShellHook pre-commit-check.shellHook;
            enabledPackages = pre-commit-check.enabledPackages ++ [
              (mkDevHelp {
                serviceName = "poller";
                serviceType = "go";
              })
            ];
          };

          rss_config = callPackage ./rss_config/shell.nix {
            inherit (gomod2nix.legacyPackages.${system}) mkGoEnv gomod2nix;
            shellHook = mkShellHook pre-commit-check.shellHook;
            enabledPackages = pre-commit-check.enabledPackages ++ [
              (mkDevHelp {
                serviceName = "config";
                serviceType = "go";
              })
            ];
          };

          rss_notify = callPackage ./rss_notify/shell.nix {
            inherit (gomod2nix.legacyPackages.${system}) mkGoEnv gomod2nix;
            shellHook = mkShellHook pre-commit-check.shellHook;
            enabledPackages = pre-commit-check.enabledPackages ++ [
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

          # Root dev shell with arion and pre-commit
          default = pkgs.mkShell {
            shellHook = mkShellHook pre-commit-check.shellHook;
            buildInputs =
              with pkgs;
              [
                arion
                docker-compose
                nixfmt-rfc-style
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
          rss-config-test = mkGoTest {
            name = "rss-config";
            src = ./rss_config;
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
          rss-config-lint = mkGoLint {
            name = "rss-config";
            src = ./rss_config;
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
          build-and-load-config = mkDockerApp {
            name = "rss-config";
            imageName = "rss_config";
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
              nix build .#rss-config-docker && docker load < result && echo "Loaded rss_config:latest"
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
