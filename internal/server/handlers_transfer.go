package server

import (
	"context"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"oss-web/internal/transfer"
)

type transferRequest struct {
	Direction  string   `json:"direction"`
	Bucket     string   `json:"bucket"`     // 目标/来源 bucket
	LocalPaths []string `json:"localPaths"` // 上传：选中的本地路径（相对 LOCAL_ROOT）
	OSSPrefix  string   `json:"ossPrefix"`  // 上传：OSS 目标前缀
	OSSKeys    []string `json:"ossKeys"`    // 下载：选中的 key/文件夹
	LocalDir   string   `json:"localDir"`   // 下载：本地目标目录（相对 LOCAL_ROOT）
}

func (s *Server) handleTransfer(w http.ResponseWriter, r *http.Request) {
	var req transferRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !requireBucket(w, req.Bucket) {
		return
	}
	var specs []transfer.ItemSpec
	var dir transfer.Direction
	var err error

	switch req.Direction {
	case "upload":
		dir = transfer.Upload
		specs, err = s.expandUpload(req.Bucket, req.LocalPaths, req.OSSPrefix)
	case "download":
		dir = transfer.Download
		specs, err = s.expandDownload(r.Context(), req.Bucket, req.OSSKeys, req.LocalDir)
	default:
		writeError(w, http.StatusBadRequest, errStr("direction 必须是 upload 或 download"))
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if len(specs) == 0 {
		writeError(w, http.StatusBadRequest, errStr("没有可传输的文件"))
		return
	}
	job := s.jobs.Submit(req.Bucket, dir, specs)
	snap, err := s.jobs.Get(job.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

// expandUpload 把选中的本地文件/文件夹映射为 ossPrefix 下的 OSS key。
func (s *Server) expandUpload(bucket string, localPaths []string, ossPrefix string) ([]transfer.ItemSpec, error) {
	if len(localPaths) == 0 {
		return nil, errStr("localPaths 不能为空")
	}
	prefix := strings.TrimPrefix(ossPrefix, "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	var specs []transfer.ItemSpec
	for _, p := range localPaths {
		st, err := s.lfs.Stat(p)
		if err != nil {
			return nil, err
		}
		files, err := s.lfs.WalkFiles(p)
		if err != nil {
			return nil, err
		}
		if !st.IsDir {
			specs = append(specs, transfer.ItemSpec{
				Bucket: bucket,
				Src:    files[0].Abs,
				Dst:    prefix + path.Base(st.Path),
				Size:   files[0].Size,
			})
			continue
		}
		dirRel := strings.TrimSuffix(st.Path, "/")
		dirName := path.Base(dirRel)
		for _, f := range files {
			sub := strings.TrimPrefix(strings.TrimPrefix(f.Rel, dirRel), "/")
			specs = append(specs, transfer.ItemSpec{
				Bucket: bucket,
				Src:    f.Abs,
				Dst:    prefix + dirName + "/" + sub,
				Size:   f.Size,
			})
		}
	}
	return specs, nil
}

// expandDownload 把选中的 OSS key/文件夹映射为 localDir 下的本地文件。
func (s *Server) expandDownload(ctx context.Context, bucket string, ossKeys []string, localDir string) ([]transfer.ItemSpec, error) {
	if len(ossKeys) == 0 {
		return nil, errStr("ossKeys 不能为空")
	}
	var specs []transfer.ItemSpec
	for _, key := range ossKeys {
		if strings.HasSuffix(key, "/") {
			entries, err := s.store.ListAll(ctx, bucket, key)
			if err != nil {
				return nil, err
			}
			dirName := path.Base(strings.TrimSuffix(key, "/"))
			for _, e := range entries {
				if e.Size == 0 && strings.HasSuffix(e.Key, "/") {
					continue // 空目录占位对象
				}
				sub := strings.TrimPrefix(e.Key, key)
				dst, err := s.joinLocal(localDir, dirName, sub)
				if err != nil {
					return nil, err
				}
				specs = append(specs, transfer.ItemSpec{Bucket: bucket, Src: e.Key, Dst: dst, Size: e.Size})
			}
			continue
		}
		meta, err := s.store.Stat(ctx, bucket, key)
		if err != nil {
			return nil, err
		}
		dst, err := s.joinLocal(localDir, path.Base(key), "")
		if err != nil {
			return nil, err
		}
		specs = append(specs, transfer.ItemSpec{Bucket: bucket, Src: key, Dst: dst, Size: meta.Size})
	}
	return specs, nil
}

// joinLocal 在 LOCAL_ROOT 内拼出目标文件路径，并确保其父目录存在。
func (s *Server) joinLocal(localDir string, parts ...string) (string, error) {
	clean := []string{localDir}
	for _, p := range parts {
		if p != "" {
			clean = append(clean, p)
		}
	}
	rel := filepath.Join(clean...)
	dirAbs, err := s.lfs.EnsureDir(filepath.Dir(rel))
	if err != nil {
		return "", err
	}
	return filepath.Join(dirAbs, filepath.Base(rel)), nil
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"jobs": s.jobs.List()})
}

func (s *Server) handleJob(w http.ResponseWriter, r *http.Request) {
	snap, err := s.jobs.Get(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	if err := s.jobs.Cancel(r.PathValue("id")); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleJobRetry(w http.ResponseWriter, r *http.Request) {
	if err := s.jobs.Retry(r.PathValue("id")); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
