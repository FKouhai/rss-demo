{
  pkgs ? import <nixpkgs> { },
  shellHook ? "",
  enabledPackages ? [ ],
  devHelp ? null,
}:
let
  install_astro = pkgs.writeShellApplication {
    name = "install_astro";
    runtimeInputs = [ pkgs.nodejs ];
    text = ''
      npm create astro@latest
    '';
  };

  run_dev = pkgs.writeShellApplication {
    name = "run_dev";
    runtimeInputs = [ pkgs.nodejs ];
    text = ''
      npm run dev
    '';
  };
in
pkgs.mkShell {
  inherit shellHook;
  buildInputs = [
    install_astro
    pkgs.nodejs
    run_dev
  ]
  ++ pkgs.lib.optional (devHelp != null) devHelp
  ++ enabledPackages;
}
