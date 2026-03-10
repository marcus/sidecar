{
  description = "Nix flake for sidecar";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f system);
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          version = if self ? shortRev then self.shortRev else "dev";
          tdVersion = "0.33.0";
        in
        {
          td = pkgs.buildGoModule {
            pname = "td";
            version = tdVersion;
            src = pkgs.fetchFromGitHub {
              owner = "marcus";
              repo = "td";
              rev = "v${tdVersion}";
              sha256 = "sha256-gci4B83x/0UMawy+fncArF9zO1aHRE/zXj91e5h6yi8=";
            };
            vendorHash = "sha256-Rp0lhnBLJx+exX7VLql3RfthTVk3LLftD6n6SsSWzVY=";
            subPackages = [ "." ];
          };
          sidecar = pkgs.buildGoModule {
            pname = "sidecar";
            inherit version;
            src = ./.;
            subPackages = [ "cmd/sidecar" ];
            vendorHash = "sha256-R/AjNJ4x4t1zXXzT+21cjY+9pxs4DVXU4xs88BQvHx4=";
            ldflags = [
              "-s"
              "-w"
              "-X"
              "main.Version=${version}"
            ];
          };
          default = self.packages.${system}.sidecar;
        }
      );

      apps = forAllSystems (system: {
        sidecar = {
          type = "app";
          program = "${self.packages.${system}.sidecar}/bin/sidecar";
        };
        default = self.apps.${system}.sidecar;
      });

      defaultPackage = forAllSystems (system: self.packages.${system}.sidecar);
    };
}
