// Package ossclient 封装阿里云 OSS Go SDK V2，对外只暴露 Store 接口。
// 接口方法均显式携带 bucket 参数，不绑定单一 bucket；不同地域的 bucket
// 通过 region 级客户端缓存自动路由（V4 签名要求 region 与 bucket 实际地域一致）。
package ossclient

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"oss-web/internal/config"
)

// ProgressFn 是传输进度回调；transferred 为该文件的绝对已传字节数
// （SDK 分片续传恢复时会一次性回填已传部分，故不可用 increment 累加）。
type ProgressFn func(increment, transferred, total int64)

type BucketInfo struct {
	Name         string    `json:"name"`
	Region       string    `json:"region"`
	Location     string    `json:"location"`
	StorageClass string    `json:"storageClass"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Entry struct {
	Key          string    `json:"key"`
	Name         string    `json:"name"`
	IsDir        bool      `json:"isDir"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
}

type ListResult struct {
	Prefix      string  `json:"prefix"`
	Entries     []Entry `json:"entries"`
	IsTruncated bool    `json:"isTruncated"`
	NextToken   string  `json:"nextToken,omitempty"`
}

type ObjectMeta struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	ContentType  string    `json:"contentType"`
	LastModified time.Time `json:"lastModified"`
}

type Store interface {
	ListBuckets(ctx context.Context) ([]BucketInfo, error)
	List(ctx context.Context, bucket, prefix, token string, maxKeys int32) (ListResult, error)
	ListAll(ctx context.Context, bucket, prefix string) ([]Entry, error)
	Stat(ctx context.Context, bucket, key string) (ObjectMeta, error)
	Delete(ctx context.Context, bucket string, keys ...string) error
	Mkdir(ctx context.Context, bucket, prefix string) error
	PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error)
	PutStream(ctx context.Context, bucket, key string, body io.Reader, size int64) error
	Upload(ctx context.Context, bucket, key, filePath string, onProgress ProgressFn) error
	Download(ctx context.Context, bucket, key, filePath string, onProgress ProgressFn) error
}

// clientSet 是某个 region 下的客户端及传输组件。
type clientSet struct {
	client     *oss.Client
	uploader   *oss.Uploader
	downloader *oss.Downloader
}

type ossStore struct {
	cfg *config.Config

	mu         sync.Mutex
	byRegion   map[string]*clientSet
	bucketHome map[string]string // bucket -> region 缓存
}

func New(cfg *config.Config) (Store, error) {
	s := &ossStore{
		cfg:        cfg,
		byRegion:   map[string]*clientSet{},
		bucketHome: map[string]string{},
	}
	// 预建配置 region 的客户端，尽早暴露配置错误。
	if _, err := s.setForRegion(cfg.Region); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *ossStore) setForRegion(region string) (*clientSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cs, ok := s.byRegion[region]; ok {
		return cs, nil
	}
	ocfg := oss.NewConfig().
		WithRegion(region).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(s.cfg.AccessKeyID, s.cfg.AccessKeySecret))
	if s.cfg.Endpoint != "" {
		ocfg.WithEndpoint(s.cfg.Endpoint)
	}
	if s.cfg.UseInternal {
		ocfg.WithUseInternalEndpoint(true)
	}
	client := oss.NewClient(ocfg)
	cs := &clientSet{
		client: client,
		uploader: oss.NewUploader(client, func(o *oss.UploaderOptions) {
			o.PartSize = s.cfg.PartSize()
			o.ParallelNum = s.cfg.ParallelNum
			o.EnableCheckpoint = true
			o.CheckpointDir = s.cfg.CheckpointDir
		}),
		downloader: oss.NewDownloader(client, func(o *oss.DownloaderOptions) {
			o.PartSize = s.cfg.PartSize()
			o.ParallelNum = s.cfg.ParallelNum
			o.EnableCheckpoint = true
			o.CheckpointDir = s.cfg.CheckpointDir
		}),
	}
	s.byRegion[region] = cs
	return cs, nil
}

// clientFor 返回目标 bucket 所在 region 的客户端；
// region 未知时用 GetBucketLocation 探测并缓存，失败则退回配置 region。
func (s *ossStore) clientFor(ctx context.Context, bucket string) (*clientSet, error) {
	s.mu.Lock()
	region, ok := s.bucketHome[bucket]
	s.mu.Unlock()
	if !ok {
		region = s.cfg.Region
		base, err := s.setForRegion(region)
		if err != nil {
			return nil, err
		}
		res, err := base.client.GetBucketLocation(ctx, &oss.GetBucketLocationRequest{
			Bucket: oss.Ptr(bucket),
		})
		if err == nil && res.LocationConstraint != nil && *res.LocationConstraint != "" {
			// LocationConstraint 形如 oss-cn-hangzhou，region 需去掉 oss- 前缀。
			region = strings.TrimPrefix(*res.LocationConstraint, "oss-")
			s.mu.Lock()
			s.bucketHome[bucket] = region
			s.mu.Unlock()
		}
	}
	return s.setForRegion(region)
}

// Ping 启动连通性自检：设置了默认 bucket 则对其做一次 List，否则列 bucket。
func (s *ossStore) Ping(ctx context.Context) error {
	if s.cfg.Bucket != "" {
		_, err := s.List(ctx, s.cfg.Bucket, "", "", 1)
		return err
	}
	_, err := s.ListBuckets(ctx)
	return err
}

func (s *ossStore) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	base, err := s.setForRegion(s.cfg.Region)
	if err != nil {
		return nil, err
	}
	var buckets []BucketInfo
	marker := ""
	for {
		req := &oss.ListBucketsRequest{MaxKeys: 100}
		if marker != "" {
			req.Marker = oss.Ptr(marker)
		}
		res, err := base.client.ListBuckets(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, b := range res.Buckets {
			info := BucketInfo{}
			if b.Name != nil {
				info.Name = *b.Name
			}
			if b.Region != nil {
				info.Region = *b.Region
			} else if b.Location != nil {
				info.Region = strings.TrimPrefix(*b.Location, "oss-")
			}
			if b.Location != nil {
				info.Location = *b.Location
			}
			if b.StorageClass != nil {
				info.StorageClass = *b.StorageClass
			}
			if b.CreationDate != nil {
				info.CreatedAt = *b.CreationDate
			}
			buckets = append(buckets, info)
			if info.Name != "" && info.Region != "" {
				s.mu.Lock()
				s.bucketHome[info.Name] = info.Region
				s.mu.Unlock()
			}
		}
		if !res.IsTruncated || res.NextMarker == nil {
			break
		}
		marker = *res.NextMarker
	}
	return buckets, nil
}

func (s *ossStore) List(ctx context.Context, bucket, prefix, token string, maxKeys int32) (ListResult, error) {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return ListResult{}, err
	}
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	req := &oss.ListObjectsV2Request{
		Bucket:    oss.Ptr(bucket),
		Prefix:    oss.Ptr(prefix),
		Delimiter: oss.Ptr("/"),
		MaxKeys:   maxKeys,
	}
	if token != "" {
		req.ContinuationToken = oss.Ptr(token)
	}
	res, err := cs.client.ListObjectsV2(ctx, req)
	if err != nil {
		return ListResult{}, err
	}
	out := ListResult{Prefix: prefix, IsTruncated: res.IsTruncated}
	if res.NextContinuationToken != nil {
		out.NextToken = *res.NextContinuationToken
	}
	for _, cp := range res.CommonPrefixes {
		if cp.Prefix == nil {
			continue
		}
		full := *cp.Prefix
		out.Entries = append(out.Entries, Entry{
			Key:   full,
			Name:  strings.TrimSuffix(strings.TrimPrefix(full, prefix), "/"),
			IsDir: true,
		})
	}
	for _, obj := range res.Contents {
		if obj.Key == nil {
			continue
		}
		key := *obj.Key
		name := strings.TrimPrefix(key, prefix)
		if name == "" {
			continue
		}
		e := Entry{Key: key, Name: name, Size: obj.Size}
		if obj.LastModified != nil {
			e.LastModified = *obj.LastModified
		}
		out.Entries = append(out.Entries, e)
	}
	return out, nil
}

func (s *ossStore) Stat(ctx context.Context, bucket, key string) (ObjectMeta, error) {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return ObjectMeta{}, err
	}
	res, err := cs.client.HeadObject(ctx, &oss.HeadObjectRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		return ObjectMeta{}, err
	}
	m := ObjectMeta{Key: key, Size: res.ContentLength}
	if res.ContentType != nil {
		m.ContentType = *res.ContentType
	}
	if res.LastModified != nil {
		m.LastModified = *res.LastModified
	}
	return m, nil
}

// Delete 删除对象；以 "/" 结尾的 key 视为前缀，递归删除其下所有对象。
func (s *ossStore) Delete(ctx context.Context, bucket string, keys ...string) error {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return err
	}
	var all []string
	for _, key := range keys {
		if key == "" {
			continue
		}
		if strings.HasSuffix(key, "/") {
			collected, err := s.listAllKeys(ctx, cs, bucket, key)
			if err != nil {
				return err
			}
			all = append(all, collected...)
		} else {
			all = append(all, key)
		}
	}
	const batch = 1000
	for i := 0; i < len(all); i += batch {
		end := i + batch
		if end > len(all) {
			end = len(all)
		}
		objects := make([]oss.DeleteObject, 0, end-i)
		for _, k := range all[i:end] {
			objects = append(objects, oss.DeleteObject{Key: oss.Ptr(k)})
		}
		_, err := cs.client.DeleteMultipleObjects(ctx, &oss.DeleteMultipleObjectsRequest{
			Bucket:  oss.Ptr(bucket),
			Objects: objects,
		})
		if err != nil {
			return fmt.Errorf("批量删除对象失败: %w", err)
		}
	}
	return nil
}

func (s *ossStore) listAllKeys(ctx context.Context, cs *clientSet, bucket, prefix string) ([]string, error) {
	var keys []string
	token := ""
	for {
		req := &oss.ListObjectsV2Request{
			Bucket:  oss.Ptr(bucket),
			Prefix:  oss.Ptr(prefix),
			MaxKeys: 1000,
		}
		if token != "" {
			req.ContinuationToken = oss.Ptr(token)
		}
		res, err := cs.client.ListObjectsV2(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, obj := range res.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
		if !res.IsTruncated || res.NextContinuationToken == nil {
			break
		}
		token = *res.NextContinuationToken
	}
	return keys, nil
}

// ListAll 返回前缀下全部对象（含大小），用于把文件夹展开成传输 item。
func (s *ossStore) ListAll(ctx context.Context, bucket, prefix string) ([]Entry, error) {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return nil, err
	}
	keys, err := s.listAllKeys(ctx, cs, bucket, prefix)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(keys))
	for _, k := range keys {
		meta, err := s.Stat(ctx, bucket, k)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", k, err)
		}
		entries = append(entries, Entry{Key: k, Size: meta.Size, LastModified: meta.LastModified})
	}
	return entries, nil
}

func (s *ossStore) Mkdir(ctx context.Context, bucket, prefix string) error {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	_, err = cs.client.PutObject(ctx, &oss.PutObjectRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(prefix),
		Body:   strings.NewReader(""),
	})
	return err
}

func (s *ossStore) PresignGet(ctx context.Context, bucket, key string, ttl time.Duration) (string, error) {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return "", err
	}
	res, err := cs.client.Presign(ctx, &oss.GetObjectRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(key),
	}, func(o *oss.PresignOptions) {
		o.Expires = ttl
	})
	if err != nil {
		return "", err
	}
	return res.URL, nil
}

func (s *ossStore) PutStream(ctx context.Context, bucket, key string, body io.Reader, size int64) error {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return err
	}
	_, err = cs.client.PutObject(ctx, &oss.PutObjectRequest{
		Bucket:        oss.Ptr(bucket),
		Key:           oss.Ptr(key),
		Body:          body,
		ContentLength: oss.Ptr(size),
	})
	return err
}

func (s *ossStore) Upload(ctx context.Context, bucket, key, filePath string, onProgress ProgressFn) error {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return err
	}
	_, err = cs.uploader.UploadFile(ctx, &oss.PutObjectRequest{
		Bucket:     oss.Ptr(bucket),
		Key:        oss.Ptr(key),
		ProgressFn: oss.ProgressFunc(onProgress),
	}, filePath)
	return err
}

func (s *ossStore) Download(ctx context.Context, bucket, key, filePath string, onProgress ProgressFn) error {
	cs, err := s.clientFor(ctx, bucket)
	if err != nil {
		return err
	}
	_, err = cs.downloader.DownloadFile(ctx, &oss.GetObjectRequest{
		Bucket:     oss.Ptr(bucket),
		Key:        oss.Ptr(key),
		ProgressFn: oss.ProgressFunc(onProgress),
	}, filePath)
	return err
}
