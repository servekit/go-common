# go-common/message 设计文档

> 独立的消息发送模块，支持 email 和 SMS 两种渠道，每种渠道支持多个 provider。
> 不依赖任何业务服务，可被验证码模块、通知系统等任何需要发送消息的场景复用。

## 1. 模块定位

位于 `go-common/message`。通用消息发送，与业务解耦。上层通过 `Messenger.Send` 发送消息，不关心具体 provider。

## 2. 核心接口

```go
// go-common/message/message.go
package message

type Message struct {
    To       string            // 收件人：邮箱地址 / 手机号
    Subject  string            // 主题（email 用）
    Body     string            // 正文内容
    Template string            // 模板 ID（可选，provider 支持时使用）
    Data     map[string]string // 模板变量（可选）
}

// Messenger 消息发送接口。
type Messenger struct {
    providers map[string][]Provider  // channel → providers（按优先级排序）
}

// Send 通过指定渠道发送消息。自动选择 provider，失败时 fallback 到下一个。
func (m *Messenger) Send(ctx context.Context, channel string, msg *Message) error
```

## 3. Provider 接口

```go
// go-common/message/provider.go

type Provider interface {
    // Channel 返回渠道类型："email" / "sms"
    Channel() string

    // Name 返回 provider 名称，如 "smtp", "mailgun", "twilio", "aliyun"
    Name() string

    // Send 发送消息。返回 nil 表示成功。
    Send(ctx context.Context, msg *Message) error
}
```

## 4. Provider 实现与配置

**Email Providers：**

| Provider | 说明 | 配置项 |
|----------|------|--------|
| SMTP | 通用 SMTP 发送 | host, port, username, password, from |
| Mailgun | Mailgun HTTP API | domain, api_key, from |

**SMS Providers：**

| Provider | 说明 | 配置项 |
|----------|------|--------|
| Aliyun | 阿里云短信 | access_key, secret_key, sign_name, template_code |
| Tencent | 腾讯云短信 | secret_id, secret_key, sdk_app_id, sign_name |
| Twilio | Twilio SMS | account_sid, auth_token, from_number |

配置方式（config.yaml）：

```yaml
message:
  email:
    default: smtp                # 默认 provider
    providers:
      smtp:
        host: smtp.example.com
        port: 587
        username: xxx
        password: xxx
        from: "noreply@example.com"
      mailgun:
        domain: example.com
        api_key: xxx
        from: "noreply@example.com"
  sms:
    default: aliyun              # 默认 provider
    providers:
      aliyun:
        access_key: xxx
        secret_key: xxx
        sign_name: "我的应用"
        templates:
          register: "SMS_123456"
          login: "SMS_123457"
          password_reset: "SMS_123458"
      tencent:
        secret_id: xxx
        secret_key: xxx
        sdk_app_id: xxx
        sign_name: "我的应用"
      twilio:
        account_sid: xxx
        auth_token: xxx
        from_number: "+1234567890"
```

## 5. Fallback 机制

同一渠道配置多个 provider 时，按配置顺序优先级递减：

```
Send("email", msg):
  1. 尝试 smtp.Send(msg)
  2. 失败 → 尝试 mailgun.Send(msg)
  3. 全部失败 → 返回最后一个错误
```

## 6. 模块结构

```
go-common/message/
├── message.go          # Messenger 核心 + Fallback 逻辑
├── message_test.go
├── provider.go         # Provider 接口定义
├── email/
│   ├── smtp.go         # SMTP 实现
│   ├── smtp_test.go
│   └── mailgun.go      # Mailgun 实现
└── sms/
    ├── aliyun.go       # 阿里云 SMS
    ├── tencent.go      # 腾讯云 SMS
    └── twilio.go       # Twilio SMS
```
