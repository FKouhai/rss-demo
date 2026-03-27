{
  pkgs ? import <nixpkgs> { },
  pkgsWithRust ? pkgs,
  shellHook ? "",
  enabledPackages ? [ ],
  devHelp ? null,
}:
let
  rustToolchain = pkgsWithRust.rust-bin.stable.latest.default.override {
    extensions = [
      "rust-src"
      "rust-analyzer"
    ];
  };
in
pkgsWithRust.mkShell {
  inherit shellHook;
  buildInputs =
    (with pkgsWithRust; [
      rustToolchain
      cargo
      cargo-edit
      cargo-outdated
      cargo-audit
      trivy
      dive
    ])
    ++ pkgs.lib.optional (devHelp != null) devHelp
    ++ enabledPackages;
  RUST_BACKTRACE = "1";
}
