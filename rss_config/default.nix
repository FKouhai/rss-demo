{
  pkgs ? (
    let
      inherit (builtins) fetchTree fromJSON readFile;
      inherit ((fromJSON (readFile ../flake.lock)).nodes) nixpkgs gomod2nix;
    in
    import (fetchTree nixpkgs.locked) {
      overlays = [
        (import "${fetchTree gomod2nix.locked}/overlay.nix")
      ];
    }
  ),
  buildGoModule ? pkgs.buildGoModule,
}:
let
  package = buildGoModule (
    finalAttrs:
    {
      pname = "rss-config";
      version = "0.1.0";
      pwd = ./.;
      src = ./.;
      modules = ./gomod2nix.toml;
      vendorHash = "sha256-mfcpdDFvuyFDx1M3Zcfvg8T3KlO2W/8b2LKUGpevU4A=";
      doCheck = false;
    }
    // pkgs.lib.optionalAttrs pkgs.stdenv.isDarwin {
      GOOS = "linux";
      GOARCH = "arm64";
      CGO_ENABLED = "0";
      dontFixup = true;
    }
  );

  dockerImage = pkgs.dockerTools.buildLayeredImage {
    name = "rss_config";
    tag = "latest";
    created = "now";
    contents = [ pkgs.cacert ];
    config.Cmd = [ "${package}/bin/rss-config" ];
  };
in
{
  inherit package dockerImage;
}
