{ pkgs, packages }:
let
  dockerApps = pkgs.lib.filterAttrs (n: _: pkgs.lib.hasSuffix "-docker" n) packages;

  mkDockerApp =
    name: _:
    let
      imageName = pkgs.lib.removeSuffix "-docker" name;
    in
    {
      type = "app";
      program = "${pkgs.writeShellScriptBin "build-and-load-${name}" ''
        nix build .#${name}
        docker load < result
        echo "Loaded ${imageName}:latest"
      ''}/bin/build-and-load-${name}";
    };

  loadAllCmds = pkgs.lib.concatMapStringsSep "\n" (
    name:
    let
      imageName = pkgs.lib.removeSuffix "-docker" name;
    in
    "nix build .#${name} && docker load < result && echo \"Loaded ${imageName}:latest\""
  ) (builtins.attrNames dockerApps);
in
pkgs.lib.mapAttrs mkDockerApp dockerApps
// {
  build-and-load-all = {
    type = "app";
    program = "${pkgs.writeShellScriptBin "build-and-load-all" ''
      echo "Building and loading all Docker images..."
      ${loadAllCmds}
      echo "All Docker images loaded successfully!"
    ''}/bin/build-and-load-all";
  };
}
