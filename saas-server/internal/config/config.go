package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr        string
	WebDir          string
	DatabaseURL     string
	JWTSecret       []byte
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	AllowedOrigins  map[string]struct{}
	RateLimit       RateLimitConfig
}

type RateLimitConfig struct {
	Disabled     bool
	Window       time.Duration
	AuthRequests int
	APIRequests  int
	MaxKeys      int
}

func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		Window: time.Minute, AuthRequests: 30, APIRequests: 300, MaxKeys: 10000,
	}
}

func Load() (Config, error) {
	databaseURL, err := LoadDatabaseURL()
	if err != nil {
		return Config{}, err
	}

	jwtSecret := []byte(os.Getenv("SAAS_JWT_SECRET"))
	if len(jwtSecret) < 32 {
		return Config{}, errors.New("SAAS_JWT_SECRET 长度不能少于 32 字节")
	}

	accessTokenTTL, err := durationFromEnv("SAAS_ACCESS_TOKEN_TTL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	refreshTokenTTL, err := durationFromEnv("SAAS_REFRESH_TOKEN_TTL", 30*24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	rateLimit, err := loadRateLimitConfig()
	if err != nil {
		return Config{}, err
	}

	return Config{
		HTTPAddr:        valueOrDefault("SAAS_HTTP_ADDR", "127.0.0.1:8787"),
		WebDir:          strings.TrimSpace(os.Getenv("SAAS_WEB_DIR")),
		DatabaseURL:     databaseURL,
		JWTSecret:       jwtSecret,
		AccessTokenTTL:  accessTokenTTL,
		RefreshTokenTTL: refreshTokenTTL,
		AllowedOrigins:  parseOrigins(os.Getenv("SAAS_ALLOWED_ORIGINS")),
		RateLimit:       rateLimit,
	}, nil
}

func loadRateLimitConfig() (RateLimitConfig, error) {
	cfg := DefaultRateLimitConfig()
	if value := strings.TrimSpace(os.Getenv("SAAS_RATE_LIMIT_ENABLED")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return RateLimitConfig{}, errors.New("SAAS_RATE_LIMIT_ENABLED 配置无效")
		}
		cfg.Disabled = !enabled
	}
	window, err := durationFromEnv("SAAS_RATE_LIMIT_WINDOW", cfg.Window)
	if err != nil || window > 24*time.Hour {
		return RateLimitConfig{}, errors.New("SAAS_RATE_LIMIT_WINDOW 配置无效，须大于零且不超过 24 小时")
	}
	cfg.Window = window
	for _, setting := range []struct {
		name   string
		target *int
	}{
		{"SAAS_RATE_LIMIT_AUTH_REQUESTS", &cfg.AuthRequests},
		{"SAAS_RATE_LIMIT_API_REQUESTS", &cfg.APIRequests},
		{"SAAS_RATE_LIMIT_MAX_KEYS", &cfg.MaxKeys},
	} {
		value := strings.TrimSpace(os.Getenv(setting.name))
		if value == "" {
			continue
		}
		number, err := strconv.Atoi(value)
		if err != nil || number < 1 || number > 1000000 {
			return RateLimitConfig{}, fmt.Errorf("%s 配置无效，须为 1 至 1000000 的整数", setting.name)
		}
		*setting.target = number
	}
	return cfg, nil
}

func LoadDatabaseURL() (string, error) {
	databaseURL := strings.TrimSpace(os.Getenv("SAAS_DATABASE_URL"))
	if databaseURL == "" {
		return "", errors.New("SAAS_DATABASE_URL 未配置")
	}
	return databaseURL, nil
}

func durationFromEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s 配置无效", name)
	}
	return duration, nil
}

func valueOrDefault(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func parseOrigins(value string) map[string]struct{} {
	origins := make(map[string]struct{})
	for _, origin := range strings.Split(value, ",") {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return origins
}

func ParsePageSize(value string) (int, error) {
	if value == "" {
		return 50, nil
	}
	size, err := strconv.Atoi(value)
	if err != nil || size < 1 || size > 200 {
		return 0, errors.New("page_size 配置无效")
	}
	return size, nil
}
