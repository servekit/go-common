# go-common

go-common 是公司内部 Go 基础库,模块路径 `github.com/servekit/go-common`,封装了配置加载、Redis/PostgreSQL、限流、验证码、消息发送、错误码、gRPC、定时任务、生命周期等通用能力。

**核心原则:写新业务前,先确认 go-common 是否已经有现成的包。** 重复造轮子(自己写 SetNX 锁、自己拼 DSN、自己 wrap error)是被严格禁止的。

## 引入方式

```bash
go get github.com/servekit/go-common@latest
```

按需 import 单个子包:

```go
import (
    "github.com/servekit/go-common/redisx"
    "github.com/servekit/go-common/dbx"
    "github.com/servekit/go-common/captcha"
)
```

不要 import 父包 `go-common` 本身——它不是有效 import path,所有 API 都在子包里。

## 全局约定(影响所有模块)

- **构造函数的 Config 一律传指针**:`func NewXxx(cfg *Config)`。便于依赖注入框架处理 nil 默认值,与 viper 解码出的指针字段对齐。
- **本库不写日志**:所有诊断信息通过 error 返回交给调用方。少数 provider fallback 流程确实需要日志时,用标准库 `log/slog`。`fmt.Println` / `log.Println` 是反模式。
- **错误包装**:`fmt.Errorf("context: %w", err)`,不要裸 return。
- **Redis key 命名**:`<module>:<purpose>:<identifier>`,例如 `captcha:code:login:13800001111`、`ratelimit:login:13800001111`。
- **测试隔离**:Redis 测试用 `redisx.NewTestClient(t)`,PostgreSQL 测试用 `dbx.SetupTestDB(t)`。两者都已封装好(miniredis / testcontainers)。
- **现代 Go 语法**:`any` 替代 `interface{}`,Go 1.26+。

---

## 模块速查

| 需求 | 模块 | 入口 |
|------|------|------|
| 加载 yaml/env/flag 配置 | `configx` | `configx.Load(&cfg, opts...)` |
| 连 Redis(standalone 或 sentinel) | `redisx` | `redisx.New(cfg)` |
| 连 PostgreSQL(GORM) | `dbx` | `dbx.New(cfg)` |
| 验证码生成、存储、限流、校验 | `captcha` | `captcha.New(cfg)` |
| Redis 固定窗口限流 | `ratelimit` | `ratelimit.NewRedisLimiter(client, cfg)` |
| Redis 分布式锁 | `redisx` | `redisx.NewLock(client, cfg)` |
| 定时任务 | `cronx` | `cronx.New(cfg)` |
| gRPC + HTTP gateway | `grpcx` | `grpcx.New(cfg, reg, regMW).Run()` |
| 并发 / goroutine 安全 | `gorx` | `gorx.GoSafe(fn)` / `gorx.NewRoutineGroup()` |
| 服务组件生命周期编排 | `lifecycle` | `lifecycle.NewManager()` |
| 信号 → 优雅关闭 | `signalx` | `signalx.Run(svc)` |
| 结构化错误码 | `xerr` + `xerr/xcodes` | `xcodes.ErrNotFound.New("user 123")` |
| 高性能 JSON | `jsonx` | `jsonx.Marshal(v)` |
| 指针工具 | `ptr` | `ptr.Ref(v)` / `ptr.Deref(p)` |
| 全局 slog logger 初始化 | `logging` | `logging.Setup(cfg)` |

---

## 基础设施层

### configx — 配置加载

封装 viper,提供单函数 `Load`,处理文件查找(pflag → env → 文件系统搜索)、env 覆盖、`default:"<value>"` 标签默认值、tagless struct 匹配。

```go
type Config struct {
    Server struct {
        GRPCAddr string `default:":9000"`
        HTTPAddr string
    }
    Redis *redisx.Config  // 子配置必须用指针 (§14)
    DB    *dbx.Config
}

var cfg Config
if err := configx.Load(&cfg,
    configx.WithServiceName("pay-service"),
    configx.WithEnvPrefix("PAY_SERVICE"),
); err != nil {
    log.Fatal(err)
}
```

**配置文件查找顺序**:
1. `-config` pflag(如 `-config /etc/pay-service/config.yaml`)
2. `<SERVICE_NAME>_CONFIG` env var(只在用了 `WithServiceName` 时生效,横线转下划线:`pay-service` → `PAY_SERVICE_CONFIG`)
3. 文件系统搜索:在 `WithConfigPaths` 指定的目录里找 `config.<ext>`。默认 `["."]`,设了 `WithServiceName` 后追加 `/etc/<name>`。

**env 覆盖规则**:
- 不设前缀:`SERVER_GRPC_ADDR=:9000`(点变下划线)
- 设前缀:`PAY_SERVICE_SERVER_GRPC_ADDR=:9000`

**关键约定**:不需要 `mapstructure:""` tag。`snake_case` 配置键自动匹配 `CamelCase` 字段(`server.grpc_addr` ↔ `Server.GRPCAddr`)。已内置 `time.Duration`、`[]string`(逗号分隔)、`time.Time`(RFC3339)的 decode hook。

**Options**:`WithServiceName` / `WithEnvPrefix` / `WithConfigName` / `WithConfigPaths` / `WithDecodeHooks`。

### logging — slog 全局 logger

`logging.Setup(cfg)` 配置全局 `slog` logger,可选 JSON / text 格式,可选文件输出 + lumberjack 滚动。

```go
logging.Setup(&logging.Config{
    Level:   "info",
    Format:  "json",
    Service: "pay-service", // 日志前缀 [pay-service]
    File: &logging.FileConfig{
        Path:       "/var/log/pay-service/app.log",
        MaxSizeMB:  100,
        MaxBackups: 3,
        MaxAgeDays: 7,
        Compress:   true,
    },
})
```

业务服务在 main 入口调用一次,之后 `slog.Info(...)` / `slog.Error(...)` 直接用。

---

## 数据层

### redisx — Redis 客户端

`redisx.New(cfg)` 创建 `*redis.Client` 并 Ping 验证。支持 standalone(`Addr`)和 sentinel(`MasterName` + `SentinelAddrs`);sentinel 优先。**不支持 Redis Cluster**(cluster 解决分片,不解决 HA,HA 用 sentinel)。

```go
client, err := redisx.New(&redisx.Config{
    Addr:         "localhost:6379",
    Password:     "...",
    DB:           0,
    PoolSize:     50,
    MinIdleConns: 10,
})
```

**关键字段**:
- `Addr` (standalone) 或 `MasterName` + `SentinelAddrs` (sentinel),二选一。
- `PoolSize`: socket 上限。0 = `10*GOMAXPROCS`。`redis.ErrPoolExhausted` 时调大。
- `MinIdleConns`: 预热连接,典型 PoolSize 的 10–20%。
- `DialTimeout`(默认 5s)、`ReadTimeout`(默认 3s)、`WriteTimeout`(默认 3s):阈值下限是 100ms / 1s / 1s,`Validate()` 会拒绝过小值避免抖动。
- `MaxRetries`(默认 3):瞬时失败重试。设 -1 让调用方自己处理重试(非幂等写)。
- `ClientName`: 通过 `CLIENT SETNAME` 设置,排查连接池归属时有用。

**测试**:`redisx.NewTestClient(t)` 返回基于 miniredis 的内存客户端,用完自动清理。

**分布式锁**:`redisx.NewLock(client, &LockConfig{Prefix, TTL, Tries, Wait})` → `Acquire(ctx, target)` 返回 id → 业务逻辑 → `Release(ctx, target, id)`。长任务用 `KeepAlive(ctx, target, id)` 续期(返回 CancelFunc)。

### dbx — PostgreSQL + GORM

`dbx.New(cfg)` 创建 `*gorm.DB`,内置连接池配置、slog logger、禁用外键约束(默认)、可选 table 前缀。

```go
db, err := dbx.New(&dbx.Config{
    Host:            "localhost",
    Port:            5432,
    User:            "pay",
    Password:        "...",
    DBName:          "pay",
    SSLMode:         "disable",
    MaxOpenConns:    50,
    MaxIdleConns:    10,
    ConnMaxLifetime: 30 * time.Minute,
    LogLevel:        "warn",        // silent | error | warn | info
    SlowThreshold:   200 * time.Millisecond,
    SkipDefaultTx:   true,          // 单操作跳过默认事务(生产推荐)
    DisableFK:       true,          // 迁移时禁用外键约束
    TablePrefix:     "stor_",       // 表名前缀
})
```

**关键约定**:
- `LogLevel` 控制日志:测试用 `silent`,生产用 `warn`(只打慢查询和错误)。
- `SkipDefaultTx = true`:GORM 默认每条写都开事务,单操作时是浪费。
- `DisableFK = true`:迁移时不创建外键,业务层维护关系。
- 迁移:`dbx.AutoMigrate(db, &User{}, &Order{})`(带 slog 日志)。

**分页**:`OffsetPaginate[T](tx, PageParams{Page, PageSize})` 返回 `*PageResult[T]{Items, Total, Page, PageSize}`。`ClampPageSize(size)` 防止超大 page size 拖垮 DB。

**测试**:`dbx.SetupTestDB(t)` 启动 PostgreSQL testcontainer,自动 truncate。错误路径(连接失败、SQL 错误)用 `go-sqlmock` 补充单元测试。

---

## 业务能力层

### ratelimit — Redis 固定窗口限流

```go
limiter := ratelimit.NewRedisLimiter(client, &ratelimit.Config{
    Prefix: "login:rate",
    Global: []*ratelimit.Rule{
        {Window: time.Minute, Max: 10},
    },
    Rules: map[string][]*ratelimit.Rule{
        "login": {{Window: time.Minute, Max: 5}, {Window: time.Hour, Max: 20}},
    },
})

ok, err := limiter.Allow(ctx, "login", "13800001111")
```

**关键点**:
- 固定窗口算法(非滑动窗口、非令牌桶)。够用就别换。
- key 经过 hash,**兼容 Redis Cluster**。
- `Allow(ctx, purpose, target)`:phase 1 检查所有规则,phase 2 全部 INCR,要么全过要么全失败(原子性由 Lua 脚本保证)。
- `Stats(ctx, target)`:查看所有窗口当前计数。
- `Reset(ctx, target)` / `ResetPurpose(ctx, purpose, target)`:清零。

### captcha — 验证码全流程

一站式:生成、存储、限流、校验、防爆破。配置驱动的多 purpose 设计。

```go
cap, err := captcha.New(&captcha.Config{
    Prefix: "captcha",
    Redis:  redisCfg, // *redisx.Config — 不传 WithRedisClient 时用这个建新连接
    MaxAttempts: 3,
    Purposes: map[string]*captcha.PurposeConfig{  // 子配置必须用指针 (§14)
        "login": {
            CodeFormat: captcha.FormatDigit6, // *CodeFormat
            RateRules:  []*ratelimit.Rule{{Window: time.Minute, Max: 1}},
        },
        "register": {
            CodeFormat: captcha.FormatAlphaNum8,
            RateRules:  []*ratelimit.Rule{{Window: time.Hour, Max: 5}},
        },
    },
})

// 生成并通过 WithSend hook 接管投递（投递通道由调用方决定）
id, code, err := cap.Generate(ctx, phone, "login", "sms",
    captcha.WithSend(func(ctx context.Context, target, code, purpose, channel string) error {
        // 调用方在此处接入自己的邮件/短信/推送通道
        return mySender.Send(ctx, target, "your code: " + code)
    }),
)

// 校验(用 id 绑定会话,防跨上下文重放)
result, err := cap.Verify(ctx, phone, code, "login", "sms", captcha.WithCaptchaID(id))
```

**关键概念**:
- `target`(手机号/邮箱)+ `purpose`(login/register/reset)+ `channel`(sms/email)三元组定位一条记录。
- `channel` 防止同 target+purpose 不同渠道(短信 vs 邮件)撞 key。
- TTL 取自该 purpose 最短限流窗口;无限流时默认 5 分钟。
- `MaxAttempts`(默认 3):错误次数超限自动删除记录。
- `WithCaptchaID(id)`:防御跨上下文重放攻击(浏览器 flow 生成的码不能拿到 app flow 验)。生产必填。
- 预定义格式:`FormatDigit6` / `FormatDigit4` / `FormatAlphaNum8` / `FormatAlphaMixed6`,也可用 `CodeFormat{Length, Charset, Case}` 自定义。
- **投递通道由调用方接入**:`WithSend` hook 接收 `(target, code, purpose, channel)`，由业务侧决定走哪家 SMS/Email provider。go-common 不内置任何 provider 实现。

---

## 服务编排层

### lifecycle — 多组件生命周期

`Manager` 并发启动多个 `Service`,顺序触发启动、并发触发停止、超时保护。

```go
mgr := lifecycle.NewManager(
    lifecycle.WithStopTimeout(30 * time.Second),
)
mgr.Add("grpc", grpcServer)        // grpcx.Server 实现了 Service 接口
mgr.Add("http", httpServer)
mgr.AddStarter("worker", starter)  // 只有 Start 的组件
mgr.AddStopper("db", dbStopper)    // 只有 Stop 的组件

if err := mgr.Start(); err != nil { log.Fatal(err) }
// ... 阻塞等信号 ...
if err := mgr.Stop(); err != nil { slog.Error("stop", "err", err) }
```

**适配器**:`StartFunc` / `StopFunc` / `starterOnly` / `stopperOnly` 把函数或单边接口包装成完整 `Service`。

**关键点**:
- 启动是顺序的——前一个失败就停,不再启后面的。这避免了"db 没起来就启了 grpc"的脏状态。
- 停止是并发的,等待所有完成或超时。
- `Run(ctx)`:启动后阻塞到 ctx.Done(),再 Stop。和 `signal.NotifyContext` 配合很自然。

### signalx — 信号驱动优雅关闭

```go
// 模式一:简单服务直接 Run
srv := grpcx.New(cfg, reg, regMW)
if err := srv.Run(); err != nil { log.Fatal(err) } // 内部就是 signalx.Run(srv)
```

```go
// 模式二:多个组件,先 lifecycle.Manager 再 signalx
ctx, cancel := signal.NotifyContext(context.Background(), signalx.DefaultSignals...)
defer cancel()
mgr := lifecycle.NewManager()
mgr.Add("grpc", srv)
// ...
go func() { <-ctx.Done(); mgr.Stop() }()
if err := mgr.Start(); err != nil { log.Fatal(err) }
<-ctx.Done()
```

**两个变体**:
- `Run(svc, sigs...)`:第一次 SIGINT/SIGTERM 触发 Stop,返回其结果。**Stop 期间再来信号会被忽略**——caller 决定策略。
- `RunWithForceQuit(svc, sigs...)`:Stop 期间第二次信号 → SIGKILL 自杀(用于卡死时强制退出,defer 不执行)。

### cronx — 定时任务

封装 `robfig/cron/v3`,默认带 panic recovery + slog logger。

```go
cron, err := cronx.New(&cronx.Config{
    Timezone:    "Asia/Shanghai",
    WithSeconds: false,
    LogLevel:    "info",
    OverlapPolicy: "skip", // 上次没跑完时跳过本次;另外有 "delay"
})

cron.AddFunc("0 */5 * * * *", func() {
    // 每 5 分钟跑一次,panic 自动 recover 并 slog.Error
})

cron.Start()
defer cron.Stop()
```

**工作日 / 周末过滤**:`cronx.OnlyWorkdays(s)` / `cronx.OnlyWeekends(s)` 包装 schedule。`cron.AddJob(spec, cronx.OnlyWorkdays(spec))`。

### grpcx — gRPC + HTTP Gateway

`Server` 实现 `signalx.Service`,可以直接 `signalx.Run(srv)`。

```go
srv := grpcx.New(
    &grpcx.ServerConfig{
        GRPCAddr:    ":9000",
        GatewayAddr: ":8080", // 空 = 不启 gateway
    },
    func(s *grpc.Server) {
        pb.RegisterPayServiceServer(s, payImpl)
    },
    func(ctx context.Context, mux *runtime.ServeMux, endpoint string, opts []grpc.DialOption) error {
        return pb.RegisterPayServiceHandlerFromEndpoint(ctx, mux, endpoint, opts)
    }, // nil 时不启 gateway
    grpcx.ErrorInterceptor, // 把 *xerr.Error 映射成对应 gRPC code
)

if err := srv.Run(); err != nil { log.Fatal(err) }
```

**关键点**:
- `Start()` 非阻塞,gRPC 和 gateway 都在后台 goroutine 里跑;`Stop()` 设 health 为 NOT_SERVING、GracefulStop、cancel gateway ctx。
- 自动注册 gRPC health server(默认 SERVING)。
- `ErrorInterceptor`:把 `xerr.Category` 映射成 gRPC codes(NotFound → NotFound code, BadRequest → InvalidArgument 等)。
- 认证:`grpcx.GetUserIDFromCtx(ctx)` / `grpcx.BearerTokenFromCtx(ctx)`,需要前面的拦截器往 ctx 里塞值。

### gorx — 安全并发

```go
// 单个安全 goroutine(panic 自动 recover + slog)
gorx.GoSafe(func() { ... })

// 等待一组
g := gorx.NewRoutineGroup()
for _, item := range items {
    g.RunSafe(func() { process(item) })
}
g.Wait()

// 限制并发数
runner := gorx.NewTaskRunner(10) // 最多 10 并发
for _, task := range tasks {
    runner.Schedule(func() { do(task) })
}
runner.Wait()
```

**关键点**:
- `OnPanic` 是可覆盖的变量,改成发 Sentry 上报:`gorx.OnPanic = func(r any, stack []byte) { sentry.CaptureException(...); slog.Error(...) }`。
- `TaskRunner.Schedule` 在 limit 满时会阻塞——适合 producer 速率可控的场景。
- `WorkGroup`:同一 job 起 N 个 worker,典型消费者模型。

---

## 工具集

### xerr + xcodes — 结构化错误

```go
// 用预定义错误码
if user == nil {
    return xcodes.ErrNotFound.New("user not found")
}
return xcodes.ErrBadRequest.Wrapf(err, "invalid email %s", email)

// 调用方判断
var xerrErr *xerr.Error
if errors.As(err, &xerrErr) {
    slog.Error("biz error", "reason", xerrErr.Code().Reason(), "http", xerrErr.HTTPCode())
}

// HTTP handler 里
httpCode := xerr.Error.HTTPCode() // 404, 400, ...
```

**预定义 codes**(`xcodes` 包):`ErrBadRequest` (400) / `ErrUnauthorized` (401) / `ErrForbidden` (403) / `ErrNotFound` (404) / `ErrConflict` (409) / `ErrTooManyRequests` (429) / `ErrInternal` (500) / `ErrServiceUnavailable` (503)。

**业务自定义 code**:
```go
var ErrUserNotFound = xerr.New("USER_NOT_FOUND", xerr.CategoryNotFound, 404, "user not found")
```

**关键设计**:
- `Error.Is` 按 `Code.Reason` 比较(`errors.Is(err, xcodes.ErrNotFound)` 即使 message 不同也会匹配)。
- `Unwrap()` 返回 cause,兼容 `errors.Is` / `errors.As` 链。
- `grpcx.ErrorInterceptor` 会把 Category 映射到 gRPC codes。

### jsonx — 高性能 JSON

```go
b, _ := jsonx.Marshal(v)
err := jsonx.Unmarshal(b, &v)
s, _ := jsonx.MarshalString(v) // 避免 []byte → string 拷贝
err := jsonx.Decode(reader, &v)
```

底层 `bytedance/sonic`,在 x86 上比 `encoding/json` 快数倍。不支持的架构(如 ARM 老芯片)自动 fallback 到 `encoding/json`,所以**API 完全等价**,可以无脑替换。

### ptr — 指针工具

```go
v := ptr.Ref(42)        // *int
n := ptr.Deref(v)       // 42
n = ptr.Deref[*int](nil) // 0 — nil 安全
```

避免到处写 `i := 42; &i` 这种 boilerplate。

---

## 常见反模式 → 正确做法

| 反模式 | 正确做法 |
|--------|---------|
| 自己拼 PostgreSQL DSN | `dbx.New(&dbx.Config{...})` |
| `redis.NewClient(...)` 不 Ping | `redisx.New(cfg)`(自动 Ping) |
| `go func() { ... }()` 裸跑 | `gorx.GoSafe(fn)`(panic 自动 recover) |
| `fmt.Errorf("user %d not found", id)` | `xcodes.ErrNotFound.New(fmt.Sprintf("user %d", id))` |
| 自己实现固定窗口限流 | `ratelimit.NewRedisLimiter(client, cfg)` |
| 自己写 SetNX 分布式锁 | `redisx.NewLock(client, &LockConfig{...})` |
| `interface{}` | `any` |
| `fmt.Println` 调试 | `slog.Debug(...)` |
| `log.Fatal(err)` 在非 main 包 | 返回 error 给调用方 |
| 配置用 `mapstructure:""` | 删掉,configx 自动 snake_case 匹配 |
| main 里 `signal.Notify` 手写 | `signalx.Run(svc)` 或 `lifecycle.Manager.Run(ctx)` |

---

## 写新业务时的检查清单

写代码前自问:
1. 这个功能 go-common 有现成包吗?(看上面速查表)
2. Config 字段是用指针传入的吗?
3. Redis key 是不是 `<module>:<purpose>:<identifier>` 格式?
4. 错误是不是用 `xerr`/`xcodes` 包装?
5. 并发是不是用了 `gorx.GoSafe` 或 `RoutineGroup`?
6. 测试是不是用了 `redisx.NewTestClient` / `dbx.SetupTestDB`?
7. 配置加载是不是 `configx.Load` + `default:""` tag?
8. 服务入口是不是 `signalx.Run` / `lifecycle.Manager`?

每个 ✅ 都意味着少一次踩坑和 review 来回。

---

## 扩展库本身

如果 go-common 现有功能不够用,需要加新模块或新能力:

1. **新模块**:遵循现有结构 — `<pkg>/<pkg>.go` 定义 Config + New(),`<pkg>/_test.go` 覆盖 85%+,需要的 helper 放 `<pkg>/testhelpers.go`。
2. **文件内声明顺序**:遵循 golang-development skill §7 — 类型 → 构造函数 `New*` → 常量 → 变量 → 导出方法 → 导出函数 → 未导出方法 → 未导出函数。Config 子配置字段必须用指针(`*redisx.Config`、`*dbx.Config` 等),`New*` 构造函数也接 `*Config`(详见 §14)。
3. **第三方依赖**:先在 GitHub 找有没有官方 SDK,优先用官方。
