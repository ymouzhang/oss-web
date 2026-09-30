package server

import "net/http"

// handleBucketList 列出当前账号下的所有 bucket（bucket 模块入口）。
func (s *Server) handleBucketList(w http.ResponseWriter, r *http.Request) {
	buckets, err := s.store.ListBuckets(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"buckets":       buckets,
		"defaultBucket": s.cfg.Bucket,
	})
}
