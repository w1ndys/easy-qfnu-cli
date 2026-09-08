# 多仓库开发协调

## 仓库定位

- 产品主线：`easy-qfnu-skill`
- 工程主仓库：`easy-qfnu-cli`
- 下游服务仓库：`easy-qfnu-hub`
- 独立预选课中转仓库：`easy-qfnu-precourse`

`easy-qfnu-cli` 是跨仓库工程协调入口。`easy-qfnu-skill` 定义产品行为，`easy-qfnu-precourse` 提供独立查询中转，`easy-qfnu-hub` 收集匿名使用情况；四者分别开发、分别提交。

## 各仓库职责

### easy-qfnu-skill

- 定义用户能力、Agent 流程和公开使用说明；
- 维护产品需求和公开版本文档；
- 不在公开 README、SKILL.md 或 Release 文案中暴露 CLI、Hub、内部域名、部署结构或维护流程。

### easy-qfnu-cli

- 实现本地教务登录、会话、成绩、课表等能力；
- 实现固定域名 relay 和匿名统计客户端；
- 维护客户端 API 消费契约与兼容版本；公开 Release 由 `easy-qfnu-skill` 发布；
- 负责协调 Hub 与 Skill 的工程变更。

### easy-qfnu-hub

- 实现反馈、推荐、匿名使用统计和 Dashboard 等云端服务；
- 遵循 Skill 已确定的产品行为和 CLI 已确认的客户端契约；
- 不独立改变产品语义，不把 Hub 内部凭据、日志或数据边界暴露给客户端。

### easy-qfnu-precourse

- 作为独立 Vercel 项目提供公开预选课只读中转；
- 仅在服务端持有上游 API Key，过滤查询字段，不接收教务 Cookie 或账号凭据。

## 跨仓库影响检查

- 新增、修改或删除功能时，必须检查 `easy-qfnu-skill`、`easy-qfnu-cli`、`easy-qfnu-hub` 和 `easy-qfnu-precourse` 中另外三个仓库是否需要同步修改；不能只检查当前仓库。
- 检查范围至少包括产品行为、公开说明、Agent 流程、Hub 使用统计契约、独立中转接口、CLI 调用与兼容版本、发布清单和部署配置。
- 需要联动时，按各仓库职责分别修改和验证；确认无需修改时，在交付说明中明确记录已检查的仓库及无需修改的原因。

## 变更流程

1. 在 `easy-qfnu-skill` 的需求或 issue 中确定用户可见的产品行为；
2. 在 `easy-qfnu-cli` 中拆分工程任务，冻结客户端接口、统计事件和兼容要求；
3. 在 `easy-qfnu-precourse` 中实现独立 Vercel 只读中转；
4. 在 `easy-qfnu-hub` 中实现匿名使用统计契约；
5. 在 `easy-qfnu-cli` 中接入中转并完成客户端验证；
6. 在 `easy-qfnu-skill` 中只同步经过脱敏的公开使用说明。

API 变更顺序：外部预选课接口与中转契约 → Hub 统计契约 → CLI 客户端 → Skill 公开文档。

## 提交与发布

- 四个仓库必须分别提交，不跨仓库混合 commit；
- 每个逻辑改动先完成检查，再展示完整 diff，获得明确确认后才 commit/push；
- 发布顺序：`easy-qfnu-precourse` → Hub → Skill；
- 破坏性变更必须同时记录迁移要求、最低兼容版本和回滚方式；
- 不在提交、日志、测试输出或文档中写入 Cookie、Token、密码、Webhook 或其他密钥。

## 当前实施主线

```text
easy-qfnu-precourse → Hub → CLI → Skill
```
