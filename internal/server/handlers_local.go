package server

import (
	"net/http"
	"strings"
)

func (s *Server) handleLocalList(w http.ResponseWriter, r *http.Request) {
	entries, err := s.lfs.List(r.URL.Query().Get("path"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

func (s *Server) handleLocalMkdir(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if strings.Trim(body.Path, "/") == "" {
		writeError(w, http.StatusBadRequest, errStr("path 不能为空"))
		return
	}
	if err := s.lfs.Mkdir(body.Path); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleLocalDelete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Paths []string `json:"paths"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	if len(body.Paths) == 0 {
		writeError(w, http.StatusBadRequest, errStr("paths 不能为空"))
		return
	}
	for _, p := range body.Paths {
		if err := s.lfs.Remove(p); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
