{ pkgs, system }:
{ serviceName, serviceType }:
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
    echo "  nix build                   Build all services"
    echo ""
    echo "Build & Load All Docker Images:"
    echo "  nix run .#build-and-load-all      Build & load ALL images"
    echo ""
  '';

  serviceNixCommands =
    let
      nixName = builtins.replaceStrings [ "_" ] [ "-" ] serviceName;
    in
    ''
      echo "Nix Build Commands:"
      echo "  nix build .#rss-${nixName}           Build the ${serviceName} service"
      echo "  nix build .#rss-${nixName}-docker    Build ${serviceName} container image"
      echo "  nix run .#build-and-load-rss-${nixName}-docker  Build & load docker image"
      echo ""
    '';

  goChecksCommands =
    let
      nixName = builtins.replaceStrings [ "_" ] [ "-" ] serviceName;
    in
    ''
      echo "Nix Checks (Tests & Linting):"
      echo "  nix build .#checks.${system}.rss-${nixName}-test   Run ${serviceName} tests"
      echo "  nix build .#checks.${system}.rss-${nixName}-lint   Run ${serviceName} linter"
      echo "  nix flake check             Run all checks"
      echo ""
    '';

  switchShellsCommands =
    let
      shellName = if serviceType == "root" then "root" else serviceName;
    in
    ''
      echo "Switch Development Shells:"
      ${
        if shellName != "poller" then
          ''echo "  nix develop .#rss_poller    Enter poller dev environment (Go)"''
        else
          ""
      }
      ${
        if shellName != "notify" then
          ''echo "  nix develop .#rss_notify    Enter notify dev environment (Go)"''
        else
          ""
      }
      ${
        if shellName != "outbox" then
          ''echo "  nix develop .#rss_outbox    Enter outbox dev environment (Go)"''
        else
          ""
      }
      ${
        if shellName != "frontend" then
          ''echo "  nix develop .#rss_frontend  Enter frontend dev environment (Node.js)"''
        else
          ""
      }
      ${
        if shellName != "root" then
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
in
pkgs.writeShellScriptBin "devhelp" ''
  ${header}
  ${
    if serviceType == "go" then
      goCommands
    else if serviceType == "nodejs" then
      nodejsCommands
    else if serviceType == "root" then
      rootCommands
    else
      ""
  }
  ${if serviceType != "root" then serviceNixCommands else ""}
  ${
    if serviceType == "go" then
      goChecksCommands
    else if serviceType == "root" then
      ''
        echo "Checks:"
        echo "  nix flake check             Run all checks"
        echo ""
      ''
    else
      ""
  }
  ${switchShellsCommands}
  ${preCommitCommands}
''
