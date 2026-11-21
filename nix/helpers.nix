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
  # Workspace-aware src shared by the Go checks: tests and lint must run
  # with go.work visible, otherwise workspace-internal imports (libs/*)
  # cannot resolve in the sandbox. Large local-only dirs are excluded.
  checkSrc = pkgs.lib.cleanSourceWith {
    src = root;
    filter =
      path: _type:
      !(pkgs.lib.elem (baseNameOf path) [
        ".git"
        "result"
        ".direnv"
        "dist"
        ".astro"
      ]);
  };

  mkGoTest =
    { name, dir }:
    pkgs.stdenv.mkDerivation {
      name = "${name}-test";
      src = checkSrc;
      dontBuild = true;
      __noChroot = true;
      doCheck = true;
      nativeBuildInputs = [
        workspaceGo
        pkgs.writableTmpDirAsHomeHook
      ];
      # -race needs cgo + a C compiler; stdenv provides cc.
      checkPhase = "go test -race -v ./${dir}/...";
      installPhase = "mkdir \"$out\"";
    };

  mkGoLint =
    { name, dir }:
    pkgs.stdenvNoCC.mkDerivation {
      name = "${name}-lint";
      src = checkSrc;
      dontBuild = true;
      __noChroot = true;
      doCheck = true;
      nativeBuildInputs = [
        golangci-lint
        workspaceGo
        pkgs.writableTmpDirAsHomeHook
      ];
      checkPhase = "golangci-lint run ./${dir}/...";
      installPhase = "mkdir \"$out\"";
    };

  pre-commit-check = git-hooks.lib.${system}.run {
    src = root;
    hooks = {
      nixfmt = {
        enable = true;
        # Vendored upstream file keeps its own formatting (see header).
        excludes = [ "nix/go-overlay/workspace\\.nix" ];
      };
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
        # The linter shells out to `go` (gci formatter, package loading);
        # without the toolchain on PATH the hook fails in the sandbox.
        extraPackages = [ workspaceGo ];
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
    mkShellHook
    inferServiceType
    pre-commit-check
    ;
}
