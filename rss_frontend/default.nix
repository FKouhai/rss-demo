{
  pkgs ? import <nixpkgs> { },
  buildNpmPackage ? pkgs.buildNpmPackage,
  # On Darwin, pass pkgs.pkgsCross.aarch64-multiplatform so the container gets
  # a linux/aarch64 node binary. On Linux this defaults to pkgs (native).
  linuxPkgs ? pkgs,
  nix2containerPkg ? null,
}:
let
  package = buildNpmPackage {
    pname = "rss-frontend";
    version = "0.1.0";
    src = ./.;
    npmDepsHash = "sha256-IdNaXcw0FCBdIMELDt/iHf1chzGWAD7OY0LKcjxy2Ks=";
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

  dockerImage = nix2containerPkg.buildImage {
    name = "rss_frontend";
    tag = "latest";
    config = {
      cmd = [
        "${linuxPkgs.nodejs}/bin/node"
        "${package}/dist/server/entry.mjs"
      ];
      Env = [ "SSL_CERT_FILE=${linuxPkgs.cacert}/etc/ssl/certs/ca-bundle.crt" ];
    };
    layers = [
      (nix2containerPkg.buildLayer {
        deps = [
          linuxPkgs.nodejs
          linuxPkgs.cacert
          linuxPkgs.openssl
        ];
      })
      (nix2containerPkg.buildLayer { deps = [ package ]; })
    ];
  };
in
{
  inherit package dockerImage;
}
