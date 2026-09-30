package server

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"oss-web/web"
)

// staticHandler 提供内嵌的 SPA 静态资源；未知的非 API 路径回退到 index.html。
func staticHandler() http.Handler {
	sub, err := fs.Sub(web.DistFS, "dist")
	if err != nil {
		panic(err)
	}
	fileServer := http.FileServer(http.FS(sub))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}
		clean := path.Clean(r.URL.Path)
		if clean == "/" || !exists(sub, clean) {
			// SPA 回退；禁用缓存以便新版本即时生效。
			w.Header().Set("Cache-Control", "no-cache")
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(clean, "/assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func exists(fsys fs.FS, name string) bool {
	_, err := fs.Stat(fsys, strings.TrimPrefix(name, "/"))
	return err == nil
}
