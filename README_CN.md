# CLIProxyAPI Plus

[English](README.md) | 中文

这是 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 的 Plus 版本，在原有基础上增加了第三方供应商的支持。

现已支持通过 OAuth 登录接入 OpenAI Codex（GPT 系列）和 Claude Code。

您可以使用本地或多账户的CLI方式，通过任何与 OpenAI（包括Responses）/Gemini/Claude 兼容的客户端和SDK进行访问。

## 赞助商

[![bigmodel.cn](https://assets.router-for.me/chinese-5-0.jpg)](https://www.bigmodel.cn/claude-code?ic=RRVJPB5SII)

本项目由 Z智谱 提供赞助, 他们通过 GLM CODING PLAN 对本项目提供技术支持。

GLM CODING PLAN 是专为AI编码打造的订阅套餐，每月最低仅需20元，即可在十余款主流AI编码工具如 Claude Code、Cline、Roo Code 中畅享智谱旗舰模型GLM-4.7（受限于算力，目前仅限Pro用户开放），为开发者提供顶尖的编码体验。

智谱AI为本产品提供了特别优惠，使用以下链接购买可以享受九折优惠：https://www.bigmodel.cn/claude-code?ic=RRVJPB5SII

---

<table>
<tbody>
<tr>
<td width="180"><a href="https://www.packyapi.com/register?aff=cliproxyapi"><img src="./assets/packycode.png" alt="PackyCode" width="150"></a></td>
<td>感谢 PackyCode 对本项目的赞助！PackyCode 是一家可靠高效的 API 中转服务商，提供 Claude Code、Codex、Gemini 等多种服务的中转。PackyCode 为本软件用户提供了特别优惠：使用<a href="https://www.packyapi.com/register?aff=cliproxyapi" target="_blank">此链接</a>注册，并在充值时输入 "cliproxyapi" 优惠码即可享受九折优惠。</td>
</tr>
<tr>
<td width="180"><a href="https://www.aicodemirror.com/register?invitecode=TJNAIF"><img src="./assets/aicodemirror.png" alt="AICodeMirror" width="150"></a></td>
<td>感谢 AICodeMirror 赞助了本项目！AICodeMirror 提供 Claude Code / Codex / Gemini CLI 官方高稳定中转服务，支持企业级高并发、极速开票、7×24 专属技术支持。 Claude Code / Codex / Gemini 官方渠道低至 3.8 / 0.2 / 0.9 折，充值更有折上折！AICodeMirror 为 CLIProxyAPI 的用户提供了特别福利，通过<a href="https://www.aicodemirror.com/register?invitecode=TJNAIF" target="_blank">此链接</a>注册的用户，可享受首充8折，企业客户最高可享 7.5 折！</td>
</tr>
<tr>
<td width="180"><a href="https://shop.bmoplus.com/?utm_source=github"><img src="./assets/bmoplus.png" alt="BmoPlus" width="150"></a></td>
<td>感谢 BmoPlus 赞助了本项目！BmoPlus 是一家专为AI订阅重度用户打造的可靠 AI 账号代充服务商，提供稳定的 ChatGPT Plus / ChatGPT Pro(全程质保) / Claude Pro / Super Grok / Gemini Pro 的官方代充&成品账号。 通过<a href="https://shop.bmoplus.com/?utm_source=github" target="_blank">BmoPlus AI成品号专卖/代充</a>注册下单的用户，可享GPT <b>官网订阅一折</b> 的震撼价格！</td>
</tr>
<tr>
<td width="180"><a href="https://www.lingtrue.com/register"><img src="./assets/lingtrue.png" alt="LingtrueAPI" width="150"></a></td>
<td>感谢 LingtrueAPI 对本项目的赞助！LingtrueAPI 是一家全球大模型API中转服务平台，提供Claude Code、Codex、Gemini 等多种顶级模型API调用服务，致力于让用户以低成本、高稳定性链接全球AI能力。LingtrueAPI为本软件用户提供了特别优惠：使用<a href="https://www.lingtrue.com/register" target="_blank">此链接</a>注册，并在首次充值时输入 "LingtrueAPI" 优惠码即可享受9折优惠。</td>
</tr>
<tr>
<td width="180"><a href="https://poixe.com/i/m8kvep"><img src="./assets/poixeai.png" alt="PoixeAI" width="150"></a></td>
<td>感谢 Poixe AI 对本项目的赞助！Poixe AI 提供可靠的 AI 模型接口服务，您可以使用平台提供的 LLM API 接口轻松构建 AI 产品，同时也可以成为供应商，为平台提供大模型资源以赚取收益。通过 CLIProxyAPI <a href="https://poixe.com/i/m8kvep" target="_blank">专属链接</a>注册，充值额外赠送 $5 美金</td>
</tr>
<tr>
<td width="180"><a href="https://coder.visioncoder.cn"><img src="./assets/visioncoder.png" alt="VisionCoder" width="150"></a></td>
<td>感谢 VisionCoder 对本项目的支持。<a href="https://coder.visioncoder.cn" target="_blank">VisionCoder 开发平台</a> 是一个可靠高效的 API 中继服务提供商，提供 Claude Code、Codex、Gemini 等主流 AI 模型，帮助开发者和团队更轻松地集成 AI 功能，提升工作效率。
<p></p>
VisionCoder 还为我们的用户提供 <a href="https://coder.visioncoder.cn" target="_blank">Token Plan</a> 限时活动：购买 1 个月，赠送 1 个月。</td>
</tr>
</tbody>
</table>


## 功能特性

- 为 CLI 模型提供 OpenAI/Gemini/Claude/Codex 兼容的 API 端点
- 新增 OpenAI Codex（GPT 系列）支持（OAuth 登录）
- 新增 Claude Code 支持（OAuth 登录）
- 支持流式与非流式响应
- 函数调用/工具支持
- 多模态输入（文本、图片）
- 多账户支持与轮询负载均衡（Gemini、OpenAI、Claude）
- 简单的 CLI 身份验证流程（Gemini、OpenAI、Claude）
- 支持 Gemini AIStudio API 密钥
- 支持 AI Studio Build 多账户轮询
- 支持 Gemini CLI 多账户轮询
- 支持 Claude Code 多账户轮询
- 支持 OpenAI Codex 多账户轮询
- 通过配置接入上游 OpenAI 兼容提供商（例如 OpenRouter）
- 可复用的 Go SDK（见 `docs/sdk-usage_CN.md`）

## 新手入门

CLIProxyAPI 用户手册： [https://help.router-for.me/](https://help.router-for.me/cn/)

## 管理 API 文档

请参见 [MANAGEMENT_API_CN.md](https://help.router-for.me/cn/management/api)

## Amp CLI 支持

CLIProxyAPI 已内置对 [Amp CLI](https://ampcode.com) 和 Amp IDE 扩展的支持，可让你使用自己的 Google/ChatGPT/Claude OAuth 订阅来配合 Amp 编码工具：

- 提供商路由别名，兼容 Amp 的 API 路径模式（`/api/provider/{provider}/v1...`）
- 管理代理，处理 OAuth 认证和账号功能
- 智能模型回退与自动路由
- 以安全为先的设计，管理端点仅限 localhost

当你需要某一类后端的请求/响应协议形态时，优先使用 provider-specific 路径，而不是合并后的 `/v1/...` 端点：

- 对于 messages 风格的后端，使用 `/api/provider/{provider}/v1/messages`。
- 对于按模型路径暴露生成接口的后端，使用 `/api/provider/{provider}/v1beta/models/...`。
- 对于 chat-completions 风格的后端，使用 `/api/provider/{provider}/v1/chat/completions`。

这些路径有助于选择协议表面，但当多个后端复用同一个客户端可见模型名时，它们本身并不能保证唯一的推理执行器。实际的推理路由仍然根据请求里的 model/alias 解析。若要严格钉住某个后端，请使用唯一 alias、前缀，或避免让多个后端暴露相同的客户端模型名。

**→ [Amp CLI 完整集成指南](https://help.router-for.me/cn/agent-client/amp-cli.html)**

## SDK 文档

- 使用文档：[docs/sdk-usage_CN.md](docs/sdk-usage_CN.md)
- 高级（执行器与翻译器）：[docs/sdk-advanced_CN.md](docs/sdk-advanced_CN.md)
- 认证: [docs/sdk-access_CN.md](docs/sdk-access_CN.md)
- 凭据加载/更新: [docs/sdk-watcher_CN.md](docs/sdk-watcher_CN.md)
- 自定义 Provider 示例：`examples/custom-provider`

## 贡献

该项目仅接受第三方供应商支持的 Pull Request。任何非第三方供应商支持的 Pull Request 都将被拒绝。

如果需要提交任何非第三方供应商支持的 Pull Request，请提交到[主线](https://github.com/router-for-me/CLIProxyAPI)版本。

## 许可证

此项目根据 MIT 许可证授权 - 有关详细信息，请参阅 [LICENSE](LICENSE) 文件。
