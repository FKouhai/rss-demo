{
  pkgs ? (
    let
      inherit (builtins) fetchTree fromJSON readFile;
      inherit ((fromJSON (readFile ../flake.lock)).nodes) nixpkgs;
    in
    import (fetchTree nixpkgs.locked) { }
  ),
  go ? pkgs.go,
  shellHook ? "",
  enabledPackages ? [ ],
  devHelp ? null,
}:
pkgs.mkShell {
  inherit shellHook;
  packages = [
    go
    # C compiler so `go test -race` works under `nix develop`.
    pkgs.gcc
  ]
  ++ pkgs.lib.optional (devHelp != null) devHelp
  ++ enabledPackages;
}
