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
  vendorHash = "sha256-iFhAJbCpJmf/SUepsFNkns9Mo+n01wT/7LLgPmw00HM=";
  modules = ./gomod2nix.toml;
})
