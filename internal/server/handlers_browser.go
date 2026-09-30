package server

import (
	"mime"
	"net/http"
	"path"
	"strings"
)

// handleBrowserUpload 把浏览器上传的文件流经服务器中转到 OSS。
// POST /api/browser-upload?bucket=b&prefix=foo/&filename=a.bin，body 为文件内容。
func (s *Server) handleBrowserUpload(w http.ResponseWriter, r *http.Request) {
	bucket := r.URL.Query().Get("bucket")
	if !requireBucket(w, bucket) {
		return
	}
	prefix := strings.TrimPrefix(r.URL.Query().Get("prefix"), "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	name := r.URL.Query().Get("filename")
	if name == "" {
		if _, params, err := mime.ParseMediaType(r.Header.Get("Content-Disposition")); err == nil {
			name = params["filename"]
		}
	}
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || name == "." || name == "/" {
		writeError(w, http.StatusBadRequest, errStr("缺少 filename 参数"))
		return
	}
	if err := s.store.PutStream(r.Context(), bucket, prefix+name, r.Body, r.ContentLength); err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": prefix + name})
}
