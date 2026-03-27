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
    pkgs.trivy
    pkgs.dive
  ]
  ++ pkgs.lib.optional (devHelp != null) devHelp
  ++ enabledPackages;
}
