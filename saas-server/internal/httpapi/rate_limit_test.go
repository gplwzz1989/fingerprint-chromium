package httpapi

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/auth"
	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/config"
)

func rateLimitTestServer() *Server {
	return &Server{cfg: config.Config{
		JWTSecret: []byte(strings.Repeat("s", 32)),
		RateLimit: config.RateLimitConfig{
			Window: time.Minute, AuthRequests: 2, APIRequests: 2, MaxKeys: 100,
		},
	}}
}

func rateLimitTestHandler(s *Server) http.Handler {
	return s.withRateLimit(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
}

func rateLimitTestRequest(handler http.Handler, method, path, remoteAddr, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.RemoteAddr = remoteAddr
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func rateLimitTestToken(t *testing.T, s *Server, userID, sessionID string) string {
	t.Helper()
	token, err := auth.IssueAccessToken(s.cfg.JWTSecret, userID, sessionID, "测试设备", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestRateLimitAuthUsesRemoteIPAcrossEndpoints(t *testing.T) {
	s := rateLimitTestServer()
	s.cfg.RateLimit.AuthRequests = 1
	for _, endpoint := range []string{
		"/api/v1/sessions", "/api/v1/sessions/refresh", "/api/v1/sessions/revoke", "/api/v1/invitations/accept",
	} {
		t.Run(endpoint, func(t *testing.T) {
			handler := rateLimitTestHandler(s)
			first := rateLimitTestRequest(handler, http.MethodPost, endpoint, "192.0.2.1:1000", rateLimitTestToken(t, s, "用户一", "会话一"))
			if first.Code != http.StatusNoContent {
				t.Fatalf("首个鉴权请求被拒绝：%d", first.Code)
			}
			for i, path := range []string{"/api/v1/sessions", "/api/v1/sessions/refresh", "/api/v1/sessions/revoke", "/api/v1/invitations/accept"} {
				r := httptest.NewRequest(http.MethodPost, path, nil)
				r.RemoteAddr = fmt.Sprintf("192.0.2.1:%d", 2000+i)
				r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
				r.Header.Set("X-Real-IP", fmt.Sprintf("198.51.100.%d", i+1))
				r.Header.Set("Forwarded", fmt.Sprintf("for=198.51.100.%d", i+1))
				r.Header.Set("Authorization", "Bearer "+rateLimitTestToken(t, s, fmt.Sprintf("其他用户%d", i), "其他会话"))
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				if w.Code != http.StatusTooManyRequests {
					t.Fatalf("轮换端点、端口、转发头或合法令牌绕过限流：%d", w.Code)
				}
				var response errorResponse
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Code != "rate_limited" || response.Message != "请求过于频繁，请稍后重试" {
					t.Fatalf("限流响应无效：%s，%v", w.Body.String(), err)
				}
				seconds, err := strconv.Atoi(w.Header().Get("Retry-After"))
				if err != nil || seconds < 1 || seconds > 60 {
					t.Fatalf("Retry-After 无效：%q", w.Header().Get("Retry-After"))
				}
				if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
					t.Fatal("限流错误未声明 UTF-8 JSON")
				}
			}
			if w := rateLimitTestRequest(handler, http.MethodPost, endpoint, "192.0.2.2:1000", ""); w.Code != http.StatusNoContent {
				t.Fatalf("不同真实 IP 的配额应独立：%d", w.Code)
			}
		})
	}
}

func TestRateLimitAPIUsesVerifiedUserAcrossSessionsAndIPs(t *testing.T) {
	s := rateLimitTestServer()
	handler := rateLimitTestHandler(s)
	for i, endpoint := range []string{"/api/v1/workspaces", "/api/v1/accounts/a/snapshot", "/api/v1/sessions"} {
		token := rateLimitTestToken(t, s, "同一用户", fmt.Sprintf("会话%d", i))
		w := rateLimitTestRequest(handler, http.MethodGet, endpoint, fmt.Sprintf("192.0.2.%d:1000", i+1), token)
		want := http.StatusNoContent
		if i == 2 {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("用户请求 %d 状态为 %d，预期 %d", i, w.Code, want)
		}
	}
	otherToken := rateLimitTestToken(t, s, "其他用户", "其他会话")
	if w := rateLimitTestRequest(handler, http.MethodGet, "/api/v1/workspaces", "192.0.2.1:1000", otherToken); w.Code != http.StatusNoContent {
		t.Fatalf("共享 IP 的不同用户配额应独立：%d", w.Code)
	}
	if w := rateLimitTestRequest(handler, http.MethodPost, "/api/v1/sessions", "192.0.2.1:1000", ""); w.Code != http.StatusNoContent {
		t.Fatalf("API 配额不应占用鉴权配额：%d", w.Code)
	}
}

func TestRateLimitInvalidTokensAndForgedHeadersFallBackToIP(t *testing.T) {
	s := rateLimitTestServer()
	wrongSignature, err := auth.IssueAccessToken([]byte(strings.Repeat("x", 32)), "合法用户", "会话", "设备", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expired, err := auth.IssueAccessToken(s.cfg.JWTSecret, "合法用户", "会话", "设备", -time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." +
		base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"合法用户","jti":"会话","iss":"fingerprint-saas"}`)) + "."
	for _, token := range []string{"", "invalid", wrongSignature, expired, unsigned, strings.Repeat("x", 8193)} {
		t.Run(fmt.Sprintf("令牌长度%d/%x", len(token), sha256.Sum256([]byte(token))), func(t *testing.T) {
			handler := rateLimitTestHandler(s)
			for i := 0; i < 3; i++ {
				r := httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil)
				r.RemoteAddr = fmt.Sprintf("192.0.2.1:%d", 1000+i)
				r.Header.Set("Authorization", "Bearer "+token)
				r.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				want := http.StatusNoContent
				if i == 2 {
					want = http.StatusTooManyRequests
				}
				if w.Code != want {
					t.Fatalf("无效令牌请求 %d 状态为 %d，预期 %d", i, w.Code, want)
				}
			}
			valid := rateLimitTestToken(t, s, "合法用户", "合法会话")
			if w := rateLimitTestRequest(handler, http.MethodGet, "/api/v1/workspaces", "192.0.2.1:1000", valid); w.Code != http.StatusNoContent {
				t.Fatalf("伪造或过期令牌不应消耗合法用户配额：%d", w.Code)
			}
		})
	}
}

func TestRateLimitRemoteIPNormalization(t *testing.T) {
	for _, test := range []struct{ remote, want string }{
		{"192.0.2.1:1000", "192.0.2.1"},
		{"192.0.2.1:2000", "192.0.2.1"},
		{"192.0.2.1", "192.0.2.1"},
		{"[::ffff:192.0.2.1]:1000", "192.0.2.1"},
		{"[2001:db8:0:0::1]:1000", "2001:db8::1"},
		{"2001:db8::1", "2001:db8::1"},
		{"[fe80::1%eth0]:1000", "fe80::1"},
		{"", "unknown"},
		{"invalid:1000", "unknown"},
	} {
		if got := rateLimitRemoteIP(test.remote); got != test.want {
			t.Errorf("来源 %q 归一化为 %q，预期 %q", test.remote, got, test.want)
		}
	}
	s := rateLimitTestServer()
	s.cfg.RateLimit.APIRequests = 1
	handler := rateLimitTestHandler(s)
	rateLimitTestRequest(handler, http.MethodGet, "/api", "192.0.2.1:1000", "")
	if w := rateLimitTestRequest(handler, http.MethodGet, "/api", "[::ffff:192.0.2.1]:2000", ""); w.Code != http.StatusTooManyRequests {
		t.Fatalf("IPv4 映射地址绕过限流：%d", w.Code)
	}
}

func TestRateLimitSkipsHealthStaticAndPreflight(t *testing.T) {
	s := rateLimitTestServer()
	s.cfg.RateLimit = config.RateLimitConfig{Window: time.Minute, AuthRequests: 1, APIRequests: 1, MaxKeys: 2}
	handler := rateLimitTestHandler(s)
	for _, request := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/sessions"},
		{http.MethodGet, "/api/v1/workspaces"},
	} {
		if w := rateLimitTestRequest(handler, request.method, request.path, "192.0.2.1:1000", ""); w.Code != http.StatusNoContent {
			t.Fatalf("建立初始配额失败：%d", w.Code)
		}
	}
	for i := 0; i < 10; i++ {
		for _, request := range []struct{ method, path string }{
			{http.MethodGet, "/healthz"}, {http.MethodHead, "/healthz"},
			{http.MethodGet, "/"}, {http.MethodGet, "/styles.css"}, {http.MethodGet, "/api.js"},
			{http.MethodGet, "/console/accounts"}, {http.MethodOptions, "/api"},
			{http.MethodOptions, "/api/v1/sessions"}, {http.MethodOptions, "/api/v1/accounts/a"},
		} {
			if w := rateLimitTestRequest(handler, request.method, request.path, "192.0.2.1:1000", ""); w.Code != http.StatusNoContent || w.Header().Get("Retry-After") != "" {
				t.Fatalf("豁免请求 %s %s 被限流：%d", request.method, request.path, w.Code)
			}
		}
	}
	if w := rateLimitTestRequest(handler, http.MethodGet, "/api/v1/workspaces", "192.0.2.1:1000", ""); w.Code != http.StatusTooManyRequests {
		t.Fatalf("豁免请求不应重置已消耗的配额：%d", w.Code)
	}
}

func TestRateLimitRecoveryAndRejectedRequestsDoNotExtendWindow(t *testing.T) {
	now := time.Unix(100, 0)
	l := newRateLimiter(config.RateLimitConfig{Window: 1500 * time.Millisecond})
	l.now = func() time.Time { return now }
	for i := 0; i < 2; i++ {
		if allowed, _ := l.allow("用户", 2); !allowed {
			t.Fatal("配额内的请求被拒绝")
		}
	}
	now = now.Add(250 * time.Millisecond)
	if allowed, wait := l.allow("用户", 2); allowed || wait != 1250*time.Millisecond {
		t.Fatalf("超限结果为 %t/%s", allowed, wait)
	}
	now = now.Add(1250*time.Millisecond - time.Nanosecond)
	if allowed, wait := l.allow("用户", 2); allowed || wait != time.Nanosecond {
		t.Fatalf("到期前请求为 %t/%s", allowed, wait)
	}
	now = now.Add(time.Nanosecond)
	if allowed, wait := l.allow("用户", 2); !allowed || wait != 0 {
		t.Fatalf("到期时未恢复配额：%t/%s", allowed, wait)
	}
	if len(l.entries) != 1 || len(l.expiry) != 1 {
		t.Fatalf("恢复后保留了重复条目：%d/%d", len(l.entries), len(l.expiry))
	}
}

func TestRateLimitCapacityPreservesExistingLimitsAndReclaimsExpiredEntries(t *testing.T) {
	now := time.Unix(100, 0)
	l := newRateLimiter(config.RateLimitConfig{Window: time.Second, MaxKeys: 2})
	l.now = func() time.Time { return now }
	l.allow("首个用户", 1)
	now = now.Add(500 * time.Millisecond)
	l.allow("第二个用户", 2)
	for i := 0; i < 1000; i++ {
		if allowed, wait := l.allow(fmt.Sprintf("新用户%d", i), 1); allowed || wait != 500*time.Millisecond {
			t.Fatalf("容量耗尽未拒绝新标识或等待时间错误：%t/%s", allowed, wait)
		}
	}
	if allowed, _ := l.allow("首个用户", 1); allowed {
		t.Fatal("容量攻击淘汰了已有计数")
	}
	if allowed, _ := l.allow("第二个用户", 2); !allowed {
		t.Fatal("容量耗尽错误地拒绝了已有用户剩余配额")
	}
	if len(l.entries) != 2 || len(l.expiry) != 2 {
		t.Fatalf("容量未受约束：%d/%d", len(l.entries), len(l.expiry))
	}
	now = now.Add(500 * time.Millisecond)
	if allowed, _ := l.allow("第三个用户", 1); !allowed {
		t.Fatal("最早条目过期后未释放容量")
	}
	if allowed, _ := l.allow("第二个用户", 2); allowed {
		t.Fatal("回收最早条目错误地重置了未过期用户的计数")
	}
	now = now.Add(2 * time.Second)
	if allowed, _ := l.allow("第四个用户", 1); !allowed || len(l.entries) != 1 || len(l.expiry) != 1 {
		t.Fatalf("过期条目未完全回收：%t/%d/%d", allowed, len(l.entries), len(l.expiry))
	}
}

func TestRateLimitConcurrentRequestsAndCapacity(t *testing.T) {
	for _, test := range []struct {
		name       string
		distinct   bool
		limit      int
		maxKeys    int
		wantAccept int64
		wantKeys   int
	}{
		{"同一标识", false, 17, 32, 17, 1},
		{"不同标识", true, 1, 32, 32, 32},
	} {
		t.Run(test.name, func(t *testing.T) {
			l := newRateLimiter(config.RateLimitConfig{Window: time.Minute, MaxKeys: test.maxKeys})
			l.now = func() time.Time { return time.Unix(100, 0) }
			var accepted atomic.Int64
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := 0; i < 512; i++ {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					<-start
					identity := "同一用户"
					if test.distinct {
						identity = fmt.Sprintf("用户%d", i)
					}
					if allowed, _ := l.allow(identity, test.limit); allowed {
						accepted.Add(1)
					}
				}(i)
			}
			close(start)
			wg.Wait()
			if accepted.Load() != test.wantAccept || len(l.entries) != test.wantKeys || len(l.expiry) != test.wantKeys {
				t.Fatalf("并发结果为 %d/%d/%d，预期 %d/%d/%d", accepted.Load(), len(l.entries), len(l.expiry), test.wantAccept, test.wantKeys, test.wantKeys)
			}
		})
	}
}

func TestRateLimitHandlerIntegrationAndDisable(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("关闭=%t", disabled), func(t *testing.T) {
			s := rateLimitTestServer()
			s.cfg.RateLimit.Disabled = disabled
			s.cfg.RateLimit.AuthRequests = 1
			s.cfg.RateLimit.Window = 1500 * time.Millisecond
			s.cfg.AllowedOrigins = map[string]struct{}{"https://manager.example": {}}
			handler := s.Handler()
			for i := 0; i < 2; i++ {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader("{}"))
				r.Header.Set("Origin", "https://manager.example")
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, r)
				want := http.StatusBadRequest
				if i == 1 && !disabled {
					want = http.StatusTooManyRequests
					if w.Header().Get("Retry-After") != "2" {
						t.Fatalf("不足整数秒的等待时间未向上取整：%q", w.Header().Get("Retry-After"))
					}
				}
				if w.Code != want || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != "https://manager.example" {
					t.Fatalf("完整处理链响应无效：%d，%v", w.Code, w.Header())
				}
			}
		})
	}
}

func TestRateLimitZeroConfigUsesDefaults(t *testing.T) {
	s := &Server{}
	handler := rateLimitTestHandler(s)
	for i := 0; i < config.DefaultRateLimitConfig().AuthRequests+1; i++ {
		w := rateLimitTestRequest(handler, http.MethodPost, "/api/v1/sessions", "192.0.2.1:1000", "")
		want := http.StatusNoContent
		if i == config.DefaultRateLimitConfig().AuthRequests {
			want = http.StatusTooManyRequests
		}
		if w.Code != want {
			t.Fatalf("零配置请求 %d 状态为 %d，预期 %d", i, w.Code, want)
		}
	}
}
