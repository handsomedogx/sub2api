# 分组 System 提示词

管理员在「分组管理」的新建或编辑窗口中配置「System 提示词策略」。配置作用于请求所属分组，账号切换不改变策略。普通用户的分组接口不返回提示词内容。

| 模式 | 行为 |
| --- | --- |
| 原样透传 | 保留客户端指令；保存的网关提示词不生效。现有分组默认使用此模式。 |
| 注入 | 将网关提示词放在客户端指令前，保留客户端指令。 |
| 替换 | 使用网关提示词替换客户端指令；Chat Completions 和 Responses 中的 system/developer 消息也会移除。 |

注入和替换模式要求填写非空文本，最多 64 KiB，按 UTF-8 字节计算。切回透传后仍保留已填写文本。复制分组会复制这两个配置项，简易模式也支持该功能。

支持 Anthropic Messages、OpenAI Chat Completions、OpenAI Responses 和 Gemini generateContent/streamGenerateContent；Anthropic count_tokens 与 Responses input_tokens 请求使用相同策略。Responses WebSocket 的连接池与透传模式会对每轮请求应用策略。Responses 压缩请求沿用原有处理，不应用该策略。

这是客户端指令层的策略。Claude OAuth System Blocks、Antigravity 身份补丁等兼容性处理仍可能在后续阶段添加内部提示词。Claude Code 的 billing attribution block 会被保留。若额外启用了 Codex 强制 instructions 模板，最终 instructions 仍受该模板控制；模板需要包含 `{{ .ExistingInstructions }}` 才会保留前面的指令。

管理员 API 的创建/更新分组请求支持：

```json
{
  "system_prompt_mode": "inject",
  "system_prompt": "请使用中文回答。"
}
```

`system_prompt_mode` 可取 `passthrough`、`inject`、`replace`。更新时省略字段会保留原值。

部署新版后，启动迁移 `295_group_system_prompt.sql` 会为分组添加这两个字段，并将已有分组设为透传。认证缓存版本同步更新，旧快照会重新加载；后续保存分组会失效该分组的认证缓存。
