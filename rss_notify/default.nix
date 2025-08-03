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
  vendorHash = "sha256-mG2LXno7xG95ks13ta5lb1W4Qfe1V5hA4s959hrRWB4=";
  modules = ./gomod2nix.toml;
})
