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

  dockerImage = pkgs.dockerTools.buildLayeredImage {
    name = "rss_notify";
    tag = "latest";
    created = "now";
    contents = [ pkgs.cacert ];
    config.Cmd = [ "${package}/bin/rss-notify" ];
  };
in
{
  inherit package dockerImage;
}
