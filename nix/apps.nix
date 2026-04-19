{ pkgs, packages }:
let
  dockerApps = pkgs.lib.filterAttrs (n: _: pkgs.lib.hasSuffix "-docker" n) packages;

  mkDockerApp =
    name: image:
    let
      imageName = pkgs.lib.removeSuffix "-docker" name;
    in
    {
      type = "app";
      program = "${pkgs.writeShellScriptBin "build-and-load-${name}" ''
        ${image.copyToDockerDaemon}/bin/copy-to-docker-daemon
        echo "Loaded ${imageName}:latest"
      ''}/bin/build-and-load-${name}";
    };

  loadAllCmds = pkgs.lib.concatMapStringsSep "\n" (
    name:
    let
      image = dockerApps.${name};
      imageName = pkgs.lib.removeSuffix "-docker" name;
    in
    "${image.copyToDockerDaemon}/bin/copy-to-docker-daemon && echo \"Loaded ${imageName}:latest\""
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
