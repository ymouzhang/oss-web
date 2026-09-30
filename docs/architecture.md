# 架构设计

版本：v1.0 草案（对应 docs/prd.md v1.0）

## 1. 总体架构

```
浏览器 (React SPA)
   │  HTTP/JSON + cookie session
   ▼
Go HTTP Server (cmd/oss-web)
   ├── internal/auth      登录、session、限流
   ├── internal/server    路由、handlers（bucket 模块 / 对象模块分离）、静态资源 embed
   ├── internal/ossclient OSS SDK V2 封装（ListBuckets + 带 bucket 参数的对象操作 + region 级客户端缓存）
   ├── internal/localfs   本地文件系统（根目录约束）
   └── internal/transfer  传输任务系统（队列、worker pool、进度、checkpoint）
                                  │
                                  ▼
                    阿里云 OSS (SDK V2, V4 签名)
```

- 单进程、单二进制：前端产物 `go:embed` 进二进制，运行时无外部静态依赖
- 任务系统全部内存态（进程重启任务列表清空，磁盘 checkpoint 保留可续传）
- 无数据库、无外部服务依赖

## 2. 项目结构（标准 Go 布局）

```
oss-web/
├── cmd/
│   └── oss-web/
│       └── main.go              # 入口：加载配置 → 组装依赖 → 启动 server（优雅退出）
├── internal/
│   ├── config/
│   │   └── config.go            # 环境变量加载、默认值、启动校验
│   ├── auth/
│   │   ├── auth.go              # 登录校验（constant-time）、session 签发/销毁
│   │   └── middleware.go        # session 中间件、登录限流
│   ├── ossclient/
│   │   └── client.go            # OSS SDK V2 初始化、region 级客户端缓存、ListBuckets、对象操作、Uploader/Downloader
│   ├── localfs/
│   │   └── localfs.go           # 目录列举、mkdir、删除、路径防逃逸（EvalSymlinks + 前缀校验）
│   ├── transfer/
│   │   └── manager.go           # Job/JobItem 模型（含 bucket）、状态机、队列、worker pool、cancel/retry
│   └── server/
│       ├── server.go            # 路由注册（net/http ServeMux，Go 1.22+ 模式路由）、安全响应头
│       ├── handlers_bucket.go   # bucket 模块 API（GET /api/oss/buckets）
│       ├── handlers_oss.go      # bucket 内对象模块 API（list/mkdir/delete/presign，均带 bucket 参数）
│       ├── handlers_local.go    # 本地文件 API
│       ├── handlers_transfer.go # 任务 API（含目录展开）
│       ├── handlers_browser.go  # 浏览器中转上传
│       ├── handlers_auth.go     # 登录/退出/配置
│       └── static.go            # go:embed web/dist，SPA fallback（未知路径回 index.html）
├── web/                         # React 前端（Vite + TypeScript）
│   ├── src/
│   │   ├── main.tsx
│   │   ├── App.tsx              # 登录视图 / 主视图切换（401 感知）
│   │   ├── api.ts               # fetch 封装（统一错误、401 处理；OSS 请求均带 bucket）
│   │   ├── components/
│   │   │   ├── LoginView.tsx
│   │   │   ├── LocalPane.tsx    # 本地文件栏
│   │   │   ├── oss/             # OSS 侧组件（模块边界清晰，可独立演进）
│   │   │   │   ├── OssPane.tsx      # 编排两级视图 + 面包屑
│   │   │   │   ├── BucketList.tsx   # bucket 列表模块
│   │   │   │   └── ObjectList.tsx   # bucket 内对象模块
│   │   │   ├── FileTable.tsx    # 通用文件列表（多选、排序、面包屑）
│   │   │   └── JobsPanel.tsx    # 任务面板（进度条、速度、取消/重试）
│   │   └── styles.css
│   ├── index.html
│   ├── package.json
│   ├── tsconfig.json
│   └── vite.config.ts           # dev 代理 /api → localhost:8080
├── docs/
│   ├── prd.md
│   └── architecture.md
├── Dockerfile                   # 多阶段：node 构建 web → go build → alpine 运行
├── docker-compose.yml
├── .dockerignore
├── .env.example
├── .gitignore
├── README.md
└── go.mod
```

### 分层规则

- `cmd` 只做组装，不含业务逻辑
- `internal/*` 单向依赖：`server → {auth, ossclient, localfs, transfer} → config`，禁止反向
- `transfer` 依赖 `ossclient` 的传输封装，不直接碰 SDK 类型之外的细节
- 所有包可独立单测（`ossclient` 通过接口隔离，测试用 fake）

## 3. 关键设计决策

### 3.1 OSS SDK：V2（不是 V1）

- 模块：`github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss`（[官方手册](https://help.aliyun.com/zh/oss/developer-reference/manual-for-go-sdk-v2/)）
- 默认 V4 签名，**Region 必填**；Endpoint 不填时按 Region 推导公网域名
- 凭证：`credentials.NewStaticCredentialsProvider(ak, sk)`（后续可扩展 ECSRAMRole 免密）
- 客户端配置：按 PRD 配置项映射 `WithEndpoint` / `WithUseInternalEndpoint`，超时与重试用默认值起步

### 3.2 分片传输与断点续传

- 使用 SDK V2 的高级封装 **Uploader / Downloader**（`oss.NewUploader(client, …)` / `oss.NewDownloader(client, …)`）：
  - `UploadFile(ctx, request, filePath)` / `DownloadFile(ctx, request, filePath)`
  - 选项：分片大小、分片并发数（ParallelNum）、`EnableCheckpoint` + checkpoint 目录
- checkpoint 文件命名：由 SDK 管理，目录统一为 `CHECKPOINT_DIR`；任务重试即新建 Job 调同一文件，SDK 命中 checkpoint 自动续传
- 进度：SDK V2 的进度回调（operation option `WithProgressFn`）按增量字节累加进 Job；前端用相邻两次轮询的字节差/时间差算瞬时速度
- 取消：每个传输中的文件持有独立 `context`，cancel 时取消 context；SDK 中止当前分片后退出，checkpoint 已落盘，retry 可续传

### 3.3 传输任务系统

```
POST /api/transfer ──► Manager.Submit(job)
                          │ items 入队（每文件一个 item）
                          ▼
                 worker pool (N = TRANSFER_FILE_CONCURRENCY)
                          │ 每 worker：取 item → 预取大小 → Uploader/Downloader → 更新进度
                          ▼
                 Job 状态聚合：全部 done→done；任一失败→error（其余继续）；cancel→cancelled
```

- 任务与 item 两级模型；进度 = Σitem 已传字节 / Σitem 总字节
- 任务存内存 ring buffer（上限 200），并发访问用 `sync.RWMutex`
- 文件夹递归展开在 Submit 时同步做（OSS 侧分页列举、本地侧遍历），超大目录后续可改异步

### 3.4 鉴权

- session：登录成功生成 32 字节随机 token（hex），内存 map `token → {user, expiry}`，cookie `HttpOnly; SameSite=Lax; Path=/`，24h 滑动过期
- 中间件：所有 `/api/*`（除 login）校验 cookie；静态首页不拦截，由前端 401 感知切登录视图
- 登录限流：按来源 IP 令牌桶（10 次/分钟）

### 3.5 前端

- Vite + React + TypeScript，**不引入 UI 组件库**（双栏文件管理器交互简单，自定义 CSS 保持产物小、构建快、无依赖风险）
- 无路由库：`App.tsx` 按登录态切换视图
- 状态管理：组件内 `useState` + 轮询 hooks（任务面板 2s 轮询 `/api/jobs`）
- 开发模式：`vite dev` + `server.proxy` 把 `/api` 转发到 Go 后端；生产：`vite build` → `web/dist` 被 embed

### 3.6 Docker 交付

- Dockerfile 三阶段：`node:22` 构建前端 → `golang:1.27`（`GOPROXY=https://goproxy.cn,direct`）编译 → `alpine` + `ca-certificates` 运行，非 root 用户
- compose：端口、环境变量、`./data:/data` 挂载示例（注释说明可挂载多个目录到 `/data` 子路径）、`restart: unless-stopped`

## 4. 接口隔离（便于测试）

```go
// internal/ossclient 对外暴露接口而非 SDK 具体类型；
// 对象操作方法显式携带 bucket 参数，不绑定单一 bucket。
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
```

`transfer` 与 `server` 只依赖该接口；单测用内存 fake 实现，无需真实 OSS。

### 4.1 多地域 bucket 的客户端路由

V4 签名要求请求 region 与 bucket 实际地域一致。`ossclient` 内部维护
region → {client, uploader, downloader} 缓存与 bucket → region 缓存
（由 `ListBuckets` 结果填充，未知时用 `GetBucketLocation` 探测）；
对某 bucket 的操作自动路由到其所在 region 的客户端，调用方无感知。

## 5. 风险与对策

| 风险 | 对策 |
|---|---|
| SDK V2 Uploader/Downloader 的 checkpoint 行为与预期不符 | M2 先做最小验证（大文件传到一半 kill 进程，重传观察是否续传），不符则自管分片（InitiateMultipartUpload/UploadPart/ListParts） |
| 递归删除 OSS 前缀误删 | 前端二次确认 + 后端要求显式 `recursive: true` |
| 任务进度轮询频率高导致锁竞争 | 进度写用原子量/细粒度锁，读快照拷贝 |
| 内网/公网 Endpoint 配错 | 启动自检（设了 OSS_BUCKET 则对该 bucket 做一次 List，否则 ListBuckets）失败只告警不退出，日志给出排查提示；亦可用 `SKIP_OSS_CHECK=true` 跳过 |
