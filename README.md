# easy-qfnu-cli

曲奇教务 skill 使用的 Go CLI 私有源码仓库。

公共用户只需要安装编译后的 `easy-qfnu` 二进制，不需要访问本仓库。命令、JSON 字段和状态目录与旧版本保持兼容，公共文档位于 [easy-qfnu-skill](https://github.com/w1ndys/easy-qfnu-skill)。

## 本地开发

```bash
go test ./...
go run ./cmd/qfnu --help
```

发布标签使用日期格式 `vYYYY.MM.DD`，例如 `v2026.08.30`。同一天如需重新发布，覆盖同名 Release 资产即可。GitHub Actions 会为 Linux、macOS 和 Windows 构建无 CGO 二进制，并将产物上传到公共 skill 仓库的 GitHub Release。发布工作流需要配置 `PUBLIC_RELEASE_TOKEN`，该 Token 只授予公共仓库的 Release 写权限。

评教提交暂时只保留预览安全门；在完成 Go 版评教协议适配前，带 `--confirm` 的命令会明确返回不可用错误，不会发送 POST。
