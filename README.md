# easy-qfnu-cli

> **已归档。** 本仓库不再开发。公开 CLI 已迁到 [easy-qfnu-skill](https://github.com/w1ndys/easy-qfnu-skill) 的标准库 Python 实现。

曲奇教务 skill 使用的 Go CLI 私有源码仓库。

公共用户使用 [easy-qfnu-skill](https://github.com/w1ndys/easy-qfnu-skill) 中的标准库 Python CLI，不需要访问本仓库。命令、JSON 输出和状态目录均使用 `easy-qfnu` 命名。

## 本地开发

```bash
go test ./...
go run ./cmd/easy-qfnu --help
```

公开日期版本由 [easy-qfnu-skill](https://github.com/w1ndys/easy-qfnu-skill) 仓库内的 `easy-qfnu-release` skill 发布，不再从本仓库构建或上传平台二进制。

评教默认只生成预览；在当前批次、课程、教师和分数经过明确确认后，追加 `--confirm` 会提交评教 POST。若响应不明确，CLI 会停止后续提交，并要求登录教务系统官方页面核对。

## 匿名使用统计

在线查询与提交功能执行后，CLI 会向 `https://hub.easy-qfnu.top/v1/telemetry/events` 上报匿名功能事件，覆盖：`jwc` 通知列表/搜索/正文、新生题库搜索、公开预选课查询、公开推荐查询、`jwxt` 登录成功、成绩、课表、评教列表与确认提交、选课轮次即时查询、`relay` 反馈/推荐/排名查询。事件字段仅包含功能名、成功状态、事件时间、CLI 版本、操作系统和 CPU 架构；不包含学号、姓名、查询关键词、课程数据、密码、验证码、Cookie、IP、设备 ID 或联系方式。登录失败与纯本地操作（验证码、status、logout）不上报；统计服务不可用也不影响业务结果。


## 公开预选课查询

预选课目录查询不需要 JWXT 登录，CLI 通过固定的 `precourse.easy-qfnu.top` 只读服务查询排课快照：

```bash
easy-qfnu precourse search "音乐鉴赏"
easy-qfnu precourse search --teacher-name "王" --campus "日照"
easy-qfnu precourse meta
easy-qfnu precourse popular --field teacherName
```

查询至少需要一个非空条件，最多返回 500 条；结果来自定时同步快照，不等同于实时选课结果，也不会提交选课或预选课操作。

选课轮次开放时，可用已登录会话做即时查询（比上面的缓存更准确，并能探测课程所在模块）：

```bash
easy-qfnu jwxt xk rounds
easy-qfnu jwxt xk search --course "音乐鉴赏"
easy-qfnu jwxt xk search --teacher "王" --module 公选课
```

`search` 默认扫描全部选课模块，`located_modules` 表示目标课程实际出现的模块；网页前端可能按年级隐藏这些入口。该命令只读，不会提交选课。无开放轮次时请改用 `precourse search`。

## 公开推荐查询

选课推荐查询不需要 JWXT 登录，CLI 通过固定的 `recommend.easy-qfnu.top` 只读服务查询已公开的课程-教师评价：

```bash
easy-qfnu recommendation search --course "高等数学"
easy-qfnu recommendation search --teacher "张" --top 20
easy-qfnu recommendation search --course "高等数学" --teacher "张"
```

至少需要非空的 `--course` 或 `--teacher`；`top` 默认 20、最大 100。同一课程、教师和学年可以有多条评价，结果不含评分。查询不会提交推荐，也不会读取教务 Cookie。提交推荐仍走已有的 `jwxt relay recommendation`。

## 安全中继

需要提交反馈或使用云端服务时，CLI 只允许固定操作映射到受信任 HTTPS 服务，不接受自定义 URL、域名、路径或 HTTP 方法。请求正文从标准输入读取，Cookie 只从本地 JWXT 会话读取并通过请求头发送，不出现在命令参数、日志或响应中。

```bash
printf '%s' '{"category_id":"feature_request","text":"..."}' \
  | easy-qfnu jwxt relay feedback
printf '%s' '{"course_name":"...","teacher_name":"...","year":"...","reason":"...","nickname":null}' \
  | easy-qfnu jwxt relay recommendation
printf '%s' '{"scope":"both","course_codes":["CS101"]}' \
  | easy-qfnu jwxt relay rank
```

## 发布

公开 Release 已迁到 skill 仓库。在 `easy-qfnu-skill` 根目录执行：

```bash
python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py
python3 .agents/skills/easy-qfnu-release/scripts/publish_release.py --publish
```
