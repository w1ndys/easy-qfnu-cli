---
name: easy-qfnu-release
description: Build and publish easy-qfnu date-tagged release binaries and fixed-format user-facing release notes from a local Go CLI repository with the authenticated GitHub CLI, without GitHub Actions.
---

# easy-qfnu 发布

使用此 skill 将当前 CLI 仓库构建为日期版本并发布到公共 GitHub Release。它适用于用户明确要求发布新版本、重新发布当天版本或检查发布产物；不用于普通 Go 开发或 GitHub Actions 配置。

## 约定

- 版本标签使用 `vYYYY.MM.DD.HH`，默认采用执行发布命令机器的本地时间，例如 `v2026.08.30.14`。
- 此 skill 随 CLI 源码仓库维护，源码仓库默认由 skill 脚本所在位置自动定位，可用 `EASY_QFNU_CLI_REPO` 或 `--repo` 覆盖。
- 目标 Release 仓库默认为 `w1ndys/easy-qfnu-skill`，可用 `EASY_QFNU_PUBLIC_REPO` 或 `--public-repo` 覆盖。
- 产物为 `easy-qfnu-linux-amd64`、`easy-qfnu-linux-arm64`、`easy-qfnu-darwin-amd64`、`easy-qfnu-darwin-arm64`、`easy-qfnu-windows-amd64.exe`、`checksums.txt` 和 `manifest.json`。
- `manifest.json` 同时记录 `release_version`、`cli_version`、`skill_version` 及每个平台产物的 SHA-256；三者均由 Release 标签自动生成，Release 标签是唯一版本来源，CLI 只依据 Release 清单检查自身版本。
- Release 标题固定为版本号本身，例如 `v2026.08.30.17`，不添加产品名或括号中的版本信息。
- Release 正文固定包含“发布说明、功能更新、修复问题、改进与维护、安装、版本信息”六个章节。脚本会读取上一个公开 Release 到当前 HEAD 的提交，并按 Conventional Commit 类型生成中文用户更新点；发布流程、CI 和测试提交会过滤掉，避免把内部实现细节展示给用户。

## 构建前置条件

- 发布构建固定使用 garble `v0.17.1-0.20260828155325-29c42928bf8b`（master 构建，支持 go1.27.1；官方 v0.17.0 尚无 go1.27 链接器补丁）和独立的 `go1.27.1` 工具链。
- Go 自动下载到 `GOMODCACHE` 的工具链不能用于 garble 的链接器补丁；如果默认命令未找到独立工具链，可设置 `EASY_QFNU_GO` 指向独立的 `go` 可执行文件。
- 发布脚本把 garble 安装到构建缓存目录，不写入用户的 `GOPATH/bin`；这只影响构建工具，不改变最终 CLI 的安装位置。
- 构建产物缓存在 `~/.cache/easy-qfnu-release/<版本>/`（可用 `EASY_QFNU_CACHE_DIR` 覆盖），缓存键为版本 + 源码提交 + 工具链 + garble 版本；dry-run 构建一次后，确认发布时直接复用，不重复交叉编译。源码提交、版本或工具链变化时自动重新构建。
- 这是提高逆向成本的混淆，不是加密；密钥和必须保密的服务端逻辑仍必须留在服务端。
- `-tiny` 会减少 panic 和崩溃堆栈信息；问题排查使用未混淆的开发构建。

## 发布流程

1. 在 CLI 仓库根目录先执行默认 dry-run。脚本会检查本地仓库是否干净、验证 `gh` 登录、运行 `go test ./...`，并使用 garble 混淆交叉构建五个平台，产物写入构建缓存目录；此阶段不创建标签、不推送代码、不上传 Release。

   ```bash
   python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py
   ```

2. 检查 dry-run 输出的上一个 Release、变更范围和完整 Release 文案。确认功能更新点确实面向用户、没有泄露内部信息；必要时将人工修订后的固定格式文案写入文件，通过 `--notes-file` 传入。获得本次发布的明确确认后，才加 `--publish`；该命令直接复用 dry-run 缓存的产物，只执行打标签与上传，不再交叉编译：

   ```bash
   python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py --publish
   ```

   自定义文案示例：

   ```bash
   python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py \
     --notes-file /path/to/release-notes.md --publish
   ```

3. 同一小时已有版本时，默认停止并要求判断。只有用户明确要求覆盖当前 Release 时才使用 `--replace`。该选项会强制更新 CLI 源码仓库标签，并把公开 skill 仓库的同名标签指到当前 skill HEAD，然后删除并重建 GitHub Release（源码包与发布时间跟随 skill 提交），再上传资产：

   ```bash
   python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py --publish --replace
   ```

4. 只重建公共 Release、但不改动 CLI 源码仓库标签时，使用 `--public-only`。这仍会同步公开 skill 仓库标签并重建 GitHub Release，适用于修复已有 Release 的资产、版本清单或过期的 skill 源码包：

   ```bash
   python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py \
     --version v2026.08.30.16 --publish --replace --public-only
   ```

脚本使用本机 `gh` 的登录身份完成 GitHub 操作，不读取或打印 Token，也不依赖 GitHub Actions。每次全新构建会在构建缓存目录安装固定版本的 garble 并生成混淆二进制，产物按版本缓存复用；发布完成后会再次读取 Release 资产，确认五个平台文件、`checksums.txt` 和 `manifest.json` 都存在，并确认标题与正文已写入。

不要在没有用户明确确认的情况下运行 `--publish` 或 `--replace`。如果源码仓库有未提交修改、版本格式不合法、测试失败、交叉编译失败或 GitHub 权限不足，应停止并报告具体错误。重新发布历史版本时，必须明确指定对应的 `--version`；如果只操作公开仓库，必须同时使用 `--public-only`，避免移动源码仓库的历史标签。
