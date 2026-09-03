# easy-qfnu-cli

曲奇教务 skill 使用的 Go CLI 私有源码仓库。

公共用户只需要安装编译后的 `easy-qfnu` 二进制，不需要访问本仓库。命令、JSON 输出和状态目录均使用 `easy-qfnu` 命名，公共文档位于 [easy-qfnu-skill](https://github.com/w1ndys/easy-qfnu-skill)。

## 本地开发

```bash
go test ./...
go run ./cmd/easy-qfnu --help
```

源码构建最低使用 Go 1.26.2；发布脚本固定使用独立的 `go1.26.8` 和 `garble v0.17.0` 生成混淆二进制。

发布标签使用执行发布命令机器本地时区的日期时间格式 `vYYYY.MM.DD.HH`，例如 `v2026.08.30.14`。同一小时如需重新发布，覆盖同名 Release 资产即可。发布通过仓库内的 `easy-qfnu-release` skill 和本机 `gh` 完成，不依赖 GitHub Actions 或仓库 Secret。

正式版本启动时会强制读取公开 Release 的 `manifest.json`，以 Release 标签作为 Release、CLI 和 skill 的统一版本来源。发现 CLI 版本过期或清单不可用时，CLI 返回 `update_required: true` 并停止业务命令；更新 skill 后需要重新读取 `SKILL.md` 再重试。

评教默认只生成预览；在当前批次、课程、教师和分数经过明确确认后，追加 `--confirm` 会提交评教 POST。若响应不明确，CLI 会停止后续提交，并要求登录教务系统官方页面核对。

## 匿名使用统计

实际执行 `easy-qfnu jwxt login` 并成功后，或执行公开预选课查询后，CLI 会向 `https://hub.easy-qfnu.top/v1/telemetry/events` 上报匿名功能事件。字段仅包含事件时间、功能名、成功状态、CLI 版本、操作系统和 CPU 架构；不包含学号、姓名、查询关键词、课程数据、密码、验证码、Cookie、IP、设备 ID 或联系方式。统计服务不可用也不影响业务结果。


## 公开预选课查询

预选课目录查询不需要 JWXT 登录，CLI 通过固定的 `precourse.easy-qfnu.top` 只读服务查询排课快照：

```bash
easy-qfnu precourse search "音乐鉴赏"
easy-qfnu precourse search --teacher-name "王" --campus "日照"
easy-qfnu precourse meta
easy-qfnu precourse popular --field teacherName
```

查询至少需要一个非空条件，最多返回 500 条；结果来自定时同步快照，不等同于实时选课结果，也不会提交选课或预选课操作。
## 安全中继

需要提交反馈或使用云端服务时，CLI 只允许固定操作映射到受信任 HTTPS 服务，不接受自定义 URL、域名、路径或 HTTP 方法。请求正文从标准输入读取，Cookie 只从本地 JWXT 会话读取并通过请求头发送，不出现在命令参数、日志或响应中。

```bash
printf '%s' '{"category_id":"feature_request","text":"..."}' \
  | easy-qfnu jwxt relay feedback
printf '%s' '{"course_name":"...","teacher_name":"...","semester":"...","reason":"...","nickname":null}' \
  | easy-qfnu jwxt relay recommendation
printf '%s' '{"scope":"both","course_codes":["CS101"]}' \
  | easy-qfnu jwxt relay rank
```

## 发布 skill

发布 skill 位于 `.agents/skills/easy-qfnu-release/`，从本仓库根目录执行默认 dry-run：

```bash
python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py
```

确认构建结果后，再使用 `--publish` 发布；同一小时覆盖已有版本时额外使用 `--replace`。

发布脚本会读取上一个日期版本以来 CLI 和 skill 仓库的提交，按固定章节生成面向用户的中文 Release 文案。dry-run 会打印完整文案供检查；如需人工调整，使用 `--notes-file <file>` 传入修订后的文案。Release 标题只使用版本号，不添加产品名或括号信息。
