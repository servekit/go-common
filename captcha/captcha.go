// Package captcha provides verification code generation, storage, rate limiting, and verification.
package captcha

import (
	"context"
	"fmt"
	"maps"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/servekit/go-common/ratelimit"
	"github.com/servekit/go-common/redisx"
)

// Config holds all captcha service configuration.
type Config struct {
	Prefix      string                    `default:"captcha"` // Redis key prefix
	Redis       *redisx.Config            // Redis connection info, required if no WithRedisClient option
	MaxAttempts int                       `default:"3"` // max verify attempts per captcha
	GlobalRules []*ratelimit.Rule         // global rate limits applied across all purposes
	Purposes    map[string]*PurposeConfig // purpose -> config
}

// PurposeConfig groups all settings for a single purpose.
type PurposeConfig struct {
	CodeFormat *CodeFormat       // default FormatDigit6
	RateRules  []*ratelimit.Rule // rate limit rules, shortest window = TTL
}

// Captcha is the verification code service.
type Captcha struct {
	client      *redis.Client
	store       Store
	limiter     ratelimit.Limiter
	codeGen     *CodeGenerator
	purposes    map[string]*PurposeConfig
	maxAttempts int
	prefix      string
}

// Option configures a Captcha instance.
type Option func(*Captcha)

// GenerateOption configures a Generate call.
type GenerateOption func(*generateConfig)

// VerifyOption configures a Verify call.
type VerifyOption func(*verifyConfig)

// SendFunc is a callback invoked by Generate after the code is stored.
// The caller typically uses it to deliver the code to the target.
type SendFunc func(ctx context.Context, target, code, purpose, channel string) error

// generateConfig groups Generate call options.
type generateConfig struct {
	send SendFunc
}

// verifyConfig groups Verify call options.
type verifyConfig struct {
	captchaID string
}

// New creates a Captcha service from the given configuration.
// If no Redis client is provided via WithRedisClient option, cfg.Redis is used
// to create a new connection. Returns an error if neither is available.
func New(cfg *Config, opts ...Option) (*Captcha, error) {
	c := &Captcha{
		prefix:      cfg.Prefix,
		maxAttempts: cfg.MaxAttempts,
		purposes:    make(map[string]*PurposeConfig),
	}

	// Apply options (may set client via WithRedisClient).
	for _, opt := range opts {
		opt(c)
	}

	// If no client from options, create one from cfg.Redis.
	if c.client == nil {
		if cfg.Redis == nil || cfg.Redis.Addr == "" {
			return nil, fmt.Errorf("captcha: redis addr is required when no client is provided")
		}
		client, err := redisx.New(cfg.Redis)
		if err != nil {
			return nil, fmt.Errorf("captcha: %w", err)
		}
		c.client = client
	}

	// Copy purposes.
	maps.Copy(c.purposes, cfg.Purposes)

	// Build rate limiter config from all purpose rules + global rules.
	limiterCfg := ratelimit.Config{
		Prefix: c.prefix + ":rate",
		Global: cfg.GlobalRules,
		Rules:  make(map[string][]*ratelimit.Rule),
	}
	for purpose, pc := range c.purposes {
		if len(pc.RateRules) > 0 {
			limiterCfg.Rules[purpose] = pc.RateRules
		}
	}
	if len(limiterCfg.Global) > 0 || len(limiterCfg.Rules) > 0 {
		c.limiter = ratelimit.NewRedisLimiter(c.client, &limiterCfg)
	}

	// Build code generator from all purpose formats.
	formats := make(map[string]*CodeFormat)
	for purpose, pc := range c.purposes {
		formats[purpose] = pc.CodeFormat
	}
	c.codeGen = NewCodeGenerator(formats)

	// Create Redis store.
	c.store = NewRedisStore(c.client, c.prefix)

	return c, nil
}

// WithRedisClient provides an existing Redis client instance.
func WithRedisClient(client *redis.Client) Option {
	return func(c *Captcha) { c.client = client }
}

// WithSend returns a GenerateOption that calls fn after the code is generated and stored.
// If fn returns an error, Generate returns it wrapped as "send code: <error>".
func WithSend(fn SendFunc) GenerateOption {
	return func(c *generateConfig) { c.send = fn }
}

// WithCaptchaID binds the Verify call to the captchaID returned by Generate.
// If id is empty or the option is omitted, the captchaID check is skipped
// and Verify behaves as it did before this option existed.
//
// Use this to defend against cross-context code-reuse attacks: a code
// generated in one session (e.g., a browser flow) cannot be verified in
// another session (e.g., a mobile app flow) without the matching captchaID.
func WithCaptchaID(id string) VerifyOption {
	return func(c *verifyConfig) { c.captchaID = id }
}

// defaultTTL is the code validity duration when no rate rules are configured.
const defaultTTL = 5 * time.Minute

// Generate creates a verification code, stores it, and returns the captchaID and plain-text code.
// channel is a free-form string used as part of the Redis key to distinguish delivery contexts
// (e.g., "sms", "email") — it prevents key collisions for the same target+purpose.
// Use WithSend option to invoke a callback (e.g., deliver the code) after generation.
func (c *Captcha) Generate(ctx context.Context, target, purpose, channel string, opts ...GenerateOption) (captchaID, code string, err error) {
	var gc generateConfig
	for _, opt := range opts {
		opt(&gc)
	}

	if target == "" {
		return "", "", fmt.Errorf("captcha: target is empty")
	}
	if purpose == "" {
		return "", "", fmt.Errorf("captcha: purpose is empty")
	}
	if channel == "" {
		return "", "", fmt.Errorf("captcha: channel is empty")
	}

	// Look up purpose config.
	if _, ok := c.purposes[purpose]; !ok {
		return "", "", fmt.Errorf("captcha: purpose %q not configured", purpose)
	}

	// Rate limit check.
	if c.limiter != nil {
		ok, err := c.limiter.Allow(ctx, purpose, target)
		if err != nil {
			return "", "", fmt.Errorf("check rate limit: %w", err)
		}
		if !ok {
			return "", "", fmt.Errorf("rate limit exceeded for %s", target)
		}
	}

	// Generate code.
	code, err = c.codeGen.Generate(purpose)
	if err != nil {
		return "", "", fmt.Errorf("generate code: %w", err)
	}

	ttl := c.ttlForPurpose(purpose)
	captchaID = uuid.New().String()

	record := &Record{
		CaptchaID:   captchaID,
		Code:        code,
		Target:      target,
		Purpose:     purpose,
		MaxAttempts: c.maxAttempts,
	}

	if err := c.store.Set(ctx, purpose, channel, target, record, ttl); err != nil {
		return "", "", fmt.Errorf("store code: %w", err)
	}

	// Invoke send hook if provided.
	if gc.send != nil {
		if err := gc.send(ctx, target, code, purpose, channel); err != nil {
			return captchaID, code, fmt.Errorf("send code: %w", err)
		}
	}

	return captchaID, code, nil
}

// Verify checks a verification code by target, purpose, and channel.
// Pass WithCaptchaID to additionally require the captchaID returned by the
// originating Generate call; otherwise the check is skipped.
//
// Example:
//
//	result, err := c.Verify(ctx, phone, code, "login", "sms", WithCaptchaID(id))
func (c *Captcha) Verify(
	ctx context.Context, target, code, purpose, channel string,
	opts ...VerifyOption,
) (*VerifyResult, error) {
	var vc verifyConfig
	for _, opt := range opts {
		opt(&vc)
	}
	return c.store.Verify(ctx, purpose, channel, target, code, vc.captchaID)
}

// ttlForPurpose returns the code validity duration for the given purpose.
// The TTL is derived from the shortest rate-limit window.
// If no rules are configured, defaultTTL is returned.
func (c *Captcha) ttlForPurpose(purpose string) time.Duration {
	pc, ok := c.purposes[purpose]
	if !ok || len(pc.RateRules) == 0 {
		return defaultTTL
	}
	shortest := pc.RateRules[0].Window
	for _, r := range pc.RateRules[1:] {
		if r.Window < shortest {
			shortest = r.Window
		}
	}
	return shortest
}
