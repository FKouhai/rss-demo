{
  pkgs ? (
    let
      inherit (builtins) fetchTree fromJSON readFile;
      inherit ((fromJSON (readFile ../flake.lock)).nodes) nixpkgs;
    in
    import (fetchTree nixpkgs.locked) { }
  ),
  # go-overlay injects buildGoWorkspace into pkgs when using pkgsWithGo.callPackage
  buildGoWorkspace ? pkgs.buildGoWorkspace,
  go ? pkgs.go,
  nix2containerPkg ? null,
}:
let
  package = buildGoWorkspace (
    {
      pname = "rss-notify";
      version = "0.1.0";
      src = ../.;
      modules = ../govendor.toml;
      inherit go;
      subPackages = [ "rss_notify" ];
      doCheck = false;
    }
    // pkgs.lib.optionalAttrs pkgs.stdenv.isDarwin {
      GOOS = "linux";
      GOARCH = "arm64";
      CGO_ENABLED = "0";
    }
  );

  dockerImage = nix2containerPkg.buildImage {
    name = "rss_notify";
    tag = "latest";
    config = {
      cmd = [ "${package}/bin/rss-notify" ];
      Env = [ "SSL_CERT_FILE=${pkgs.cacert}/etc/ssl/certs/ca-bundle.crt" ];
    };
    layers = [
      (nix2containerPkg.buildLayer { deps = [ pkgs.cacert ]; })
      (nix2containerPkg.buildLayer { deps = [ package ]; })
    ];
  };
in
{
  inherit package dockerImage;
}
