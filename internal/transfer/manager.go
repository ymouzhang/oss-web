// Package transfer 管理本地磁盘与 OSS 之间的上传/下载任务。
package transfer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"oss-web/internal/ossclient"
)

type Direction string

const (
	Upload   Direction = "upload"
	Download Direction = "download"
)

type Status string

const (
	Pending   Status = "pending"
	Running   Status = "running"
	Done      Status = "done"
	Error     Status = "error"
	Cancelled Status = "cancelled"
)

type ItemSpec struct {
	Bucket string // 目标/来源 bucket
	Src    string // 上传：本地绝对路径；下载：OSS key
	Dst    string // 上传：OSS key；下载：本地绝对路径
	Size   int64
}

type Item struct {
	Bucket string `json:"bucket"`
	Src    string `json:"src"`
	Dst    string `json:"dst"`
	Size   int64  `json:"size"`

	Transferred int64  `json:"transferred"`
	Status      Status `json:"status"`
	Error       string `json:"error,omitempty"`

	cancel context.CancelFunc
}

type Job struct {
	ID        string    `json:"id"`
	Bucket    string    `json:"bucket"`
	Direction Direction `json:"direction"`
	Status    Status    `json:"status"`
	Items     []*Item   `json:"items"`
	Total     int64     `json:"total"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`

	// doneItems 记录进入终态的 item 数，用于聚合 job 状态。
	doneItems atomic.Int64
}

// Transferred 汇总各 item 的已传字节数。
func (j *Job) Transferred() int64 {
	var n int64
	for _, it := range j.Items {
		n += atomic.LoadInt64(&it.Transferred)
	}
	return n
}

type JobSnapshot struct {
	ID          string         `json:"id"`
	Bucket      string         `json:"bucket"`
	Direction   Direction      `json:"direction"`
	Status      Status         `json:"status"`
	Total       int64          `json:"total"`
	Transferred int64          `json:"transferred"`
	Error       string         `json:"error,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	Items       []ItemSnapshot `json:"items,omitempty"`
}

type ItemSnapshot struct {
	Bucket      string `json:"bucket"`
	Src         string `json:"src"`
	Dst         string `json:"dst"`
	Size        int64  `json:"size"`
	Transferred int64  `json:"transferred"`
	Status      Status `json:"status"`
	Error       string `json:"error,omitempty"`
}

func snapshotJob(j *Job, withItems bool) JobSnapshot {
	s := JobSnapshot{
		ID:          j.ID,
		Bucket:      j.Bucket,
		Direction:   j.Direction,
		Status:      j.Status,
		Total:       j.Total,
		Transferred: j.Transferred(),
		Error:       j.Error,
		CreatedAt:   j.CreatedAt,
	}
	if withItems {
		for _, it := range j.Items {
			s.Items = append(s.Items, ItemSnapshot{
				Bucket:      it.Bucket,
				Src:         it.Src,
				Dst:         it.Dst,
				Size:        it.Size,
				Transferred: atomic.LoadInt64(&it.Transferred),
				Status:      it.Status,
				Error:       it.Error,
			})
		}
	}
	return s
}

var (
	ErrNotFound     = errors.New("任务不存在")
	ErrNotRunning   = errors.New("任务不在进行中")
	ErrNotRetryable = errors.New("只有失败/已取消的任务可以重试")
)

const maxJobs = 200

type Manager struct {
	store ossclient.Store

	mu   sync.Mutex
	jobs []*Job // 最新的在末尾；超出上限淘汰最旧
	byID map[string]*Job

	queue chan queueEntry
}

type queueEntry struct {
	job  *Job
	item *Item
}

func NewManager(store ossclient.Store, fileConcurrency int) *Manager {
	m := &Manager{
		store: store,
		byID:  map[string]*Job{},
		queue: make(chan queueEntry, 1024),
	}
	for i := 0; i < fileConcurrency; i++ {
		go m.worker()
	}
	return m
}

// Submit 用已展开的 item 列表创建任务并入队。
func (m *Manager) Submit(bucket string, direction Direction, specs []ItemSpec) *Job {
	j := &Job{
		ID:        newID(),
		Bucket:    bucket,
		Direction: direction,
		Status:    Pending,
		CreatedAt: time.Now(),
	}
	for _, sp := range specs {
		j.Items = append(j.Items, &Item{Bucket: sp.Bucket, Src: sp.Src, Dst: sp.Dst, Size: sp.Size, Status: Pending})
		j.Total += sp.Size
	}

	m.mu.Lock()
	m.jobs = append(m.jobs, j)
	m.byID[j.ID] = j
	for len(m.jobs) > maxJobs {
		old := m.jobs[0]
		m.jobs = m.jobs[1:]
		delete(m.byID, old.ID)
	}
	m.mu.Unlock()

	for _, it := range j.Items {
		m.queue <- queueEntry{job: j, item: it}
	}
	return j
}

func (m *Manager) List() []JobSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]JobSnapshot, 0, len(m.jobs))
	// 最新的排前面
	for i := len(m.jobs) - 1; i >= 0; i-- {
		out = append(out, snapshotJob(m.jobs[i], false))
	}
	return out
}

func (m *Manager) Get(id string) (JobSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.byID[id]
	if !ok {
		return JobSnapshot{}, ErrNotFound
	}
	return snapshotJob(j, true), nil
}

// Cancel 停止排队/进行中的任务；进行中的 item 在当前分片完成后退出，checkpoint 保留。
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	j, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	if j.Status != Pending && j.Status != Running {
		m.mu.Unlock()
		return ErrNotRunning
	}
	j.Status = Cancelled
	for _, it := range j.Items {
		if it.Status == Pending {
			it.Status = Cancelled
			j.doneItems.Add(1)
		} else if it.Status == Running && it.cancel != nil {
			it.cancel()
		}
	}
	m.mu.Unlock()
	return nil
}

// Retry 把失败/已取消任务中未完成的 item 重新入队；SDK 命中 checkpoint 自动续传。
func (m *Manager) Retry(id string) error {
	m.mu.Lock()
	j, ok := m.byID[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	if j.Status != Error && j.Status != Cancelled {
		m.mu.Unlock()
		return ErrNotRetryable
	}
	j.Status = Pending
	j.Error = ""
	var requeue []*Item
	for _, it := range j.Items {
		if it.Status == Done {
			continue
		}
		it.Status = Pending
		it.Error = ""
		atomic.StoreInt64(&it.Transferred, 0)
		j.doneItems.Add(-1)
		requeue = append(requeue, it)
	}
	m.mu.Unlock()
	for _, it := range requeue {
		m.queue <- queueEntry{job: j, item: it}
	}
	return nil
}

func (m *Manager) worker() {
	for e := range m.queue {
		m.runItem(e.job, e.item)
	}
}

func (m *Manager) runItem(j *Job, it *Item) {
	m.mu.Lock()
	if j.Status == Pending {
		j.Status = Running
	}
	if it.Status != Pending {
		m.mu.Unlock()
		return
	}
	it.Status = Running
	ctx, cancel := context.WithCancel(context.Background())
	it.cancel = cancel
	m.mu.Unlock()

	onProgress := func(increment, transferred, total int64) {
		atomic.StoreInt64(&it.Transferred, transferred)
	}

	var err error
	if j.Direction == Upload {
		err = m.store.Upload(ctx, it.Bucket, it.Dst, it.Src, onProgress)
	} else {
		err = m.store.Download(ctx, it.Bucket, it.Src, it.Dst, onProgress)
	}
	cancel()

	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		if j.Status == Cancelled || errors.Is(err, context.Canceled) {
			it.Status = Cancelled
		} else {
			it.Status = Error
			it.Error = err.Error()
		}
	} else {
		it.Status = Done
		atomic.StoreInt64(&it.Transferred, it.Size)
	}
	m.aggregate(j)
}

func (m *Manager) aggregate(j *Job) {
	done := j.doneItems.Add(1)
	if int(done) < len(j.Items) {
		return
	}
	if j.Status == Cancelled {
		return
	}
	allDone := true
	var firstErr string
	for _, it := range j.Items {
		if it.Status == Error && firstErr == "" {
			firstErr = it.Error
		}
		if it.Status != Done {
			allDone = false
		}
	}
	if allDone {
		j.Status = Done
	} else {
		j.Status = Error
		j.Error = firstErr
	}
}

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
