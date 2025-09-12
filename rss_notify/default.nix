{
  pkgs ? (
    let
      inherit (builtins) fetchTree fromJSON readFile;
      inherit ((fromJSON (readFile ./flake.lock)).nodes) nixpkgs gomod2nix;
    in
    import (fetchTree nixpkgs.locked) {
      overlays = [
        (import "${fetchTree gomod2nix.locked}/overlay.nix")
      ];
    }
  ),
  buildGoModule ? pkgs.buildGoModule,
}:

buildGoModule (finalAttrs: {
  pname = "rss-notify";
  version = "0.1";
  pwd = ./.;
  src = ./.;
  vendorHash = "sha256-mfcpdDFvuyFDx1M3Zcfvg8T3KlO2W/8b2LKUGpevU4A=";
  modules = ./gomod2nix.toml;
})
