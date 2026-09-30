// Package config 从环境变量加载并校验运行时配置。
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	User     string
	Password string

	AccessKeyID     string
	AccessKeySecret string
	Region          string
	Bucket          string // 可选；设置后前端默认选中该 bucket
	Endpoint        string
	UseInternal     bool
	SkipOSSCheck    bool

	LocalRoot     string
	CheckpointDir string

	PartSizeMB      int64
	ParallelNum     int
	FileConcurrency int
	Port            int
}

func Load() (*Config, error) {
	c := &Config{
		User:            os.Getenv("OSS_WEB_USER"),
		Password:        os.Getenv("OSS_WEB_PASSWORD"),
		AccessKeyID:     os.Getenv("OSS_ACCESS_KEY_ID"),
		AccessKeySecret: os.Getenv("OSS_ACCESS_KEY_SECRET"),
		Region:          os.Getenv("OSS_REGION"),
		Bucket:          os.Getenv("OSS_BUCKET"),
		Endpoint:        os.Getenv("OSS_ENDPOINT"),
		UseInternal:     boolEnv("OSS_USE_INTERNAL_ENDPOINT", false),
		SkipOSSCheck:    boolEnv("SKIP_OSS_CHECK", false),
		LocalRoot:       strEnv("LOCAL_ROOT", "/data"),
		PartSizeMB:      int64Env("TRANSFER_PART_SIZE_MB", 8),
		ParallelNum:     intEnv("TRANSFER_PARALLEL_NUM", 4),
		FileConcurrency: intEnv("TRANSFER_FILE_CONCURRENCY", 2),
		Port:            intEnv("PORT", 8080),
	}

	var missing []string
	for name, val := range map[string]string{
		"OSS_WEB_USER":          c.User,
		"OSS_WEB_PASSWORD":      c.Password,
		"OSS_ACCESS_KEY_ID":     c.AccessKeyID,
		"OSS_ACCESS_KEY_SECRET": c.AccessKeySecret,
		"OSS_REGION":            c.Region,
	} {
		if val == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("缺少必填环境变量: %s", strings.Join(missing, ", "))
	}
	if c.PartSizeMB < 1 {
		return nil, fmt.Errorf("TRANSFER_PART_SIZE_MB 必须 >= 1，当前为 %d", c.PartSizeMB)
	}
	if c.ParallelNum < 1 || c.FileConcurrency < 1 {
		return nil, fmt.Errorf("TRANSFER_PARALLEL_NUM 和 TRANSFER_FILE_CONCURRENCY 必须 >= 1")
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, fmt.Errorf("PORT 必须在 1-65535 之间，当前为 %d", c.Port)
	}

	root, err := filepath.Abs(c.LocalRoot)
	if err != nil {
		return nil, fmt.Errorf("LOCAL_ROOT 无效: %w", err)
	}
	c.LocalRoot = root
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("LOCAL_ROOT %q 不是已存在的目录: %v", root, err)
	}

	c.CheckpointDir = strEnv("CHECKPOINT_DIR", filepath.Join(root, ".oss-checkpoints"))
	if err := os.MkdirAll(c.CheckpointDir, 0o755); err != nil {
		return nil, fmt.Errorf("无法创建 CHECKPOINT_DIR %q: %w", c.CheckpointDir, err)
	}

	return c, nil
}

func (c *Config) PartSize() int64 { return c.PartSizeMB * 1024 * 1024 }

func strEnv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func boolEnv(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func intEnv(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func int64Env(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}
