# easy-qfnu-cli

曲奇教务 skill 使用的 Go CLI 私有源码仓库。

公共用户只需要安装编译后的 `easy-qfnu` 二进制，不需要访问本仓库。命令、JSON 输出和状态目录均使用 `easy-qfnu` 命名，公共文档位于 [easy-qfnu-skill](https://github.com/w1ndys/easy-qfnu-skill)。

## 本地开发

```bash
go test ./...
go run ./cmd/easy-qfnu --help
```

发布标签使用执行发布命令机器本地时区的日期时间格式 `vYYYY.MM.DD.HHmm`，例如 `v2026.08.30.1430`。同一分钟如需重新发布，覆盖同名 Release 资产即可。发布通过仓库内的 `easy-qfnu-release` skill 和本机 `gh` 完成，不依赖 GitHub Actions 或仓库 Secret。

正式版本启动时会强制读取公开 Release 的 `manifest.json`，同时检查 CLI 和 `easy-qfnu-skill/VERSION`。发现任一版本过期或清单不可用时，CLI 返回 `update_required: true` 并停止业务命令；更新 skill 后需要重新读取 `SKILL.md` 再重试。

评教提交暂时只保留预览安全门；在完成 Go 版评教协议适配前，带 `--confirm` 的命令会明确返回不可用错误，不会发送 POST。

## 发布 skill

发布 skill 位于 `.agents/skills/easy-qfnu-release/`，从本仓库根目录执行默认 dry-run：

```bash
python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py
```

确认构建结果后，再使用 `--publish` 发布；同一分钟覆盖已有版本时额外使用 `--replace`。

发布脚本会读取上一个日期版本以来 CLI 和 skill 仓库的提交，按固定章节生成面向用户的中文 Release 文案。dry-run 会打印完整文案供检查；如需人工调整，使用 `--notes-file <file>` 传入修订后的文案。Release 标题只使用版本号，不添加产品名或括号信息。
