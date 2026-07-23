# go-common/captcha v2 设计文档

> 验证码服务模块 v2，基于 Config struct 重构，内部封装 Redis store、rate limiter、code generator。
> 调用方只需提供 Config，一行初始化即可使用。

**依赖：** `go-common/message`、`go-common/ratelimit`

## 1. 设计目标

相比 v1 的改进：
- **TTL 自动推导**：从限流规则的最短窗口推导，不再手动配置 `WithTTL`，消除 TTL 与限流不一致的风险
- **内部封装**：RedisStore、RateLimiter、CodeGenerator 全部内部创建，调用方只需提供 Config
- **Config struct**：替代 Functional Options，配置集中、一目了然
- **Redis client 灵活注入**：支持 `WithRedisClient` 传入已有实例，或通过 Config 中的 RedisConfig 创建；两者都没有则报错

## 2. 配置结构

```go
// RedisConfig holds Redis connection info (typically from config file).
type RedisConfig struct {
    Addr     string
    Password string
    DB       int
}

// Config holds all captcha service configuration.
// Can be loaded from config file (YAML/JSON etc).
type Config struct {
    Prefix      string                   // Redis key prefix, default "captcha"
    Redis       RedisConfig              // Redis connection info, required if no WithRedisClient option
    MaxAttempts int                       // max verify attempts per captcha, default 3
    GlobalRules []ratelimit.Rule          // global rate limits applied across all purposes
    Purposes    map[string]PurposeConfig  // purpose → config
}

// Option configures a Captcha instance.
type Option func(*Captcha)

// WithRedisClient provides an existing Redis client instance.
// Overrides Config.Redis — no new connection is created.
func WithRedisClient(client *redis.Client) Option
```

// PurposeConfig groups all settings for a single purpose.
type PurposeConfig struct {
    CodeFormat CodeFormat        // verification code format, default FormatDigit6
    RateRules  []ratelimit.Rule  // rate limit rules for this purpose
    Channels   []ChannelConfig   // supported delivery channels
}

// ChannelConfig describes a delivery channel for a purpose.
type ChannelConfig struct {
    Channel Channel
    Sender  ChannelSender // EmailChannel or SMSChannel instance
}
```

### TTL 推导

验证码有效期 = 该 purpose 的 RateRules 中最短窗口。
- 有 RateRules → `min(rule.Window)`
- 无 RateRules → 默认 5 分钟

原理：最短窗口到期后才能发新码，旧码也同时过期，保持一致性。

## 3. 核心结构

```go
type Captcha struct {
    client      *redis.Client
    store       Store
    limiter     ratelimit.Limiter
    codeGen     *CodeGenerator
    channels    map[string]map[Channel]ChannelSender // purpose → channel → sender
    purposes    map[string]PurposeConfig
    maxAttempts int
}

// New creates a Captcha service from config + optional overrides.
func New(cfg Config, opts ...Option) (*Captcha, error)
```

### New 内部流程

1. Apply options → 检查是否通过 `WithRedisClient` 传入了实例
2. 如果没有传入实例：
   - 用 `cfg.Redis` (RedisConfig) 创建 `redis.Client`
   - 如果 `cfg.Redis` 为空（Addr 为空）→ 返回错误
3. 从 `cfg.Purposes` 提取所有 RateRules + `cfg.GlobalRules` → 构建 `ratelimit.Config` → 创建 `RedisLimiter`
4. 从 `cfg.Purposes` 提取所有 CodeFormat → 创建 `CodeGenerator`
5. 创建 `RedisStore`
6. 构建 `channels` map（按 purpose 索引）

## 4. 接口（不变）

```go
// Send generates, stores, and delivers a verification code.
func (c *Captcha) Send(ctx context.Context, target string, channel Channel, purpose string) (string, error)

// Verify checks a verification code by target and purpose.
func (c *Captcha) Verify(ctx context.Context, target, code, purpose string) (*VerifyResult, error)
```

### Send 流程

1. 校验 purpose 是否注册 → 否则返回错误
2. 校验 channel 是否在该 purpose 的 channels 里 → 否则返回错误
3. 限流检查：`limiter.Allow(purpose, target)` → 超限返回错误
4. 生成验证码：`codeGen.Generate(purpose)`
5. TTL：从该 purpose 的 RateRules 推导最短窗口
6. 生成 captchaID：`uuid.New().String()`
7. 存储到 Redis：key = `captcha:<purpose>:<target>`，value = Record JSON
8. 发送消息：调用对应 channel sender
9. 返回 captchaID

### Verify 流程

委托给 `store.Verify(purpose, target, code)`，Lua 脚本原子操作。

## 5. 不变的部分

以下保持现有设计不变：
- `Store` 接口 + `RedisStore`（Lua 原子校验）
- `Record` / `VerifyResult` 结构
- `ChannelSender` 接口
- `EmailChannel` / `SMSChannel` 及对应的 `EmailMessageFunc` / `SmsMessageFunc`
- `CodeGenerator` + `CodeFormat` 预定义格式
- `Channel` 常量（`ChannelEmail` / `ChannelSMS`）

## 6. 移除的部分

- `WithTTL` / `WithRateLimit` / `WithChannel` / `WithMaxAttempts` — 被 Config struct 替代
- `NewCaptcha(client, codeGen, opts...)` — 被 `New(cfg, opts...)` 替代
- `Option` 类型改为只有 `WithRedisClient`

## 7. 调用示例

```go
// From config file
c, err := captcha.New(captcha.Config{
    Prefix:      "captcha",
    Redis:       captcha.RedisConfig{Addr: "localhost:6379"},
    MaxAttempts: 3,
    GlobalRules: []ratelimit.Rule{
        {Window: 24 * time.Hour, Max: 100},
    },
    Purposes: map[string]captcha.PurposeConfig{
        "register": {
            CodeFormat: captcha.FormatDigit6,
            RateRules: []ratelimit.Rule{
                {Window: time.Minute, Max: 1},
                {Window: time.Hour, Max: 5},
            },
            Channels: []captcha.ChannelConfig{
                {Channel: captcha.ChannelEmail, Sender: captcha.NewEmailChannel(emailSender, myEmailMsg)},
                {Channel: captcha.ChannelSMS, Sender: captcha.NewSMSChannel(smsSender, mySmsMsg)},
            },
        },
        "login": {
            RateRules: []ratelimit.Rule{
                {Window: 5 * time.Minute, Max: 3},
            },
            Channels: []captcha.ChannelConfig{
                {Channel: captcha.ChannelSMS, Sender: captcha.NewSMSChannel(smsSender, loginSmsMsg)},
            },
        },
    },
})

// Or with existing Redis client
c, err := captcha.New(cfg, captcha.WithRedisClient(existingClient))
```

// Send
captchaID, err := c.Send(ctx, "13800001111", captcha.ChannelSMS, "register")

// Verify
result, err := c.Verify(ctx, "13800001111", "123456", "register")
```

## 8. 模块结构

```
go-common/captcha/
├── captcha.go          # Config, Captcha struct, New(), Send, Verify
├── captcha_test.go     # 核心测试
├── store.go            # Store interface + RedisStore (Lua atomic verify)
├── store_test.go       # Store tests
├── generator.go        # CodeGenerator (configurable: length/charset/case)
└── generator_test.go   # CodeGenerator tests

go-common/ratelimit/
├── ratelimit.go        # Limiter interface + RedisLimiter (fixed window)
└── ratelimit_test.go   # Rate limit tests
```

## 关联

**限流包：** [[services/go-common/ratelimit/ratelimit|ratelimit]]
**消息包：** [[services/go-common/message/message-design|message-design]]
