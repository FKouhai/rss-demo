{
  description = "RSS Locator service - Rust";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    rust-overlay = {
      url = "github:oxalica/rust-overlay";
      inputs = {
        nixpkgs.follows = "nixpkgs";
      };
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
      rust-overlay,
    }:
    (flake-utils.lib.eachDefaultSystem (
      system:
      let
        overlays = [ (import rust-overlay) ];
        pkgs = import nixpkgs {
          inherit system overlays;
        };
        inherit (pkgs) lib;

        rustToolchain = pkgs.rust-bin.stable.latest.default.override {
          extensions = [
            "rust-src"
            "rust-analyzer"
          ];
        };

        # Rust package
        rss-locator = pkgs.rustPlatform.buildRustPackage {
          pname = "rss-locator";
          version = "0.1.0";
          src = ./.;
          cargoLock.lockFile = ./Cargo.lock;
          buildType = "release";
        };

        # build for container image
        dockerImage = pkgs.dockerTools.buildLayeredImage {
          name = "rss_locator";
          tag = "latest";
          created = "now";
          contents = [
            pkgs.cacert
          ];
          config = {
            Cmd = [ "${rss-locator}/bin/rss_locator" ];
          };
        };
      in
      {
        inherit dockerImage rss-locator;
        packages.default = rss-locator;
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            rustToolchain
            cargo
            cargo-edit
            cargo-outdated
            cargo-audit
            trivy
            dive
          ];
          RUST_BACKTRACE = "1";
        };

        # Custom shell command to build and load Docker image
        apps.build-and-load-docker = {
          type = "app";
          program = "${pkgs.writeShellScriptBin "build-and-load-docker" ''
            nix build .#dockerImage.${system}
            docker load < result
            echo "Docker image loaded as rss_locator:latest"
          ''}/bin/build-and-load-docker";
        };
      }
    ));
}
