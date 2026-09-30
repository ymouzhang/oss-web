// Package localfs 把所有文件系统访问限制在配置的根目录内。
package localfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var ErrEscape = errors.New("路径超出 LOCAL_ROOT 范围")

type FS struct {
	root string // 绝对路径，已解析符号链接
}

type Entry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"` // 相对根目录
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

type FileInfo struct {
	Rel  string
	Abs  string
	Size int64
}

func New(root string) (*FS, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("解析根目录失败: %w", err)
	}
	return &FS{root: resolved}, nil
}

func (f *FS) Root() string { return f.root }

// resolve 把用户给的相对路径映射到根目录内的绝对路径。
// 拒绝：含 ".." 段的路径、根目录外的绝对路径、逃逸根目录的符号链接。
func (f *FS) resolve(rel string) (string, error) {
	var abs string
	if filepath.IsAbs(rel) {
		abs = filepath.Clean(rel)
	} else {
		for _, seg := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == '\\' }) {
			if seg == ".." {
				return "", ErrEscape
			}
		}
		abs = filepath.Join(f.root, filepath.Clean("/"+rel))
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// 目标尚不存在（mkdir、下载目的地）时，向上解析最深的已存在祖先。
			ancestor := abs
			for {
				parent := filepath.Dir(ancestor)
				if parent == ancestor {
					return "", err
				}
				r, e := filepath.EvalSymlinks(parent)
				if e == nil {
					rest, relErr := filepath.Rel(parent, abs)
					if relErr != nil {
						return "", relErr
					}
					resolved = filepath.Join(r, rest)
					break
				}
				if !errors.Is(e, os.ErrNotExist) {
					return "", e
				}
				ancestor = parent
			}
		} else {
			return "", err
		}
	}
	if resolved != f.root && !strings.HasPrefix(resolved, f.root+string(filepath.Separator)) {
		return "", ErrEscape
	}
	return resolved, nil
}

// Rel 把（已在根目录内的）绝对路径转换为相对根目录的形式。
func (f *FS) Rel(abs string) (string, error) {
	rel, err := filepath.Rel(f.root, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", ErrEscape
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}

func (f *FS) List(rel string) ([]Entry, error) {
	abs, err := f.resolve(rel)
	if err != nil {
		return nil, err
	}
	des, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue
		}
		childRel := filepath.Join(rel, de.Name())
		e := Entry{
			Name:    de.Name(),
			Path:    filepath.ToSlash(childRel),
			IsDir:   de.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if de.Type()&os.ModeSymlink != 0 {
			// 符号链接取目标的是否目录属性，方便前端导航；
			// 真正访问时 resolve 仍会拦截逃逸。
			if st, serr := os.Stat(filepath.Join(abs, de.Name())); serr == nil && st.IsDir() {
				e.IsDir = true
			}
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func (f *FS) Mkdir(rel string) error {
	abs, err := f.resolve(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0o755)
}

func (f *FS) Remove(rel string) error {
	if rel == "" || rel == "." || rel == "/" {
		return fmt.Errorf("不允许删除根目录")
	}
	abs, err := f.resolve(rel)
	if err != nil {
		return err
	}
	return os.RemoveAll(abs)
}

func (f *FS) Stat(rel string) (Entry, error) {
	abs, err := f.resolve(rel)
	if err != nil {
		return Entry{}, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		Name:    info.Name(),
		Path:    filepath.ToSlash(filepath.Clean(rel)),
		IsDir:   info.IsDir(),
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

// WalkFiles 把 rel 展开为其下所有普通文件；rel 本身是文件时结果只含它自己。
func (f *FS) WalkFiles(rel string) ([]FileInfo, error) {
	abs, err := f.resolve(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []FileInfo{{Rel: filepath.ToSlash(filepath.Clean(rel)), Abs: abs, Size: info.Size()}}, nil
	}
	var files []FileInfo
	err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		r, err := f.Rel(path)
		if err != nil {
			return err
		}
		files = append(files, FileInfo{Rel: filepath.ToSlash(r), Abs: path, Size: fi.Size()})
		return nil
	})
	return files, err
}

// EnsureDir 把 rel 解析为根目录内的目录并确保其存在，用于准备下载目的地。
func (f *FS) EnsureDir(rel string) (string, error) {
	abs, err := f.resolve(rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", err
	}
	return f.resolve(rel)
}
