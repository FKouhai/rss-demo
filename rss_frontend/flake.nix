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
          pname = "rss_frontend";
          version = "0.1.0";
          src = ./.;
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
        });
        dockerImage = pkgs.dockerTools.buildLayeredImage {
          name = "rss_frontend";
          tag = "latest";
          created = "now";
          contents = [
            pkgs.nodejs
            pkgs.cacert
            pkgs.openssl
            frontend
          ];
          config = {
            Cmd = [
              "${pkgs.nodejs}/bin/node"
              "${frontend}/dist/server/entry.mjs"
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
