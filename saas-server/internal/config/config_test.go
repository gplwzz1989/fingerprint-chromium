package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadRequiresDatabaseAndStrongJWTSecret(t *testing.T) {
	t.Setenv("SAAS_DATABASE_URL", "")
	t.Setenv("SAAS_JWT_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted missing database configuration")
	}

	t.Setenv("SAAS_DATABASE_URL", "postgres://configured")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted a weak JWT secret")
	}
}

func TestLoadDatabaseURL(t *testing.T) {
	t.Setenv("SAAS_DATABASE_URL", "  postgres://configured  ")
	value, err := LoadDatabaseURL()
	if err != nil || value != "postgres://configured" {
		t.Fatalf("LoadDatabaseURL() = (%q, %v), want trimmed database URL", value, err)
	}
}

func TestParsePageSize(t *testing.T) {
	if size, err := ParsePageSize(""); err != nil || size != 50 {
		t.Fatalf("ParsePageSize(empty) = (%d, %v), want (50, nil)", size, err)
	}
	if _, err := ParsePageSize("201"); err == nil {
		t.Fatal("ParsePageSize() accepted a size above the limit")
	}
}

func setRateLimitTestEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SAAS_DATABASE_URL", "postgres://configured")
	t.Setenv("SAAS_JWT_SECRET", strings.Repeat("s", 32))
	t.Setenv("SAAS_ACCESS_TOKEN_TTL", "")
	t.Setenv("SAAS_REFRESH_TOKEN_TTL", "")
	for _, name := range []string{
		"SAAS_RATE_LIMIT_ENABLED", "SAAS_RATE_LIMIT_WINDOW",
		"SAAS_RATE_LIMIT_AUTH_REQUESTS", "SAAS_RATE_LIMIT_API_REQUESTS", "SAAS_RATE_LIMIT_MAX_KEYS",
	} {
		t.Setenv(name, "")
	}
}

func TestLoadRateLimitDefaults(t *testing.T) {
	setRateLimitTestEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RateLimit != DefaultRateLimitConfig() || cfg.RateLimit.Disabled {
		t.Fatalf("限流默认配置无效：%+v", cfg.RateLimit)
	}
}

func TestLoadRateLimitOverrides(t *testing.T) {
	setRateLimitTestEnv(t)
	t.Setenv("SAAS_RATE_LIMIT_ENABLED", " false ")
	t.Setenv("SAAS_RATE_LIMIT_WINDOW", " 2s ")
	t.Setenv("SAAS_RATE_LIMIT_AUTH_REQUESTS", " 3 ")
	t.Setenv("SAAS_RATE_LIMIT_API_REQUESTS", " 12 ")
	t.Setenv("SAAS_RATE_LIMIT_MAX_KEYS", " 50 ")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := RateLimitConfig{Disabled: true, Window: 2 * time.Second, AuthRequests: 3, APIRequests: 12, MaxKeys: 50}
	if cfg.RateLimit != want {
		t.Fatalf("限流配置为 %+v，预期 %+v", cfg.RateLimit, want)
	}
	t.Setenv("SAAS_RATE_LIMIT_ENABLED", "true")
	cfg, err = Load()
	if err != nil || cfg.RateLimit.Disabled {
		t.Fatalf("启用限流失败：%+v，%v", cfg.RateLimit, err)
	}
}

func TestLoadRateLimitRejectsInvalidSettings(t *testing.T) {
	for _, test := range []struct{ name, value string }{
		{"SAAS_RATE_LIMIT_ENABLED", "invalid"},
		{"SAAS_RATE_LIMIT_WINDOW", "invalid"},
		{"SAAS_RATE_LIMIT_WINDOW", "0s"},
		{"SAAS_RATE_LIMIT_WINDOW", "-1s"},
		{"SAAS_RATE_LIMIT_WINDOW", "25h"},
		{"SAAS_RATE_LIMIT_AUTH_REQUESTS", "0"},
		{"SAAS_RATE_LIMIT_AUTH_REQUESTS", "-1"},
		{"SAAS_RATE_LIMIT_AUTH_REQUESTS", "1.5"},
		{"SAAS_RATE_LIMIT_API_REQUESTS", "invalid"},
		{"SAAS_RATE_LIMIT_API_REQUESTS", "1000001"},
		{"SAAS_RATE_LIMIT_MAX_KEYS", "0"},
		{"SAAS_RATE_LIMIT_MAX_KEYS", "-1"},
		{"SAAS_RATE_LIMIT_MAX_KEYS", "1000001"},
		{"SAAS_RATE_LIMIT_MAX_KEYS", "9999999999999999999999999"},
	} {
		t.Run(test.name+"/"+test.value, func(t *testing.T) {
			setRateLimitTestEnv(t)
			t.Setenv(test.name, test.value)
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), test.name) || !strings.Contains(err.Error(), "配置无效") {
				t.Fatalf("无效配置应返回中文错误并指明变量：%v", err)
			}
		})
	}
}

func TestLoadRateLimitAcceptsUpperBounds(t *testing.T) {
	setRateLimitTestEnv(t)
	t.Setenv("SAAS_RATE_LIMIT_WINDOW", "24h")
	t.Setenv("SAAS_RATE_LIMIT_AUTH_REQUESTS", "1000000")
	t.Setenv("SAAS_RATE_LIMIT_API_REQUESTS", "1000000")
	t.Setenv("SAAS_RATE_LIMIT_MAX_KEYS", "1000000")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}
