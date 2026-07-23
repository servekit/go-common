package captcha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Record stores a verification code with metadata.
type Record struct {
	CaptchaID   string `json:"captcha_id"`
	Code        string `json:"code"`
	Target      string `json:"target"`
	Purpose     string `json:"purpose"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"max_attempts"`
}

// VerifyResult is returned by Verify.
type VerifyResult struct {
	Matched           bool
	CaptchaID         string
	Target            string
	Purpose           string
	RemainingAttempts int // -1 if matched or expired
}

// Store is the interface for verification code storage.
type Store interface {
	// Set stores a captcha record keyed by purpose+channel+target with the given TTL.
	Set(ctx context.Context, purpose, channel, target string, record *Record, ttl time.Duration) error
	// Verify atomically checks the code, increments attempts on mismatch,
	// and deletes the record on success or max attempts exceeded.
	// captchaID is the binding token returned by Generate; pass "" to skip the
	// captchaID match check, otherwise the stored Record.CaptchaID must equal it.
	Verify(ctx context.Context, purpose, channel, target, code, captchaID string) (*VerifyResult, error)
}

// RedisStore is a Redis-backed verification code store.
type RedisStore struct {
	client *redis.Client
	prefix string
	verify *redis.Script
}

// NewRedisStore creates a Redis verification code store.
// prefix is used as the Redis key prefix, e.g., "captcha" -> key "captcha:<purpose>:<channel>:<target>".
func NewRedisStore(client *redis.Client, prefix string) *RedisStore {
	return &RedisStore{
		client: client,
		prefix: prefix,
		verify: redis.NewScript(`
			-- ARGV[1]: code (the verification code being checked)
			-- ARGV[2]: captchaID ("" skips binding check; otherwise must equal r.captcha_id)
			local data = redis.call('GET', KEYS[1])
			if not data then return nil end

			local r = cjson.decode(data)

			if r.attempts >= r.max_attempts then
				redis.call('DEL', KEYS[1])
				return cjson.encode({status="exhausted", captcha_id=r.captcha_id, target=r.target, purpose=r.purpose, remaining=0})
			end

			r.attempts = r.attempts + 1

			local idMatch = ARGV[2] == "" or ARGV[2] == r.captcha_id

			if idMatch and r.code == ARGV[1] then
				redis.call('DEL', KEYS[1])
				return cjson.encode({status="matched", captcha_id=r.captcha_id, target=r.target, purpose=r.purpose, remaining=-1})
			end

			local remaining = r.max_attempts - r.attempts
			if remaining <= 0 then
				redis.call('DEL', KEYS[1])
			else
				redis.call('SET', KEYS[1], cjson.encode(r), 'KEEPTTL')
			end

			return cjson.encode({status="mismatch", captcha_id=r.captcha_id, target=r.target, purpose=r.purpose, remaining=remaining})
		`),
	}
}

// Set stores the captcha record with the given TTL.
func (s *RedisStore) Set(ctx context.Context, purpose, channel, target string, record *Record, ttl time.Duration) error {
	data, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	return s.client.Set(ctx, s.key(purpose, channel, target), data, ttl).Err()
}

// Verify atomically checks the code against the stored record.
// If captchaID is non-empty, the stored Record.CaptchaID must equal it;
// otherwise the call returns mismatch even when the code is correct.
// An empty captchaID skips the binding check (legacy behavior).
func (s *RedisStore) Verify(ctx context.Context, purpose, channel, target, code, captchaID string) (*VerifyResult, error) {
	raw, err := s.verify.Run(ctx, s.client, []string{s.key(purpose, channel, target)}, code, captchaID).Text()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return &VerifyResult{RemainingAttempts: 0}, nil
		}
		return nil, fmt.Errorf("run verify script: %w", err)
	}

	var result struct {
		Status    string `json:"status"`
		CaptchaID string `json:"captcha_id"`
		Target    string `json:"target"`
		Purpose   string `json:"purpose"`
		Remaining int    `json:"remaining"`
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("unmarshal verify result: %w", err)
	}

	switch result.Status {
	case "matched":
		return &VerifyResult{
			Matched:           true,
			CaptchaID:         result.CaptchaID,
			Target:            result.Target,
			Purpose:           result.Purpose,
			RemainingAttempts: -1,
		}, nil
	case "mismatch":
		return &VerifyResult{
			Matched:           false,
			CaptchaID:         result.CaptchaID,
			Target:            result.Target,
			Purpose:           result.Purpose,
			RemainingAttempts: result.Remaining,
		}, nil
	case "exhausted":
		return &VerifyResult{
			Matched:           false,
			CaptchaID:         result.CaptchaID,
			Target:            result.Target,
			Purpose:           result.Purpose,
			RemainingAttempts: 0,
		}, nil
	default:
		return nil, fmt.Errorf("unexpected verify status: %s", result.Status)
	}
}

func (s *RedisStore) key(purpose, channel, target string) string {
	return fmt.Sprintf("%s:%s:%s:%s", s.prefix, purpose, channel, target)
}
