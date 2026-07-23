# CLAUDE.md — go-common

## 项目定位

go-common 是基础工具库，供业务服务引入。包含错误码、验证码、限流、消息生命周期等通用能力。

## 技术栈约定

### Redis

- 使用 `github.com/redis/go-redis/v9`，不使用其他 Redis 客户端
- 统一使用 `redisx.New(cfg)` 创建客户端（含 Ping 验证）
- 客户端通过构造函数注入（`func NewXxx(client *redis.Client)`），不持有全局连接
- key 命名：`<module>:<purpose>:<identifier>`，如 `captcha:code:login:13800001111`
- 测试用 `redisx.NewTestClient(t)` 做内存 Redis（miniredis 封装）

### PostgreSQL / GORM

- 使用 `gorm.io/gorm` + `gorm.io/driver/postgres`
- 统一使用 `dbx.New(cfg)` 创建数据库连接（含连接池、slog 日志、禁用外键）
- GORM 日志通过 `dbx` 内置的 slog logger 实现，与 `log/slog` 统一
- 配置项 `log_level` 控制 GORM 日志级别：silent, error, warn, info
- 使用 `gorm.io/cli` 做代码生成（参考 gorm-cli-development skill）
- Model 定义在 `internal/models/`，生成代码输出到 `internal/generated/`

### 数据库集成测试

- 使用 `dbx.SetupTestDB(t)` 启动 PostgreSQL testcontainer（已封装在 dbx 包）
- 每个测试用例前清理数据（truncate 或事务回滚），保证测试隔离
- Error path（连接失败、超时等）可用 `go-sqlmock` 做单元测试补充

### 日志

- **本库是基础库，原则上不写日志** — 所有诊断信息通过返回 error 交给调用方处理
- 如果确实需要日志（如 provider fallback 等内部流程），使用标准库 `log/slog`
- 禁止使用 `fmt.Println`、`log.Println` 等非结构化输出

### 通用

- Go 版本：见 `go.mod`
- 使用现代 Go 语法：`any` 代替 `interface{}`，`~` 在类型约束中等
- 错误处理：返回 error，不用 panic；错误包装用 `fmt.Errorf("context: %w", err)`
- 构造函数的 Config 参数使用指针（`func NewProvider(cfg *Config)`），便于依赖注入框架集成
- 遵循 golang-development skill 的编码规范

### 第三方集成

- 引入第三方服务时，优先查找其官方最新 SDK
- 实现复杂功能前，先在 GitHub 上搜索是否有成熟的开源实现

## 文件内声明顺序

每个 `.go` 文件（除 `*_test.go` 和生成文件外）的顶层声明按 golang-development skill §7 的顺序:

1. `package` + 包/文件注释
2. `import`（标准库 → 第三方 → 项目内部）
3. **类型声明**（`type` / `struct` / `interface`）— 所有类型集中放
4. **构造函数** `New*()` — 紧贴类型,便于"先看数据形状,再看怎么构造"
5. **常量** `const`
6. **包级变量** `var` — 越少越好
7. **导出方法**（按 receiver 分组,先导出方法、后未导出方法）
8. **导出函数**（非 method）
9. **未导出方法**（按 receiver 分组）
10. **未导出函数**（文件底部）

**约束：**

- `init()` 必须在所有其它函数之前(decorder 强制)
- 同一 receiver 的方法集中放,不要散到文件多处
- 同类常量合并成一个 `const (...)` 块,不要散开多条 `const`
- With* Option helpers 紧跟 New* 构造函数(语义分组)
- `*_test.go` 与生成文件(`*_generated.go`、`*.pb.go`、`internal/generated/` 下)豁免
- 由 `.golangci.yml` 中的 `decorder` linter 强制(`disable-init-func-first-check: false`)
- 该规则仅约束排版,不影响逻辑

**目的：** 使用方打开任意源文件,从上往下扫一眼就能看到:数据形状 → 怎么构造 → 默认值 → 行为。这是 Go 业界主流(decorder 默认、Uber style、标准库样本都这么做)。

## Config 子配置必须用指针

顶层 `Config` struct 里的**子配置 struct 字段必须用指针**(`*ServerConfig`、`*dbx.Config`、`*redisx.Config`),`New*` 构造函数的 Config 参数也必须用指针(`func New(cfg *Config)`)。详见 golang-development skill §14。

**为什么：**
1. 跟整体风格一致(Load 返回 `*Config`,字段也用指针,内外一致)
2. 修改子配置类型不 breaking(从单值变 struct,父字段已是指针)
3. 避免大 struct 拷贝
4. 业务侧代码不变(Go 自动解引用单层指针,`cfg.Server.GRPCAddr` 等用法照常)

## 代码质量

- 测试覆盖率目标：**85%**，CI 中强制执行

```bash
# 格式化
gofmt -w <file.go>
goimports -w <file.go>

# Lint
golangci-lint run ./...

# 测试
go test ./... -cover
```

## Skill 维护

仓库根目录有一份使用指南 skill: `skills/go-common-usage/SKILL.md`。业务服务安装后会加载它来指导正确的 API 用法。当仓库本身做了影响调用方的变更时，必须同步更新这个 skill —— 否则使用方按过时文档写代码会踩坑（构造函数签名变了、Config 字段加了默认值、新模块没人知道存在）。

**需要更新 skill 的场景：**

- **新增模块或子包**（例如加 `ocr/`）：在 skill 的"模块速查"表加一行，并新增对应小节（Config / 构造函数 / 最简示例 / 关键约定）
- **新增导出 API**（Config 字段、构造函数、关键方法、`With*` Option、错误类型）：更新对应模块小节的示例和字段说明
- **修改既有 API 的签名、默认值或行为约定**：同步修正示例和"关键约定"段落
- **新增 Provider 实现**（如新的 SMS/Email provider）：在对应小节加 Provider 条目，说明 Config 和构造函数
- **新增预定义常量或错误码**（如 `xcodes.ErrXxx`、`captcha.FormatXxx`）：更新对应小节的列表

**不需要更新 skill 的场景：**

- 纯内部重构、私有函数调整
- test 文件修改
- 不改变调用方体验的性能优化
- bug 修复（除非修复改变了文档化的行为契约）

**判断标准：** 使用方读完 skill 后能否写出与最新代码一致的调用。如果能，不用动；如果不能，就需要更新。

更新时优先编辑对应模块小节，必要时调整"模块速查"表和"常见反模式 → 正确做法"对照表。

## 目录结构

```
go-common/
├── captcha/      # 验证码生成、存储、限流、校验
├── configx/      # yaml/env/flag 配置加载
├── cronx/        # 定时任务（panic recovery + slog）
├── dbx/          # GORM 数据库初始化（PostgreSQL + slog logger）
├── gorx/         # goroutine 安全（GoSafe / RoutineGroup / TaskRunner）
├── grpcx/        # gRPC server 封装（含 health / gateway / interceptors）
├── jsonx/        # 高性能 JSON（sonic + encoding/json fallback）
├── lifecycle/    # 多组件生命周期编排
├── logging/      # slog 全局 logger 初始化
├── ptr/          # 指针工具（Ref / Deref）
├── ratelimit/    # Redis 固定窗口限流
├── redisx/       # Redis 客户端初始化 + 分布式锁
├── signalx/      # 信号驱动优雅关闭
├── xerr/         # 错误码与结构化错误
│   └── xcodes/
└── go.mod
```
