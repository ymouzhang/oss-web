// Package auth 实现账号密码登录、内存态 session 和登录限流。
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"sync"
	"time"
)

const (
	SessionCookieName = "oss_web_session"
	sessionTTL        = 24 * time.Hour
)

var ErrInvalidCredentials = errors.New("用户名或密码错误")

type Manager struct {
	user     string
	password string

	mu       sync.Mutex
	sessions map[string]time.Time // token -> 过期时间
}

func NewManager(user, password string) *Manager {
	return &Manager{user: user, password: password, sessions: map[string]time.Time{}}
}

func (m *Manager) Login(user, password string) (token string, err error) {
	userOK := subtle.ConstantTimeCompare([]byte(user), []byte(m.user)) == 1
	passOK := subtle.ConstantTimeCompare([]byte(password), []byte(m.password)) == 1
	if !userOK || !passOK {
		return "", ErrInvalidCredentials
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token = hex.EncodeToString(buf)
	m.mu.Lock()
	m.sessions[token] = time.Now().Add(sessionTTL)
	m.mu.Unlock()
	return token, nil
}

// Validate 校验 token 是否为有效 session；有效则滑动续期。
func (m *Manager) Validate(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	expiry, ok := m.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(m.sessions, token)
		return false
	}
	m.sessions[token] = time.Now().Add(sessionTTL)
	return true
}

func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// TokenFromRequest 从请求 cookie 中取出 session token。
func TokenFromRequest(r *http.Request) string {
	c, err := r.Cookie(SessionCookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// SetSessionCookie 把 session 写入响应 cookie。
func SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// Require 包装 handler 做 session 校验，未登录返回 401。
func (m *Manager) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Validate(TokenFromRequest(r)) {
			w.Header().Set("Content-Type", "application/json")
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RateLimiter 是按 key 的令牌桶，用于登录接口限流。
type RateLimiter struct {
	mu       sync.Mutex
	buckets  map[string]*bucket
	capacity float64
	refill   float64 // 每秒补充的令牌数
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewRateLimiter(perMinute float64) *RateLimiter {
	return &RateLimiter{
		buckets:  map[string]*bucket{},
		capacity: perMinute,
		refill:   perMinute / 60,
	}
}

func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	b, ok := rl.buckets[key]
	now := time.Now()
	if !ok {
		b = &bucket{tokens: rl.capacity, last: now}
		rl.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.refill
	if b.tokens > rl.capacity {
		b.tokens = rl.capacity
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
