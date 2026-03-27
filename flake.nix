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
        # Single package set with both overlays applied.
        pkgs = import nixpkgs {
          inherit system;
          overlays = [
            (import rust-overlay)
            go-overlay.overlays.default
          ];
        };

        # Workspace-pinned Go 1.26.0 — all tools derived from this version.
        workspaceGo = pkgs.go-bin.versions."1.26.0";
        golangci-lint = workspaceGo.tools.golangci-lint.latest;
        govendor = go-overlay.packages.${system}.govendor;

        # On Darwin, pull linux/aarch64 container runtime deps from the cache.
        linuxPkgs = if pkgs.stdenv.isDarwin then pkgs.pkgsCross.aarch64-multiplatform else pkgs;

        helpers = import ./nix/helpers.nix {
          inherit
            pkgs
            workspaceGo
            golangci-lint
            git-hooks
            system
            govendor
            linuxPkgs
            ;
          root = ./.;
        };

        inherit (helpers)
          mkGoTest
          mkGoLint
          mkShellHook
          inferServiceType
          pre-commit-check
          ;

        mkDevHelp = import ./nix/devhelp.nix { inherit pkgs system; };

        # Extend the combined package set with cross-cutting service args so
        # callPackage injects only what each default.nix/shell.nix declares.
        servicePkgs = pkgs.extend (
          _: _: {
            inherit linuxPkgs workspaceGo govendor;
            go = workspaceGo;
            pkgsWithRust = pkgs; # rust-overlay is already in pkgs
            rustPlatform = pkgs.rustPlatform;
          }
        );

        shellPkgs = servicePkgs.extend (
          _: _: {
            shellHook = mkShellHook pre-commit-check.shellHook;
            enabledPackages = pre-commit-check.enabledPackages ++ [ govendor ];
            devHelp = null; # overridden per-service below
          }
        );

        subdirs = builtins.attrNames (pkgs.lib.filterAttrs (_: t: t == "directory") (builtins.readDir ./.));

        # All subdirs with default.nix → package + docker image.
        serviceNames = builtins.filter (n: builtins.pathExists (./. + "/${n}/default.nix")) subdirs;

        serviceOutputs = builtins.listToAttrs (
          map (n: {
            name = n;
            value = servicePkgs.callPackage (./. + "/${n}") { };
          }) serviceNames
        );

        # { rss-poller, rss-poller-docker, rss-notify, rss-notify-docker, ... }
        servicePackages = builtins.foldl' (
          acc: n:
          let
            nixName = builtins.replaceStrings [ "_" ] [ "-" ] n;
            out = serviceOutputs.${n};
          in
          acc
          // {
            "${nixName}" = out.package;
          }
          // pkgs.lib.optionalAttrs (out ? dockerImage) { "${nixName}-docker" = out.dockerImage; }
        ) { } serviceNames;

        # All subdirs with shell.nix → devShell.
        shellNames = builtins.filter (n: builtins.pathExists (./. + "/${n}/shell.nix")) subdirs;

        serviceShells = builtins.listToAttrs (
          map (n: {
            name = n;
            value = shellPkgs.callPackage (./. + "/${n}/shell.nix") {
              devHelp = mkDevHelp {
                serviceName = builtins.replaceStrings [ "rss_" ] [ "" ] n;
                serviceType = inferServiceType n;
              };
            };
          }) shellNames
        );

        # Go services get test + lint checks auto-generated.
        goServiceNames = builtins.filter (n: builtins.pathExists (./. + "/${n}/go.mod")) serviceNames;

        goChecks = builtins.listToAttrs (
          pkgs.lib.concatMap (
            n:
            let
              nixName = builtins.replaceStrings [ "_" ] [ "-" ] n;
            in
            [
              {
                name = "${nixName}-test";
                value = mkGoTest {
                  name = nixName;
                  src = ./. + "/${n}";
                };
              }
              {
                name = "${nixName}-lint";
                value = mkGoLint {
                  name = nixName;
                  src = ./. + "/${n}";
                };
              }
            ]
          ) goServiceNames
        );

      in
      {
        packages = servicePackages // {
          default = pkgs.symlinkJoin {
            name = "all-services";
            paths = map (n: serviceOutputs.${n}.package) serviceNames;
          };
        };

        devShells = serviceShells // {
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
        }
        // goChecks;

        apps = import ./nix/apps.nix {
          inherit pkgs;
          packages = servicePackages;
        };
      }
    );
}
