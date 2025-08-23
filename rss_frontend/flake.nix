{
  description = "Flake for frontend microservice";

  inputs = {
    flake-utils.url = "github:numtide/flake-utils";
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs {
          inherit system;
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
        frontend = pkgs.buildNpmPackage (finalAttrs: {
          pname = "frontend";
          version = "0.1.0";
          src = ./.;
          npmDepsHash = "sha256-QxGqtIcanIFKJmn5ZJXCl7q8AGbObKtTqgZS7BzthJc=";
          NODE_OPTIONS = "--openssl-legacy-provider";
        });
        dockerImage = pkgs.dockerTools.buildLayeredImage {
          name = "frontend";
          tag = "latest";
          created = "now";
          contents = [
            pkgs.nodejs
          ];
          config = {
            Cmd = [
              "${pkgs.nodejs}/bin/node"
              "${frontend}/lib/node_modules/rss_frontend/dist/server/entry.mjs"
            ];
          };
        };
      in
      rec {
        inherit frontend dockerImage;
        defaultPackage = frontend;
        flakedPkgs = pkgs;

        # enables use of `nix shell`
        devShell = pkgs.mkShell {
          # add things you want in your shell here
          buildInputs = with pkgs; [
            install_astro
            nodejs
            run_dev
          ];
        };
      }
    );
}
