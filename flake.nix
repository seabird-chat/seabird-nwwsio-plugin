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
            version = "0.3.2-dev";

            src = ./.;

            vendorHash = "sha256-8+VUR5gYGSom4tV0RkD5LEF+X1Ds4BQZH7+u/1ygPGk=";

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
