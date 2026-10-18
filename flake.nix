{
  description = "miao：终端里的 AI 编程助手（Go + Bubble Tea）";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs =
    { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f: nixpkgs.lib.genAttrs systems (system: f nixpkgs.legacyPackages.${system});
      version = "0.1.0-${self.shortRev or self.dirtyShortRev or "dev"}";
    in
    {
      packages = forAllSystems (pkgs: {
        miao = pkgs.buildGoModule {
          pname = "miao";
          inherit version;
          src = self;
          # go.mod / go.sum 一变就要更新：先改成 pkgs.lib.fakeHash，nix build 报出的 got 填回来
          vendorHash = "sha256-2d30SCGEYiSHdQpYBq37RGFBPQUu/jUkpEOWe3qDGNU=";
          subPackages = [ "cmd/miao" ];
          env.CGO_ENABLED = 0;
          ldflags = [
            "-s"
            "-w"
          ];
          # 只构建 cmd/miao（产物就是 bin/miao），这里不跑测试；完整测试用 make check
          meta = {
            description = "终端里的 AI 编程助手";
            mainProgram = "miao";
            platforms = systems;
          };
        };
        default = self.packages.${pkgs.stdenv.hostPlatform.system}.miao;
      });

      overlays.default = final: _prev: {
        miao = self.packages.${final.stdenv.hostPlatform.system}.miao;
      };

      devShells = forAllSystems (pkgs: {
        # gcc 给 go test -race 用
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gnumake
            gcc
            golangci-lint
            goreleaser
          ];
        };
      });

      formatter = forAllSystems (pkgs: pkgs.nixfmt-rfc-style);
    };
}
