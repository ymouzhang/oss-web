# OSS Web 传输工具

自托管的网页工具，在浏览器中完成 **本地文件系统 ↔ 阿里云 OSS bucket** 的双向传输。支持分片并发、断点续传、浏览器拖拽直传和预签名 URL 下载。

典型场景：本地电脑与远程服务器直连传输慢时，用 OSS 做中转"网盘"——一端上传，另一端下载，两端都可以用本工具。

## 功能

- 自建登录页（环境变量配置账号密码），session cookie 鉴权 + 登录限流
- 双栏文件管理器：左栏本地文件（限定 `LOCAL_ROOT`），右栏 OSS（bucket 列表 → bucket 内对象列表两级视图，目录式导航、分页、搜索、新建/删除）
- 传输核心：OSS Go SDK V2 的 Uploader/Downloader
  - 分片 8MB、单文件分片并发 4、文件级并发 2（均可配置）
  - 断点续传：checkpoint 落盘在 `CHECKPOINT_DIR`，任务失败/取消后点「重试」即续传，服务重启后 checkpoint 仍有效
  - 任务面板实时显示进度、速度、ETA，支持取消/重试
- 浏览器拖拽文件到 OSS 面板：经服务器中转流式 `PutObject` 上传
- OSS 文件生成 15 分钟预签名 URL，浏览器直接下载，不占服务器带宽

## 快速开始

### Docker Compose（推荐）

```bash
cp .env.example .env   # 填写账号密码和 OSS 凭证
mkdir -p data          # 本地数据目录（挂载到容器 /data）
docker compose up -d --build
```

访问 `http://服务器IP:8080`。

### 裸机运行

```bash
cd web && npm install && npm run build && cd ..
go build -o oss-web ./cmd/oss-web
export OSS_WEB_USER=admin OSS_WEB_PASSWORD=secret \
       OSS_ACCESS_KEY_ID=xxx OSS_ACCESS_KEY_SECRET=yyy \
       OSS_REGION=cn-hangzhou OSS_BUCKET=my-bucket \
       LOCAL_ROOT=/path/to/data
./oss-web
```

## 配置项

| 环境变量 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `OSS_WEB_USER` | 是 | — | 登录用户名 |
| `OSS_WEB_PASSWORD` | 是 | — | 登录密码 |
| `OSS_ACCESS_KEY_ID` | 是 | — | RAM 用户 AK（建议最小权限，仅授权目标 bucket） |
| `OSS_ACCESS_KEY_SECRET` | 是 | — | RAM 用户 SK |
| `OSS_REGION` | 是 | — | Bucket 地域，如 `cn-hangzhou`（V4 签名必填） |
| `OSS_BUCKET` | 否 | — | 默认选中的 bucket（前端进入时预选）；不填则先展示账号下全部 bucket 列表 |
| `OSS_ENDPOINT` | 否 | 按 Region 推导公网域名 | 自定义 Endpoint |
| `OSS_USE_INTERNAL_ENDPOINT` | 否 | `false` | `true` 时使用内网域名（ECS 同地域免流量费、更快） |
| `LOCAL_ROOT` | 否 | `/data` | 本地文件浏览根目录 |
| `CHECKPOINT_DIR` | 否 | `$LOCAL_ROOT/.oss-checkpoints` | 断点续传 checkpoint 目录 |
| `TRANSFER_PART_SIZE_MB` | 否 | `8` | 分片大小（MB） |
| `TRANSFER_PARALLEL_NUM` | 否 | `4` | 单文件分片并发数 |
| `TRANSFER_FILE_CONCURRENCY` | 否 | `2` | 同时传输的文件数 |
| `PORT` | 否 | `8080` | HTTP 监听端口 |
| `SKIP_OSS_CHECK` | 否 | `false` | `true` 时跳过启动自检；注意自检失败默认只告警不退出 |

## 开发

```bash
# 后端（:8080）
go run ./cmd/oss-web

# 前端热更新（:5173，/api 代理到 :8080）
cd web && npm run dev
```

## 安全提示

- **不要裸暴露公网**。建议仅监听内网，或前置带 HTTPS 的反向代理（nginx/caddy）。当前版本 cookie 未强制 `Secure`，明文 HTTP 在公网会被窃听。
- 登录账号密码、OSS 凭证只通过环境变量注入；请使用**最小权限的 RAM 用户**（仅授权需要访问的 bucket 的读写）。
- 本地文件访问被严格限制在 `LOCAL_ROOT` 内（含符号链接逃逸检测），但仍建议挂载专用数据目录而非系统目录。
- OSS 端的递归删除、本地删除均有二次确认；请留意操作对象。

## 已知限制

- 任务记录为内存态：服务重启后任务列表清空，但磁盘上的 checkpoint 保留，重新发起同一传输即可续传（上限 200 条，超出淘汰最旧）。
- 大目录的递归展开在提交任务时同步进行，超大目录可能等待较久。
- 同名文件默认覆盖，暂无「跳过/重命名」策略。
- 单账号，无多用户/权限分级。

## 项目结构

```
cmd/oss-web        入口：配置加载、依赖组装、优雅退出
internal/config    环境变量配置
internal/auth      登录、session、限流
internal/ossclient OSS SDK V2 封装（Store 接口）
internal/localfs   本地文件系统（根目录约束）
internal/transfer  传输任务系统（队列、worker、进度、取消/重试）
internal/server    HTTP 路由与 handlers
web/               React + TypeScript + Vite 前端（产物 go:embed 进二进制）
docs/              PRD 与架构设计
```
