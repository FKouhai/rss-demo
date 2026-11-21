# Local fixup overlay for go-overlay, applied AFTER go-overlay's own
# overlay (see `overlays = [...]` in flake.nix).
#
# It rebuilds ONLY buildGoWorkspace/buildGoVendoredWorkspace with our
# patched ./workspace.nix (same directory). Everything else (govendor,
# go toolchain, buildGoApplication, …) falls through to `prev` untouched.
#
# WHY: upstream writes `=> <path>` trailers for workspace members into
# vendor/modules.txt unconditionally, which Go >= 1.22 rejects
# ("is marked as replaced in vendor/modules.txt, but not replaced in
# the workspace"). The patch emits plain `# mod version` headers for
# workspace members — byte-identical to what `go work vendor` produces —
# and matches member paths after normalizing both sides.
# Upstream issue: https://github.com/purpleclay/go-overlay/issues/719
# DELETE THIS FILE (and the `overlays` entry) once upstream fixes it.
{ goOverlaySrc }:
final: prev:
let
  upstream = file: import (goOverlaySrc + "/builder/${file}.nix");

  fetchGoModule = upstream "fetch-module" {
    inherit (final)
      lib
      stdenvNoCC
      cacert
      git
      jq
      ;
  };

  modulesTxtModule = upstream "modules-txt" { inherit (final) lib; };
  inherit (modulesTxtModule)
    mkAnnotation
    mkRemoteModuleEntry
    mkRemoteReplaceTrailers
    isUnusedReplace
    ;

  vendorEnvModule = upstream "vendor-env" {
    inherit (final) lib runCommand;
    inherit
      fetchGoModule
      mkAnnotation
      mkRemoteModuleEntry
      mkRemoteReplaceTrailers
      isUnusedReplace
      ;
  };
  inherit (vendorEnvModule) mkVendorEnv mkModuleCopyCommands;

  hostToolModule = upstream "host-tool" { inherit (final) lib stdenv runCommand; };
  inherit (hostToolModule) mkHostTool parseGoWorkModules;

  commonModule = upstream "common" { inherit (final) lib; };
  inherit (commonModule) commonRemovedAttrs mkCommonAttrs;

  inherit (upstream "test-packages" { inherit (final) lib; }) mkTestPackages;

  applicationModule = upstream "application" {
    inherit (final) lib stdenv;
    inherit
      mkVendorEnv
      mkHostTool
      commonRemovedAttrs
      mkCommonAttrs
      mkTestPackages
      ;
  };

  workspaceModule = import ./workspace.nix {
    inherit (final) lib stdenv runCommand;
    inherit
      fetchGoModule
      mkModuleCopyCommands
      mkHostTool
      parseGoWorkModules
      commonRemovedAttrs
      mkCommonAttrs
      mkTestPackages
      mkRemoteModuleEntry
      mkRemoteReplaceTrailers
      isUnusedReplace
      ;
  };
in
{
  inherit (applicationModule) buildGoApplication buildGoVendoredApplication;
  inherit (workspaceModule) buildGoWorkspace buildGoVendoredWorkspace;
}
