package server

import (
	"errors"
	"net/http"

	"oss-web/internal/auth"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.limiter.Allow(clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, errors.New("登录尝试过于频繁，请稍后再试"))
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	token, err := s.auth.Login(body.Username, body.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, errors.New("用户名或密码错误"))
		return
	}
	auth.SetSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]string{"user": body.Username})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.auth.Logout(auth.TokenFromRequest(r))
	auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// handleConfig 返回前端需要的基础配置（默认 bucket、region、本地根目录）。
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"defaultBucket": s.cfg.Bucket,
		"region":        s.cfg.Region,
		"localRoot":     s.cfg.LocalRoot,
	})
}
