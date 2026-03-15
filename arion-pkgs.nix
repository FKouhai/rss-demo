let
  lock = builtins.fromJSON (builtins.readFile ./flake.lock);
  nixpkgs = lock.nodes.nixpkgs.locked;
  pkgs = import (builtins.fetchTarball {
    url = "https://github.com/NixOS/nixpkgs/archive/${nixpkgs.rev}.tar.gz";
    sha256 = nixpkgs.narHash;
  }) { };
in
pkgs
