{
  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  outputs =
    { self, nixpkgs, ... }:
    let
      sys = builtins.currentSystem;
    in
    {
      pkgs = nixpkgs.legacyPackages.${sys};
    };
}
