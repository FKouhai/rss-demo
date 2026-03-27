{
  pkgs,
  workspaceGo,
  golangci-lint,
  git-hooks,
  system,
  govendor,
  linuxPkgs,
  root,
}:
let
  mkGoTest =
    { name, src }:
    pkgs.stdenvNoCC.mkDerivation {
      name = "${name}-test";
      inherit src;
      dontBuild = true;
      __noChroot = true;
      doCheck = true;
      nativeBuildInputs = [
        workspaceGo
        pkgs.writableTmpDirAsHomeHook
      ];
      checkPhase = "go test -v ./...";
      installPhase = "mkdir \"$out\"";
    };

  mkGoLint =
    { name, src }:
    pkgs.stdenvNoCC.mkDerivation {
      name = "${name}-lint";
      inherit src;
      dontBuild = true;
      doCheck = true;
      nativeBuildInputs = [
        golangci-lint
        workspaceGo
        pkgs.writableTmpDirAsHomeHook
      ];
      checkPhase = "golangci-lint run";
      installPhase = "mkdir \"$out\"";
    };

  pre-commit-check = git-hooks.lib.${system}.run {
    src = root;
    hooks = {
      nixfmt-rfc-style.enable = true;
      commitizen.enable = true;
      convco.enable = true;
      # Use go-overlay-provided packages for consistency with the build toolchain.
      gofmt = {
        enable = true;
        package = workspaceGo;
      };
      gotest = {
        enable = true;
        package = workspaceGo;
      };
      golangci-lint = {
        enable = true;
        package = golangci-lint;
      };
    };
  };

  mkShellHook = preCommitHook: ''
    ${preCommitHook}
    echo ""
    echo "Type 'devhelp' for available commands"
  '';

  # Infer service type from directory contents.
  inferServiceType =
    dir:
    if builtins.pathExists (root + "/${dir}/go.mod") then
      "go"
    else if builtins.pathExists (root + "/${dir}/Cargo.toml") then
      "rust"
    else if builtins.pathExists (root + "/${dir}/package.json") then
      "nodejs"
    else
      "unknown";

in
{
  inherit
    mkGoTest
    mkGoLint
    mkGoVulnCheck
    mkShellHook
    inferServiceType
    pre-commit-check
    ;
}
