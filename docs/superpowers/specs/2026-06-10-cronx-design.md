# cronx — Cron Scheduler Wrapper Design

## 概述

封装 `robfig/cron/v3`，提供与项目现有包（redisx/dbx）一致的 `Config + New()` 模式，返回底层 `*cron.Cron` 类型，由调用方管理任务注册和生命周期。

设计原则：**常见能力内置，高级需求开放扩展。**

## Config

```go
type Config struct {
    Timezone      string `mapstructure:"timezone"`       // 时区，如 "Asia/Shanghai"，空=Local
    WithSeconds   bool   `mapstructure:"with_seconds"`   // 秒级精度，默认 false
    LogLevel      string `mapstructure:"log_level"`      // silent | error | info，默认 info
    OverlapPolicy string `mapstructure:"overlap_policy"` // skip | delay | 空(不处理)
}
```

- `Timezone` — IANA 时区名，空字符串用 `time.Local`。无效时区导致 `New()` 返回 error。
- `WithSeconds` — 启用秒级 cron 表达式（6 字段而非标准 5 字段）。
- `LogLevel` — 控制 cron 内部日志输出：
  - `silent` — 不输出
  - `error` — 只输出错误
  - `info`（默认）— 输出任务调度信息（开始/结束等）
- `OverlapPolicy` — 任务重叠处理策略：
  - 空 — 不处理（默认），同一任务可并发执行
  - `skip` — `SkipIfStillRunning`，上次没跑完就跳过本次
  - `delay` — `DelayIfStillRunning`，等上次跑完立即再跑

## Option（扩展机制）

```go
type Option func(*cronOptions)

// WithCronOption 透传底层 cron.Option，用于高级定制（自定义 Parser 等）
func WithCronOption(opt cron.Option) Option
```

调用方可以通过 `WithCronOption` 传入任何 `cron.Option`，包括自定义 `WithParser`、额外的 `WithChain` wrapper 等。

## New()

```go
func New(cfg *Config, opts ...Option) (*cron.Cron, error)
```

逻辑：

1. 解析时区：`time.LoadLocation(cfg.Timezone)`，空则用 `time.Local`
2. 创建 slog 日志适配器：`newSlogLogger(cfg.LogLevel)`
3. 构建 `cron.Option` 列表：
   - 时区：`cron.WithLocation(loc)`
   - 秒级：`cron.WithSeconds()`（按配置）
   - 日志：`cron.WithLogger(logger)`
   - Panic recover：`cron.Recover(logger)` — 始终启用
   - 重叠策略：根据 `OverlapPolicy` 加 `SkipIfStillRunning(logger)` 或 `DelayIfStillRunning(logger)`
4. 合并调用方通过 `WithCronOption` 传入的额外 option
5. 返回 `cron.New(allOpts...)`

错误处理：只有时区解析失败返回 error，用 `fmt.Errorf("cronx: invalid timezone %q: %w", ...)` 包装。

## slog 日志适配器

实现 `cron.Logger` 接口，用 `log/slog` 输出：

```go
type slogLogger struct {
    level string // silent, error, info
}

func (l *slogLogger) Info(msg string, keysAndValues ...any)
func (l *slogLogger) Error(err error, msg string, keysAndValues ...any)
```

- `silent` — 所有方法 no-op
- `error` — 只输出 Error
- `info` — 输出 Info 和 Error

## 文件结构

```
cronx/
├── cronx.go            # Config + Option + New()
├── slog_logger.go      # cron.Logger 的 slog 适配器
├── cronx_test.go       # New() 测试
└── slog_logger_test.go # slog adapter 测试
```

不需要 testhelpers.go — cron 是纯内存调度，无外部依赖。

## 用法示例

### 基本用法

```go
c, err := cronx.New(&cronx.Config{
    Timezone:    "Asia/Shanghai",
    WithSeconds: true,
    LogLevel:    "info",
})
if err != nil {
    log.Fatal(err)
}

c.AddFunc("0 30 * * * *", func() { /* 每小时30分执行 */ })
c.AddFunc("@every 5m", func() { /* 每5分钟执行 */ })
c.Start()

defer c.Stop()
```

### 带重叠策略

```go
c, err := cronx.New(&cronx.Config{
    OverlapPolicy: "skip", // 上次没跑完就跳过
})
```

### 高级定制（透传 cron.Option）

```go
c, err := cronx.New(&cronx.Config{},
    cronx.WithCronOption(cron.WithParser(
        cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor),
    )),
)
```

## 测试计划

| 测试 | 覆盖内容 |
|------|---------|
| TestNew_DefaultConfig | 默认配置，验证返回非 nil |
| TestNew_WithTimezone | 指定时区，验证 Location() 正确 |
| TestNew_InvalidTimezone | 无效时区返回 error |
| TestNew_WithSeconds | 秒级表达式正常工作 |
| TestNew_PanicRecover | 任务 panic 不影响 scheduler，通过 slog 记录 |
| TestNew_OverlapSkip | SkipIfStillRunning 策略生效 |
| TestNew_OverlapDelay | DelayIfStillRunning 策略生效 |
| TestNew_WithCronOption | 透传自定义 cron.Option |
| TestSlogLogger_Info | info 级别输出 Info 和 Error |
| TestSlogLogger_Error | error 级别只输出 Error |
| TestSlogLogger_Silent | silent 级别不输出 |
| TestIntegration | 验证 cron 实际触发回调 |

## cron 能力覆盖

| cron 能力 | cronx 如何覆盖 |
|-----------|---------------|
| `WithLocation` | Config.Timezone |
| `WithSeconds` | Config.WithSeconds |
| `WithLogger` | Config.LogLevel + slog 适配器 |
| `WithChain(Recover)` | 内置，始终启用 |
| `WithChain(SkipIfStillRunning)` | Config.OverlapPolicy = "skip" |
| `WithChain(DelayIfStillRunning)` | Config.OverlapPolicy = "delay" |
| `WithChain(自定义)` | WithCronOption(cron.WithChain(...)) |
| `WithParser` | WithCronOption(cron.WithParser(...)) |

## 关联

- 遵循 CLAUDE.md 中的编码规范
- 对齐 redisx/dbx 的 Config + New() 模式
