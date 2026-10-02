package httpapi

import (
	"container/heap"
	"crypto/sha256"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/auth"
	"github.com/gplwzz1989/fingerprint-chromium/saas-server/internal/config"
)

type rateLimitEntry struct {
	key       [sha256.Size]byte
	requests  int
	expiresAt time.Time
}

type rateLimitExpiryHeap []*rateLimitEntry

func (h rateLimitExpiryHeap) Len() int           { return len(h) }
func (h rateLimitExpiryHeap) Less(i, j int) bool { return h[i].expiresAt.Before(h[j].expiresAt) }
func (h rateLimitExpiryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *rateLimitExpiryHeap) Push(value any)    { *h = append(*h, value.(*rateLimitEntry)) }
func (h *rateLimitExpiryHeap) Pop() any {
	last := len(*h) - 1
	entry := (*h)[last]
	(*h)[last] = nil
	*h = (*h)[:last]
	return entry
}

type rateLimiter struct {
	mu      sync.Mutex
	cfg     config.RateLimitConfig
	entries map[[sha256.Size]byte]*rateLimitEntry
	expiry  rateLimitExpiryHeap
	now     func() time.Time
}

func newRateLimiter(cfg config.RateLimitConfig) *rateLimiter {
	// 直接构造 Config 的调用方也使用默认值，避免零值误禁用或拒绝全部请求。
	defaults := config.DefaultRateLimitConfig()
	if cfg.Window <= 0 {
		cfg.Window = defaults.Window
	}
	if cfg.AuthRequests <= 0 {
		cfg.AuthRequests = defaults.AuthRequests
	}
	if cfg.APIRequests <= 0 {
		cfg.APIRequests = defaults.APIRequests
	}
	if cfg.MaxKeys <= 0 {
		cfg.MaxKeys = defaults.MaxKeys
	}
	return &rateLimiter{
		cfg: cfg, entries: make(map[[sha256.Size]byte]*rateLimitEntry), now: time.Now,
	}
}

func (l *rateLimiter) allow(identity string, limit int) (bool, time.Duration) {
	// 固定长度键保证内存占用不随请求中的标识长度增长。
	key := sha256.Sum256([]byte(identity))
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	// 请求驱动的过期回收不创建后台协程；每个条目在堆中只出现一次。
	for len(l.expiry) > 0 && !l.expiry[0].expiresAt.After(now) {
		entry := heap.Pop(&l.expiry).(*rateLimitEntry)
		delete(l.entries, entry.key)
	}
	entry, exists := l.entries[key]
	if !exists {
		if len(l.entries) >= l.cfg.MaxKeys {
			// 容量满时保留已有配额，防止轮换 IP 或用户标识淘汰旧计数绕过限流。
			return false, l.expiry[0].expiresAt.Sub(now)
		}
		entry = &rateLimitEntry{key: key, expiresAt: now.Add(l.cfg.Window)}
		l.entries[key] = entry
		heap.Push(&l.expiry, entry)
	}
	if entry.requests >= limit {
		return false, entry.expiresAt.Sub(now)
	}
	entry.requests++
	return true, 0
}

func (s *Server) withRateLimit(next http.Handler) http.Handler {
	if s.cfg.RateLimit.Disabled {
		return next
	}
	limiter := newRateLimiter(s.cfg.RateLimit)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions || (r.URL.Path != "/api" && !strings.HasPrefix(r.URL.Path, "/api/")) {
			next.ServeHTTP(w, r)
			return
		}
		identity := "api:ip:" + rateLimitRemoteIP(r.RemoteAddr)
		limit := limiter.cfg.APIRequests
		if isRateLimitedAuthRequest(r) {
			// 登录、刷新、注销和邀请接受共用真实来源 IP 配额，不能用令牌切换配额。
			identity = "auth:ip:" + rateLimitRemoteIP(r.RemoteAddr)
			limit = limiter.cfg.AuthRequests
		} else if userID := s.rateLimitUserID(r); userID != "" {
			identity = "api:user:" + userID
		}
		if allowed, retryAfter := limiter.allow(identity, limit); !allowed {
			seconds := retryAfter / time.Second
			if retryAfter%time.Second != 0 {
				seconds++
			}
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.FormatInt(int64(seconds), 10))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "请求过于频繁，请稍后重试")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isRateLimitedAuthRequest(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/api/v1/sessions", "/api/v1/sessions/refresh", "/api/v1/sessions/revoke", "/api/v1/invitations/accept":
		return true
	default:
		return false
	}
}

func rateLimitRemoteIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return addr.WithZone("").Unmap().String()
	}
	// 无法解析的来源统一计数，始终不读取客户端可伪造的转发头。
	return "unknown"
}

func (s *Server) rateLimitUserID(r *http.Request) string {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(s.cfg.JWTSecret) < 32 || len(authorization) <= len("Bearer ") || len(authorization) > 8192 ||
		!strings.EqualFold(authorization[:len("Bearer ")], "Bearer ") {
		return ""
	}
	claims, err := auth.ParseAccessToken(s.cfg.JWTSecret, strings.TrimSpace(authorization[len("Bearer "):]))
	if err != nil {
		return ""
	}
	// 这里只验证限流标识；业务处理仍由 requireUser 校验会话是否有效。
	return claims.Subject
}
