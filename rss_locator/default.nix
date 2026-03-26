{
  pkgs ? import <nixpkgs> { },
  pkgsWithRust ? pkgs,
  rustPlatform ? pkgsWithRust.rustPlatform,
}:
let
  isDarwin = pkgs.stdenv.isDarwin;

  # On Darwin: extend the toolchain with the aarch64-linux target for cross-compilation.
  # Nix is lazy so this is only evaluated on Darwin.
  crossRustToolchain = pkgsWithRust.rust-bin.stable.latest.default.override {
    extensions = [
      "rust-src"
      "rust-analyzer"
    ];
    targets = [ "aarch64-unknown-linux-gnu" ];
  };

  crossRustPlatform = pkgsWithRust.makeRustPlatform {
    cargo = crossRustToolchain;
    rustc = crossRustToolchain;
  };

  activePlatform = if isDarwin then crossRustPlatform else rustPlatform;

  package = activePlatform.buildRustPackage (
    {
      pname = "rss-locator";
      version = "0.1.0";
      src = ./.;
      cargoLock.lockFile = ./Cargo.lock;
      buildType = "release";
    }
    // pkgs.lib.optionalAttrs isDarwin {
      # cargo-zigbuild uses Zig as a drop-in cross-linker — no pkgsCross needed.
      nativeBuildInputs = [
        pkgs.cargo-zigbuild
        pkgs.zig
      ];
      # Prevent cargoBuildHook / cargoInstallHook setup hooks from overriding
      # our explicit buildPhase / installPhase below.
      dontCargoBuild = true;
      dontCargoInstall = true;
      buildPhase = ''
        runHook preBuild
        cargo zigbuild --target aarch64-unknown-linux-gnu --release
        runHook postBuild
      '';
      installPhase = ''
        runHook preInstall
        mkdir -p $out/bin
        cp target/aarch64-unknown-linux-gnu/release/rss_locator $out/bin/
        runHook postInstall
      '';
      dontFixup = true;
    }
  );

  dockerImage = pkgs.dockerTools.buildLayeredImage {
    name = "rss_locator";
    tag = "latest";
    created = "now";
    contents = [ pkgs.cacert ];
    config.Cmd = [ "${package}/bin/rss_locator" ];
  };
in
{
  inherit package dockerImage;
}
