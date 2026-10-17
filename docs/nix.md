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

## 接进 MyFlake（需要你自己改）

> 注意：MyFlake 在 GitHub 上有 CI，`git+file://` 路径在 CI 机器上不存在，提交后 CI 会失败。
> 要提交的话先把仓库推到 GitHub（私有也行），把 url 改成 `github:<用户名>/<仓库名>`；只在本机用就别提交这处改动。

1. 在 `~/MyFlake/flake.nix` 的 `inputs` 里加（仓库没有远端，用本地路径；仓库目录仍是 `/home/huomiao/agnet`）：

   ```nix
   miao = {
     # 本地 Go agent，源码在 ~/agnet（仓库目录名没改）；更新：nix flake update miao
     url = "git+file:///home/huomiao/agnet";
     inputs.nixpkgs.follows = "nixpkgs";
   };
   ```

2. 在 `home/ai/default.nix` 的 `home.packages` 里，照 llm-agents 的写法加一行：

   ```nix
   inputs.miao.packages.${system}.miao
   ```

3. `nix flake update miao`，然后照常重建系统（`nh os switch` / `nixos-rebuild switch`）。

以后 miao 有新提交，在 MyFlake 里再跑一次 `nix flake update miao` 再重建即可。
`git+file://` 只会取已提交的内容，没提交的改动不会被打包进去。

## 更新 vendorHash

`go.mod` / `go.sum` 改了以后构建会报 hash mismatch：把 `flake.nix` 里的 `vendorHash`
改成 `pkgs.lib.fakeHash`，跑一次 `nix build .#miao`，把报错里 `got:` 后面的值填回去。

## 从 agnet 改名过来

以前接进 MyFlake 时用的是 `agnet` 这个 input 名和 `packages.<system>.agnet`，flake 里已经没有这个输出了，
要在 MyFlake 里把 input 名和包名都改成 `miao`（url 不变，仍是 `git+file:///home/huomiao/agnet`）。
