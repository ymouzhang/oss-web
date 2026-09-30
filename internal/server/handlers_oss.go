package server

import (
	"net/http"
	"strings"
	"time"
)

// 本文件是 bucket 内对象模块的 handler，均要求 bucket 参数。

func (s *Server) handleOSSList(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	if !requireBucket(w, bucket) {
		return
	}
	prefix := r.URL.Query().Get("prefix")
	token := r.URL.Query().Get("continuation-token")
	res, err := s.store.List(r.Context(), bucket, prefix, token, 100)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleOSSMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bucket string `json:"bucket"`
		Prefix string `json:"prefix"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !requireBucket(w, body.Bucket) {
		return
	}
	prefix := strings.Trim(body.Prefix, "/")
	if prefix == "" {
		writeError(w, http.StatusBadRequest, errStr("prefix 不能为空"))
		return
	}
	if strings.Contains(prefix, "..") {
		writeError(w, http.StatusBadRequest, errStr("prefix 不允许包含 .."))
		return
	}
	if err := s.store.Mkdir(r.Context(), body.Bucket, prefix+"/"); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOSSDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bucket    string   `json:"bucket"`
		Keys      []string `json:"keys"`
		Recursive bool     `json:"recursive"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !requireBucket(w, body.Bucket) {
		return
	}
	if len(body.Keys) == 0 {
		writeError(w, http.StatusBadRequest, errStr("keys 不能为空"))
		return
	}
	for _, k := range body.Keys {
		if strings.HasSuffix(k, "/") && !body.Recursive {
			writeError(w, http.StatusBadRequest, errStr("删除文件夹需要 recursive=true"))
			return
		}
	}
	if err := s.store.Delete(r.Context(), body.Bucket, body.Keys...); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleOSSPresign(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Bucket string `json:"bucket"`
		Key    string `json:"key"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if !requireBucket(w, body.Bucket) {
		return
	}
	if body.Key == "" || strings.HasSuffix(body.Key, "/") {
		writeError(w, http.StatusBadRequest, errStr("key 必须是文件对象"))
		return
	}
	url, err := s.store.PresignGet(r.Context(), body.Bucket, body.Key, 15*time.Minute)
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

type strErr string

func (e strErr) Error() string { return string(e) }

func errStr(msg string) error { return strErr(msg) }
