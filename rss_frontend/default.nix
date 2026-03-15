{
  pkgs ? import <nixpkgs> { },
  buildNpmPackage ? pkgs.buildNpmPackage,
}:

buildNpmPackage {
  pname = "rss-frontend";
  version = "0.1.0";
  src = ./.;
  npmDepsHash = "sha256-bNJ8ExoG2d/vuoC39UZKptrvEORaRGbpEi/rry06qv4=";
  NODE_OPTIONS = "--openssl-legacy-provider";
  buildPhase = ''
    runHook preBuild
    npm run build
    runHook postBuild
  '';
  installPhase = ''
    mkdir -p $out/dist
    cp -r dist/* $out/dist
    mkdir -p $out/node_modules
    cp -r node_modules/* $out/node_modules
  '';
}
