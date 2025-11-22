{
  description = "A basic gomod2nix flake";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      gomod2nix,
    }:
    (flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};

        # The current default sdk for macOS fails to compile go projects, so we use a newer one for now.
        # This has no effect on other platforms.
        callPackage = pkgs.darwin.apple_sdk_11_0.callPackage or pkgs.callPackage;
        # Simple test check added to nix flake check
        go-test = pkgs.stdenvNoCC.mkDerivation {
          name = "go-test";
          dontBuild = true;
          src = ./.;
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
        # Simple lint check added to nix flake check
        go-lint = pkgs.stdenvNoCC.mkDerivation {
          name = "go-lint";
          dontBuild = true;
          src = ./.;
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
        # variable for rss-notify package
        rss-notify = callPackage ./. {
          inherit (gomod2nix.legacyPackages.${system}) ;
        };

        dockerImage = pkgs.dockerTools.buildLayeredImage {
          name = "rss_notify";
          tag = "latest";
          created = "now";
          contents = [
            pkgs.cacert
            pkgs.openssl
          ];
          config = {
            Cmd = [ "${rss-notify}/bin/rss-notify" ];
          };
        };
      in
      {
        inherit dockerImage rss-notify;
        checks = {
          inherit go-test go-lint;
        };

        packages.default = rss-notify;
        devShells.default = callPackage ./shell.nix {
          inherit (gomod2nix.legacyPackages.${system}) mkGoEnv gomod2nix;
        };

        # Custom shell command to build and load Docker image
        apps.build-and-load-docker = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "build-and-load-docker" ''
            nix build .#dockerImage.${system}
            docker load < result
            echo "Docker image loaded as rss_notify:latest"
          ''}/bin/build-and-load-docker";
        };
      }
    ));
}
