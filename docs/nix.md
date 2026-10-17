# 用 Nix 安装 miao

仓库根目录的 `flake.nix` 提供：

| 输出 | 用途 |
| --- | --- |
| `packages.<system>.miao`（也是 `default`） | 可执行文件 `miao`（静态编译，`CGO_ENABLED=0`） |
| `overlays.default` | 往 pkgs 里加 `pkgs.miao` |
| `devShells.<system>.default` | go、gopls、make、gcc（`make check` 的 race 测试要用 gcc） |

## 本地试用

```sh
nix build .#miao && ./result/bin/miao        # 构建
nix run .                                     # 直接运行
nix develop                                   # 开发环境，然后 make check
```

flake 只看得到 git 已跟踪的文件，新加的文件要先 `git add`。

## 接进自己的 NixOS / home-manager flake

1. 在 `flake.nix` 的 `inputs` 里加：

   ```nix
   miao = {
     url = "github:ainnhuomiao/miao";
     inputs.nixpkgs.follows = "nixpkgs";
   };
   ```

2. 在 `home.packages`（或 `environment.systemPackages`）里加：

   ```nix
   inputs.miao.packages.${system}.miao
   ```

   也可以用 overlay：`nixpkgs.overlays = [ inputs.miao.overlays.default ];` 然后直接写 `pkgs.miao`。

3. `nix flake update miao`，然后照常重建系统。以后要更新，再跑一次 `nix flake update miao`。

## 更新 vendorHash

`go.mod` / `go.sum` 改了以后构建会报 hash mismatch：把 `flake.nix` 里的 `vendorHash`
改成 `pkgs.lib.fakeHash`，跑一次 `nix build .#miao`，把报错里 `got:` 后面的值填回去。
