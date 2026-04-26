{
  description = "Fast, smart tmux state persistence — replaces resurrect/continuum.";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
    git-hooks-nix.url = "github:cachix/git-hooks.nix";
    git-hooks-nix.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs = inputs @ {flake-parts, ...}:
    flake-parts.lib.mkFlake {inherit inputs;} {
      imports = [inputs.git-hooks-nix.flakeModule];

      systems = ["x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin"];

      perSystem = {
        config,
        pkgs,
        lib,
        self',
        ...
      }: {
        pre-commit.settings.hooks = {
          gofmt.enable = true;
          govet.enable = true;
          golangci-lint.enable = true;
          typos.enable = true;
          check-merge-conflicts.enable = true;
          trim-trailing-whitespace.enable = true;
        };

        devShells.default = pkgs.mkShell {
          inherit (config.pre-commit) shellHook;
          packages = config.pre-commit.settings.enabledPackages ++ [
            pkgs.go
            pkgs.gopls
            pkgs.gotools
            pkgs.golangci-lint
            pkgs.tmux
            pkgs.fzf
            pkgs.sqlite
          ];
        };

        packages = {
          default = pkgs.buildGoModule {
            pname = "tmux-state";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-t7QGKxs+KqdOhZN2wDKKbsaIytPg+G7kiJ1z52w4onQ=";
            subPackages = ["cmd/tmux-state"];
            doCheck = true;
            meta = {
              description = "Fast, smart tmux state persistence";
              mainProgram = "tmux-state";
              license = lib.licenses.mit;
            };
          };
        };

        apps.default = {
          type = "app";
          program = "${self'.packages.default}/bin/tmux-state";
        };
      };
    };
}
