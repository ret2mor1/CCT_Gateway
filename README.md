# CCT (Claude-to-OpenAI Protocol Translator)

CCT 是一个轻量级、高性能的 Go 语言代理服务器，专门用于将 **Anthropic Messages API** 协议转换为 **OpenAI Chat Completion API** 协议。

### 为什么需要 CCT？

目前市面上大多数代理工具（如 LiteLLM）侧重于将 OpenAI 请求转换为 Claude，但反向转换的支持往往不够完善。特别是对于 **Claude Code** 这种深度依赖：
- **Tool Use (工具调用)**
- **复杂 SSE 流 (Streaming Events)**
- **多层级 System Prompt**

的项目，CCT 提供了针对性的优化，使得你可以通过 NVIDIA NIM、Groq、DeepSeek 等 OpenAI 兼容后端完美运行 Claude Code。

## 主要特性

- [x] **协议双向转换**：完美支持 Anthropic 与 OpenAI 格式互转。
- [x] **工具调用 (Tool Use)**：支持 `write_file`, `bash`, `grep` 等工具指令的翻译。
- [x] **系统提示词优化**：支持 Claude 特有的多区块系统提示词（Content Block Array）。
- [x] **灵活的模型映射**：支持自定义客户端模型名与后端模型名的映射关系。
- [x] **高性能**：基于 Go 原生开发，内存占用极低。

## 快速开始

### 1. 安装

确保你已安装 Go 1.21+，然后克隆并编译：

```bash
git clone https://github.com/your-username/cct.git
cd cct
go build -o cct
```

### 2. 配置

编辑 `config.yaml`：

```yaml
server:
  host: 127.0.0.1
  port: 4000
  api_keys:
    - "cct-secret-key-123" # 自定义key

providers:
  nvidia_nim:
    protocol: openai-chat
    base_url: https://xxxxx.com/v1
    api_key: ${your_API_KEY}
    limits:
      rpm: 40 # rpm限速配置，确保不会因为限速导致会话中断
    transport:
      timeout: 120
      retries: 3
      verify_ssl: true

  xiaomimimo:
    protocol: openai-chat
    base_url: https://xxxx.xiaomimimo.com/v1
    api_key: ${your_API_KEY}

models:
  mistral:
    routes:
      - provider: nvidia_nim # provider配置
        model: mistralai/mistral-medium-3.5-128b # 后端模型名称
        weight: 100 # 后端模型权重，可以实现负载均衡

  mimo-v2.5:
    routes:
      - provider: xiaomimimo
        model: mimo-v2.5
        weight: 100

protocols:
  # 备注：此处配置决定了鉴权 Key 的提取位置。
  anthropic-messages:
    enabled: true
    auth:
      key_name: Authorization
      location: header
      prefix: Bearer
  openai-chat:
    enabled: true
    auth:
      key_name: Authorization
      location: header
      prefix: Bearer
```

### 3. 运行

```bash
./cct -config config.yaml
```

### 4. 在 Claude Code 中使用

Claude Code 默认连接到 Anthropic 官方 API，你可以通过设置 `ANTHROPIC_BASE_URL` 重定向其请求到 CCT。

#### 配置 (推荐)

如果你希望每次启动 Claude 都不需要手动设置，可以修改 Claude Code 的全局配置文件。

配置文件路径通常位于：
*   **macOS/Linux**: `~/.claude/settings.json`
*   **Windows**: `%USERPROFILE%\.claude\settings.json` (通常为 `C:\Users\你的用户名\.claude\settings.json`)

如果文件或目录不存在，请手动创建。

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://localhost:4000/v1",
    "ANTHROPIC_API_KEY": "cct-secret-key-123"
  }
}
```

#### 验证配置
启动 Claude Code 后，输入以下命令确认配置已生效：

1. 输入 `/config` 查看当前的 API Base URL。
2. 尝试运行一个简单的命令，如 `hi`，观察 CCT 控制台是否有代理请求输出。

> [!TIP]
> **工具调用 (Tool Use)**：如果使用第三方后端导致 MCP 工具搜索失效，可以尝试设置环境变量 `ENABLE_TOOL_SEARCH=true`。

## 贡献

欢迎提交 Issue 和 Pull Request 来帮助我们改进 CCT！

## 开源协议

MIT License
