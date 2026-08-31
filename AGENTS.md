# 多仓库开发协调

## 仓库定位

- 产品主线：`easy-qfnu-skill`
- 工程主仓库：`easy-qfnu-cli`
- 下游服务仓库：`easy-qfnu-hub`

`easy-qfnu-cli` 是跨仓库工程协调入口。`easy-qfnu-skill` 定义产品行为，`easy-qfnu-hub` 实现云端服务，三者分别开发、分别提交。

## 各仓库职责

### easy-qfnu-skill

- 定义用户能力、Agent 流程和公开使用说明；
- 维护产品需求和公开版本文档；
- 不在公开 README、SKILL.md 或 Release 文案中暴露 CLI、Hub、内部域名、部署结构或维护流程。

### easy-qfnu-cli

- 实现本地教务登录、会话、成绩、课表等能力；
- 实现固定域名 relay 和匿名统计客户端；
- 维护客户端 API 消费契约、兼容版本和 Go 二进制发布；
- 负责协调 Hub 与 Skill 的工程变更。

### easy-qfnu-hub

- 实现云端 API、教务 Cookie 鉴权、KV、飞书转发和 Dashboard；
- 遵循 Skill 已确定的产品行为和 CLI 已确认的客户端契约；
- 不独立改变产品语义，不把 Hub 内部凭据、日志或数据边界暴露给客户端。

## 变更流程

1. 在 `easy-qfnu-skill` 的需求或 issue 中确定产品行为；
2. 在 `easy-qfnu-cli` 中拆分工程任务，冻结跨仓库接口和兼容要求；
3. 在 `easy-qfnu-hub` 中实现服务端；
4. 在 `easy-qfnu-cli` 中接入并验证客户端行为；
5. 在 `easy-qfnu-skill` 中只同步经过脱敏的公开使用说明。

API 变更顺序：Hub 契约 → CLI 客户端 → Skill 公开文档。

## 提交与发布

- 三个仓库必须分别提交，不跨仓库混合 commit；
- 每个逻辑改动先完成检查，再展示 diff，获得明确确认后才 commit/push；
- 发布顺序：Hub → CLI → Skill；
- 破坏性变更必须同时记录迁移要求、最低兼容版本和回滚方式；
- 不在提交、日志、测试输出或文档中写入 Cookie、Token、密码、Webhook 或其他密钥。

## 当前实施主线

```text
Hub API 与部署稳定
→ CLI relay 接入
→ Skill 用户流程和公开说明
```
