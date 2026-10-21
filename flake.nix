{
  description = "miao：用 Go 标准库学习终端对话和 Agent";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      # 锁定的 nixpkgs 使用 Go 1.26.8，保留标准库安全更新。
      goFor = pkgs:
        if pkgs.lib.versionAtLeast pkgs.go.version "1.26.9" then pkgs.go
        else pkgs.go.overrideAttrs {
          version = "1.26.9";
          src = pkgs.fetchurl {
            url = "https://go.dev/dl/go1.26.9.src.tar.gz";
            hash = "sha256-lzXX3Ntls10/pXfwQGRzfAO4nPGitx5uaf4vPG+f1Mo=";
          };
        };
    in
    {
      packages = forAllSystems (pkgs: {
        default = (pkgs.buildGoModule.override { go = goFor pkgs; }) {
          pname = "miao";
          version = "0.1.0-learning";
          src = self;
          # 只有标准库，无需下载或校验 vendor 依赖。
          vendorHash = null;
          subPackages = [ "cmd/miao" ];
          env.CGO_ENABLED = 0;
          # Nix 构建也验证所有包，而不只是入口包。
          checkPhase = ''
            runHook preCheck
            go test ./...
            runHook postCheck
          '';
          meta.mainProgram = "miao";
        };
      });

      checks = forAllSystems (pkgs: {
        miao = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      });

      devShells = forAllSystems (pkgs: {
        default = pkgs.mkShell {
          packages = [ (goFor pkgs) pkgs.gopls pkgs.gnumake pkgs.gcc pkgs.golangci-lint ];
        };
      });
    };
}
