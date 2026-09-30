// Package server 负责 HTTP 路由、鉴权中间件和各 API handler。
package server

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"oss-web/internal/auth"
	"oss-web/internal/config"
	"oss-web/internal/localfs"
	"oss-web/internal/ossclient"
	"oss-web/internal/transfer"
)

type Server struct {
	cfg     *config.Config
	auth    *auth.Manager
	limiter *auth.RateLimiter
	store   ossclient.Store
	lfs     *localfs.FS
	jobs    *transfer.Manager
	mux     *http.ServeMux
}

func New(cfg *config.Config, store ossclient.Store, lfs *localfs.FS, jobs *transfer.Manager) *Server {
	s := &Server{
		cfg:     cfg,
		auth:    auth.NewManager(cfg.User, cfg.Password),
		limiter: auth.NewRateLimiter(10),
		store:   store,
		lfs:     lfs,
		jobs:    jobs,
		mux:     http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	require := s.auth.Require

	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.Handle("POST /api/logout", require(http.HandlerFunc(s.handleLogout)))
	s.mux.Handle("GET /api/config", require(http.HandlerFunc(s.handleConfig)))

	// bucket 模块
	s.mux.Handle("GET /api/oss/buckets", require(http.HandlerFunc(s.handleBucketList)))
	// bucket 内对象模块
	s.mux.Handle("GET /api/oss/list", require(http.HandlerFunc(s.handleOSSList)))
	s.mux.Handle("POST /api/oss/mkdir", require(http.HandlerFunc(s.handleOSSMkdir)))
	s.mux.Handle("DELETE /api/oss/object", require(http.HandlerFunc(s.handleOSSDelete)))
	s.mux.Handle("POST /api/oss/presign", require(http.HandlerFunc(s.handleOSSPresign)))

	s.mux.Handle("GET /api/local/list", require(http.HandlerFunc(s.handleLocalList)))
	s.mux.Handle("POST /api/local/mkdir", require(http.HandlerFunc(s.handleLocalMkdir)))
	s.mux.Handle("DELETE /api/local", require(http.HandlerFunc(s.handleLocalDelete)))

	s.mux.Handle("POST /api/transfer", require(http.HandlerFunc(s.handleTransfer)))
	s.mux.Handle("GET /api/jobs", require(http.HandlerFunc(s.handleJobs)))
	s.mux.Handle("GET /api/jobs/{id}", require(http.HandlerFunc(s.handleJob)))
	s.mux.Handle("POST /api/jobs/{id}/cancel", require(http.HandlerFunc(s.handleJobCancel)))
	s.mux.Handle("POST /api/jobs/{id}/retry", require(http.HandlerFunc(s.handleJobRetry)))

	s.mux.Handle("POST /api/browser-upload", require(http.HandlerFunc(s.handleBrowserUpload)))

	s.mux.Handle("/", staticHandler())
}

func (s *Server) Handler() http.Handler {
	return s.securityHeaders(s.mux)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("写 JSON 响应失败: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	statusFor := status
	if errors.Is(err, localfs.ErrEscape) {
		statusFor = http.StatusForbidden
	}
	writeJSON(w, statusFor, map[string]string{"error": err.Error()})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体不是合法 JSON: " + err.Error()})
		return false
	}
	return true
}

// requireBucket 取出并校验 bucket 参数，缺失时写 400 并返回空串。
func requireBucket(w http.ResponseWriter, bucket string) bool {
	if bucket == "" {
		writeError(w, http.StatusBadRequest, errStr("缺少 bucket 参数"))
		return false
	}
	return true
}

func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		return host[:i]
	}
	return host
}
