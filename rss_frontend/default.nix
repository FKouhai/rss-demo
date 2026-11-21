{
  pkgs ? (
    let
      inherit (builtins) fetchTree fromJSON readFile;
      inherit ((fromJSON (readFile ../flake.lock)).nodes) nixpkgs;
    in
    import (fetchTree nixpkgs.locked) { }
  ),
  buildGoWorkspace ? pkgs.buildGoWorkspace,
  go ? pkgs.go,
  tailwindcss ? pkgs.tailwindcss,
  nix2containerPkg ? null,
}:
let
  package = buildGoWorkspace (
    {
      pname = "rss-frontend";
      version = "0.1.0";
      src = ../.;
      modules = ../govendor.toml;
      inherit go;
      subPackages = [ "rss_frontend" ];
      doCheck = false;
      # Compile the newspaper stylesheet before go:embed picks it up.
      preBuild = ''
        cd rss_frontend
        ${tailwindcss}/bin/tailwindcss -i static/input.css -o static/app.css --minify
        cd ..
      '';
    }
    // pkgs.lib.optionalAttrs pkgs.stdenv.isDarwin {
      GOOS = "linux";
      GOARCH = "arm64";
      CGO_ENABLED = "0";
    }
  );

  dockerImage = nix2containerPkg.buildImage {
    name = "rss_frontend";
    tag = "latest";
    config = {
      cmd = [ "${package}/bin/rss-frontend" ];
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
