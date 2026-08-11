{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
  };

  outputs =
    inputs@{
      nixpkgs,
      flake-parts,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = nixpkgs.lib.systems.flakeExposed;
      perSystem =
        { pkgs, ... }:
        {
          formatter = pkgs.treefmt.withConfig {
            runtimeInputs = [
              pkgs.nixfmt
              pkgs.gotools
            ];

            settings = {
              on-unmatched = "info";

              formatter.nixfmt = {
                command = "nixfmt";
                includes = [ "*.nix" ];
              };

              formatter.goimports = {
                command = "goimports";
                options = [ "-w" ];
                includes = [ "*.go" ];
              };
            };
          };

          packages.default = pkgs.buildGoModule rec {
            pname = "seabird-nwwsio-plugin";
            version = "0.3.1";

            src = ./.;

            vendorHash = "sha256-zDADdv3e5KavR4w4KJE/80K/yO/hUU7QWiv6e+d2fd0=";

            subPackages = [ "cmd/${pname}" ];

            ldflags = [
              "-s"
              "-w"
            ];
          };

          devShells.default = pkgs.mkShell {
            nativeBuildInputs = [
              pkgs.go
              pkgs.gopls
            ];
          };
        };
    };
}
