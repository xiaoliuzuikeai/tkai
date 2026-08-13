# GoAI Backend

GoAI 是一个使用 Go 开发的 AI 对话与个人知识库后端。项目围绕大模型应用中常见的会话管理、流式输出、长上下文、知识库检索、工具调用及服务稳定性问题进行实现，而不是只对模型 API 做简单转发。

项目提供普通对话、SSE 流式对话、RAG 文档问答、MCP 工具调用、用户认证、会话记录、语音合成和图像识别等接口。

## 技术栈

| 分类 | 使用技术 |
| --- | --- |
| Web 框架 | Go 1.24、Gin |
| 数据访问 | GORM、MySQL |
| 缓存与检索 | Redis、RediSearch、Redis Vector |
| AI 编排 | CloudWeGo Eino |
| 模型接入 | OpenAI Compatible API、DashScope、Ollama |
| 流式通信 | Server-Sent Events（SSE） |
| 工具协议 | MCP Streamable HTTP |
| 认证与安全 | JWT、bcrypt、Gin Middleware |
| 并发控制 | `sync.RWMutex`、`singleflight`、`context` |
| 可选组件 | Kafka、SMTP、ONNX Runtime、TTS 服务 |

## 整体设计

项目采用分层架构，将 HTTP 协议、业务规则、数据访问和基础设施分开：

```text
HTTP Request
    │
    ▼
Router ── 路由注册与中间件装配
    │
    ▼
Controller ── 参数解析、校验和响应封装
    │
    ▼
Service ── 会话、用户、知识库等业务逻辑
    │
    ├── DAO ── MySQL 数据访问
    └── Common ── AI、RAG、Redis、MCP、TTS 等基础能力
```

这种设计让 Controller 不直接操作数据库或模型客户端，业务逻辑也不依赖具体的 HTTP 返回格式，便于单元测试和后续替换基础组件。

## 功能设计与实现

### 1. 用户认证与接口安全

用户模块包含邮箱验证码、注册、登录和 JWT 鉴权。

**设计理念**

- 认证采用无状态设计，服务端不保存登录 Session，便于后续横向扩容。
- 密码只保存哈希值，不保存或返回明文密码。
- 用户身份由 JWT 中间件统一解析，再通过 Gin Context 传递给后续业务层。
- 登录、注册和验证码接口采用独立限流窗口，避免某一类请求影响其他接口。
- 错误信息不区分“用户不存在”和“密码错误”，减少账号探测风险。

**使用技术**

- `golang-jwt/jwt` 生成和解析 JWT。
- `bcrypt` 进行密码哈希与校验，并兼容旧 MD5 密码的登录后升级。
- Redis 保存邮箱验证码并设置过期时间，验证码验证成功后立即删除。
- Gin Middleware 实现 Bearer Token 鉴权。
- 基于 IP 的固定时间窗口限流，并返回 `Retry-After` 响应头。

### 2. 分层 API 与统一响应

后端接口按用户、AI、文件和图像模块划分，统一挂载在 `/api/v1` 下。

**设计理念**

- Router 只负责路径和中间件，Controller 负责协议转换，Service 负责业务规则，DAO 负责数据读写。
- 使用统一业务状态码和响应结构，避免每个接口自行定义错误格式。
- 通过版本化路径保留后续接口升级空间。
- 参数校验尽量在进入业务逻辑前完成，减少无效数据库和模型请求。

**使用技术**

- Gin 路由分组和中间件机制。
- GORM 封装 MySQL 查询、事务和自动迁移。
- Controller、Service、DAO 分层组织代码。

### 3. 多模型统一接入

AI 模块当前包含普通模型、RAG 模型、MCP 模型和 Ollama 本地模型四种实现。

**设计理念**

- 使用接口隔离模型差异，上层会话服务只依赖 `AIModel`，不关心具体供应商 SDK。
- 使用工厂模式按照 `modelType` 创建模型实例，新增模型时只需实现统一接口并注册创建函数。
- 普通生成与流式生成使用同一套模型抽象，避免两套业务流程重复维护。
- 无用户状态的底层模型客户端按配置共享，会话实例只保存上下文相关状态。

**使用技术**

- Eino `ToolCallingChatModel` 作为统一模型能力接口。
- OpenAI Compatible API 接入 DashScope 等兼容服务。
- Eino Ollama 扩展接入本地模型。
- `singleflight` 合并相同配置下的并发客户端创建请求。
- 工厂模式与接口多态隔离模型实现。

### 4. SSE 流式对话

模型生成的内容通过 SSE 分块推送，响应中包含正文、引用、Token 用量、耗时和完成事件。

**设计理念**

- SSE 基于标准 HTTP，适合服务端单向持续推送，聊天场景下比轮询更及时，也比 WebSocket 更轻量。
- 每个文本分块使用 JSON 封装，避免换行内容被误解析为 SSE 控制字段。
- 请求上下文贯穿 Controller、Service 和模型层；客户端断开或请求超时后，模型调用能够及时取消。
- 流写入失败会向上传递，不再继续生成内容，避免无效 Token 消耗。
- 使用明确的 `chunk`、`citation`、`usage` 和 `done` 事件区分不同数据。

**使用技术**

- Go `http.Flusher` 实时刷新响应缓冲区。
- SSE `data:` 协议传输 JSON 数据。
- `context.Context` 处理超时、取消和客户端断连。
- Gin 提供路由、鉴权和请求生命周期管理。

### 5. 会话与消息持久化

会话和消息存储在 MySQL 中，服务重启后仍可恢复聊天记录和上下文。

**设计理念**

- 数据库是真实数据源，内存缓存只用于加速，避免服务重启后会话列表丢失。
- 创建会话与写入首条用户消息放在同一事务中，避免产生没有消息的空会话。
- 每个会话固定模型类型，防止中途切换模型导致缓存状态与持久化配置不一致。
- 对话前校验会话归属，阻止用户通过猜测 Session ID 访问他人数据。
- 消息记录保存完成、失败、取消和中断状态，并记录模型、Token、耗时、结束原因及引用信息，便于审计和排查问题。
- 历史消息采用分页查询并限制最大页大小，避免一次读取过多数据。

**使用技术**

- MySQL 保存用户、会话、消息和知识库文档元数据。
- GORM Transaction 保证会话与首条消息的原子写入。
- UUID 生成会话 ID 和消息事件 ID。
- Event ID 唯一索引用于消息幂等控制。
- GORM AutoMigrate 自动创建和更新数据表。

### 6. 长对话上下文管理

系统不会把全部历史消息直接发送给模型，而是按照 Token 预算构建上下文。

**设计理念**

- 为模型输出预留 Token 空间，剩余部分才用于历史上下文，避免超过模型窗口。
- 优先保留最近若干轮对话，较旧消息按预算裁剪。
- 被裁剪的内容整理为结构化摘要，分为事实、用户偏好、已做决定和待解决问题。
- 摘要与处理到的消息 ID 一并持久化，下一次请求只增量处理新的旧消息。
- 失败或取消的消息不进入有效上下文，防止错误内容影响后续回答。

**使用技术**

- 自定义 `ContextBuilder` 进行 Token 估算、消息选择和截断。
- MySQL 持久化会话摘要及摘要进度。
- Eino Schema 将数据库消息转换为模型所需的 User、Assistant 和 System 消息。

当前 Token 数使用轻量近似算法估算，减少每次请求进行精确分词的额外开销。

### 7. AI 会话缓存与并发控制

每个用户的每个会话对应一个 AIHelper，用于维护模型实例和已加载的聊天上下文。

**设计理念**

- 使用 Cache-Aside 思路：首次访问时从 MySQL 加载近期消息，之后复用内存中的会话实例。
- 缓存设置最大容量和 TTL，防止会话数量持续增长导致内存无界占用。
- 容量达到上限时淘汰最久未使用的实例。
- 相同会话同时首次访问时，只允许一个协程执行初始化，避免缓存击穿和重复加载历史记录。
- 缓存项移除或服务退出时主动关闭模型及 MCP 连接。

**使用技术**

- `sync.RWMutex` 保护共享 Map。
- `singleflight.Group` 合并同一会话的并发初始化。
- TTL 与近似 LRU 淘汰策略控制缓存规模。
- Go Context 将请求取消信号传入初始化过程。

### 8. 模型调用可靠性

外部模型服务可能出现限流、网络错误或短暂不可用，项目对可恢复错误进行统一处理。

**设计理念**

- 对错误分类，只有限流和暂时不可用等可恢复错误才重试，参数错误和鉴权错误直接返回。
- 使用指数退避并加入随机抖动，避免大量请求在相同时间再次冲击上游服务。
- 连续失败达到阈值后打开熔断器，在冷却时间内快速失败，防止故障扩散。
- 成功请求会重置失败计数，使服务恢复后自动回到正常状态。
- HTTP Server 设置读取、空闲和优雅退出超时；SSE 请求使用独立的 AI 超时控制。

**使用技术**

- 泛型重试函数统一包装普通生成和流式生成。
- 指数退避、Jitter 和按供应商维度的熔断状态。
- `signal.NotifyContext` 监听退出信号。
- `http.Server.Shutdown` 等待在途请求完成后关闭服务。

### 9. RAG 个人知识库

用户可以上传文档建立个人知识库，并选择 RAG 模型基于文档内容回答问题。

**设计理念**

- 文档按照近似 Token 数切分，并保留可配置重叠，减少语义在切片边界处丢失。
- 每个用户使用独立的索引名称和 Key 前缀，实现逻辑上的多租户数据隔离。
- 检索结果经过 Top-K、距离阈值、内容去重和总 Token 预算过滤，避免无关或重复内容占满上下文。
- 文档元数据保存在 MySQL，向量和文本切片保存在 Redis，分别承担状态管理与相似度检索职责。
- RAG 提示词明确将检索内容视为不可信参考数据，降低文档中提示词注入的影响。
- 回答要求使用来源标签，引用随消息一起保存并返回给客户端。

**使用技术**

- Eino Indexer、Retriever 和 Embedding 组件。
- DashScope Embedding API 生成文本向量。
- Redis Stack / RediSearch 建立 `FLAT` 向量索引，使用余弦距离检索。
- SHA-256 保存文件校验值，UUID 生成安全存储名。
- MySQL 保存索引状态、切片数量和失败原因。

### 10. 文档索引状态与失败重试

知识库文档具有 `indexing`、`ready` 和 `failed` 三种状态，并提供失败重试接口。

**设计理念**

- 将索引过程显式建模为状态机，调用方可以区分处理中、成功和失败，而不是只得到模糊的上传结果。
- 索引失败时记录错误并清理不完整的向量数据。
- 重建索引时先写入临时文档 ID，完整成功后再替换旧索引，避免失败重试破坏原有可用数据。
- 删除文档时同时清理向量、原始文件和数据库元数据。

**使用技术**

- MySQL 持久化文档状态和错误信息。
- Redis SCAN 分批查找文档切片，避免在大数据量下使用阻塞式 KEYS。
- 临时索引与替换策略实现接近 Copy-on-Write 的更新流程。

### 11. MCP 工具调用

MCP 模型可以从 MCP Server 获取工具定义，由模型选择工具、执行调用并根据结果继续生成答案。

**设计理念**

- 工具能力通过标准协议发现，不在业务代码中硬编码具体工具。
- 每次调用前校验必填参数和基础类型，减少无效或错误工具请求。
- 限制最大工具调用轮数、单次超时和返回内容大小，避免循环调用或超大结果占用资源。
- 相同 MCP 地址共享客户端连接，并使用引用计数管理生命周期。
- 工具调用失败后重置连接，下一次请求可重新初始化。

**使用技术**

- `mcp-go` 与 Streamable HTTP Transport。
- Eino Tool Calling 将 MCP Schema 转换为模型工具定义。
- JSON Schema 完成工具参数约束。
- 连接池、引用计数和互斥锁管理并发访问。

### 12. 扩展功能

项目还提供语音合成和图像识别接口。

**语音合成**

- 采用异步任务模式：创建任务后返回 Task ID，客户端通过查询接口获取状态和音频地址。
- 使用 HTTP Client 调用外部 TTS 服务，并通过 Context 控制请求生命周期。

**图像识别**

- 使用 ONNX Runtime 在本地加载模型进行推理。
- 上传文件经过类型、大小和路径检查后再解码、缩放和推理。
- 未启用 CGO 或 ONNX Runtime 时使用 Stub 实现返回明确错误，避免影响其他模块编译。

## 数据模型

| 数据表 | 主要用途 |
| --- | --- |
| `users` | 用户账号、邮箱和密码哈希 |
| `sessions` | 会话标题、所属用户、固定模型、摘要及摘要进度 |
| `messages` | 消息正文、状态、模型信息、Token、耗时和引用 |
| `rag_documents` | 文档路径、校验值、索引状态、切片数量和错误信息 |

应用启动时通过 GORM AutoMigrate 创建或更新数据表，但数据库本身需要提前创建。

## 项目结构

```text
GoAI-v2/
├── common/
│   ├── aihelper/      # 模型抽象、工厂、上下文、缓存、重试和熔断
│   ├── rag/           # 文档切分、向量索引、检索和引用构建
│   ├── redis/         # Redis、验证码和向量索引操作
│   ├── mysql/         # MySQL 连接与自动迁移
│   ├── mcp/           # MCP 示例服务
│   ├── kafkaqueue/    # 可选消息队列能力
│   ├── tts/           # 语音合成客户端
│   └── image/         # ONNX 图像识别
├── config/            # TOML 配置、环境变量覆盖和启动校验
├── controller/        # 参数解析与 HTTP 响应
├── dao/               # 数据访问层
├── middleware/        # JWT 鉴权与接口限流
├── model/             # 数据库模型和传输对象
├── router/            # API 路由注册
├── service/           # 业务逻辑
├── utils/             # 密码、JWT、文件校验等工具
├── .env.example       # 环境变量模板
├── go.mod
└── main.go            # 服务入口与生命周期管理
```

## 运行环境

- Go 1.24+
- MySQL 8.0+
- Redis Stack（RAG 依赖 RediSearch 向量检索）
- DashScope 或其他兼容 OpenAI 协议的模型服务

MCP Server、Kafka、SMTP、TTS 和 ONNX Runtime 只在使用对应功能时需要。

## 快速开始

### 1. 创建数据库

```sql
CREATE DATABASE GopherAI
    DEFAULT CHARACTER SET utf8mb4
    COLLATE utf8mb4_unicode_ci;
```

### 2. 配置环境变量

Linux / macOS：

```bash
cp .env.example .env.local
```

Windows PowerShell：

```powershell
Copy-Item .env.example .env.local
```

至少填写以下配置：

```dotenv
DASHSCOP_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
DASHSCOP_API_KEY=your-api-key
OPENAI_MODEL_NAME=qwen-turbo

JWT_SECRET=replace-with-at-least-32-random-characters

MYSQL_HOST=127.0.0.1
MYSQL_PORT=3306
MYSQL_USER=root
MYSQL_PASSWORD=your-password
MYSQL_DATABASE=GopherAI

REDIS_HOST=127.0.0.1
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
```

完整配置参见 [`.env.example`](./.env.example) 和 [`config/config.toml`](./config/config.toml)。请勿将真实密钥提交到 GitHub。

### 3. 启动服务

```bash
go mod download
go run .
```

服务默认监听：

```text
http://localhost:9090
```

## API 概览

| 模块 | 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- | --- |
| 用户 | POST | `/api/v1/user/captcha` | 否 | 发送邮箱验证码 |
| 用户 | POST | `/api/v1/user/register` | 否 | 用户注册 |
| 用户 | POST | `/api/v1/user/login` | 否 | 用户登录 |
| 模型 | GET | `/api/v1/AI/models` | 是 | 获取可用模型 |
| 会话 | GET | `/api/v1/AI/chat/sessions` | 是 | 获取用户会话 |
| 对话 | POST | `/api/v1/AI/chat/send-new-session` | 是 | 新建会话并发送消息 |
| 对话 | POST | `/api/v1/AI/chat/send` | 是 | 普通对话 |
| 对话 | POST | `/api/v1/AI/chat/send-stream-new-session` | 是 | 新建会话并流式回复 |
| 对话 | POST | `/api/v1/AI/chat/send-stream` | 是 | SSE 流式对话 |
| 历史 | POST | `/api/v1/AI/chat/history` | 是 | 分页查询聊天记录 |
| 语音 | POST | `/api/v1/AI/chat/tts` | 是 | 创建语音任务 |
| 语音 | GET | `/api/v1/AI/chat/tts/query` | 是 | 查询语音任务 |
| 知识库 | POST | `/api/v1/file/upload` | 是 | 上传并索引文档 |
| 知识库 | GET | `/api/v1/file/documents` | 是 | 查询文档列表 |
| 知识库 | DELETE | `/api/v1/file/documents/:id` | 是 | 删除文档及索引 |
| 知识库 | POST | `/api/v1/file/documents/:id/retry` | 是 | 重建失败索引 |
| 图像 | POST | `/api/v1/image/recognize` | 是 | 图像分类 |

受保护接口需要携带 JWT：

```http
Authorization: Bearer <token>
```

## 配置设计

项目使用 TOML 保存非敏感默认值，使用环境变量保存密钥和部署差异。启动时环境变量会覆盖 TOML，并对必要参数、端口、超时、JWT 密钥长度及 Token 配额进行校验；配置错误时服务快速失败，避免以不完整配置继续运行。

常用配置包括：

- `AI_MAX_CONTEXT_TOKENS`：模型上下文总上限。
- `AI_MAX_OUTPUT_TOKENS`：为回答预留的 Token。
- `AI_RETRY_MAX`：模型最大重试次数。
- `AI_CIRCUIT_FAILURES`：触发熔断的连续失败次数。
- `RAG_CHUNK_TOKENS`：知识库切片大小。
- `RAG_CHUNK_OVERLAP`：相邻切片重叠长度。
- `RAG_TOP_K`：向量检索候选数量。
- `RAG_DISTANCE_THRESHOLD`：向量距离过滤阈值。
- `MCP_MAX_TOOL_ROUNDS`：单次对话最大工具调用轮数。
- `AI_REQUEST_TIMEOUT_SECONDS`：AI 请求超时时间。

## 测试

```bash
go test ./...
```

测试覆盖模型管理器、并发初始化、流式回调错误、上下文裁剪、RAG 切片、消息持久化、会话事务、JWT、限流和 Controller 响应等核心逻辑。

## 当前限制

- 文档索引目前按纯文本读取，PDF、Word 等格式需要先增加内容解析器。
- Token 计算采用近似算法，并非模型官方 tokenizer 的精确结果。
- 会话缓存为单进程内存缓存，多实例部署需要增加分布式协调或保持请求亲和性。
- 接口限流为进程内实现，多实例统一限流可迁移至 Redis Lua 脚本。
- Kafka 模块属于可选能力，当前基础聊天链路采用同步持久化，不依赖 Kafka 启动。
- 图像识别依赖 ONNX Runtime 和 CGO，默认构建不保证该接口可用。

## 后续计划

- [ ] 增加 Docker Compose，统一启动 MySQL、Redis Stack 和后端服务
- [ ] 增加 Swagger / OpenAPI 文档
- [ ] 支持 PDF、DOCX 和 Markdown 文档解析
- [ ] 接入精确 tokenizer
- [ ] 增加 Prometheus 指标、链路追踪和结构化日志
- [ ] 将限流和会话协调扩展为多实例方案

## License

本项目目前未附带开源许可证。如需公开使用或参与贡献，建议在仓库中补充合适的 `LICENSE` 文件。
