{
  pkgs ? import <nixpkgs> { },
  buildNpmPackage ? pkgs.buildNpmPackage,
  # On Darwin, pass pkgs.pkgsCross.aarch64-multiplatform so the container gets
  # a linux/aarch64 node binary. On Linux this defaults to pkgs (native).
  linuxPkgs ? pkgs,
}:
let
  package = buildNpmPackage {
    pname = "rss-frontend";
    version = "0.1.0";
    src = ./.;
    npmDepsHash = "sha256-bNJ8ExoG2d/vuoC39UZKptrvEORaRGbpEi/rry06qv4=";
    NODE_OPTIONS = "--openssl-legacy-provider";
    # LOCATOR_URL is a runtime concern — blank it out at build time so any
    # .env file cannot bake a localhost URL into the build output.
    LOCATOR_URL = "";
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

  dockerImage = pkgs.dockerTools.buildLayeredImage {
    name = "rss_frontend";
    tag = "latest";
    created = "now";
    contents = [
      linuxPkgs.nodejs
      linuxPkgs.cacert
      linuxPkgs.openssl
      package
    ];
    config = {
      Cmd = [
        "${linuxPkgs.nodejs}/bin/node"
        "${package}/dist/server/entry.mjs"
      ];
      Env = [ "SSL_CERT_FILE=${linuxPkgs.cacert}/etc/ssl/certs/ca-bundle.crt" ];
    };
  };
in
{
  inherit package dockerImage;
}
