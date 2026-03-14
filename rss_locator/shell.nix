{
  pkgs ? import <nixpkgs> { },
}:

let
  rustToolchain = pkgs.rust-bin.stable.latest.default.override {
    extensions = [
      "rust-src"
      "rust-analyzer"
    ];
  };
in
pkgs.mkShell {
  buildInputs = with pkgs; [
    rustToolchain
    cargo
    cargo-edit
    cargo-outdated
    cargo-audit
    trivy
    dive
  ];
  RUST_BACKTRACE = "1";
}
