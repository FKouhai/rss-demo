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
  pname = "rss-fetcher";
  version = "0.1.0";
  pwd = ./.;
  src = ./.;
  modules = ./gomod2nix.toml;
  vendorHash = "sha256-Owkdnxoju195OwPXs1bc6eyUvRoIioRP3Dp/veVMH/8=";
  doCheck = false;
})
