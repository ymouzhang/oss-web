# OSS Web 传输工具 — 产品需求文档（PRD）

版本：v1.0 草案
日期：2026-10-01

## 1. 背景与目标

### 1.1 背景

用户在本地电脑（电脑 A）与远程 Linux 服务器之间传输数据很慢（SSH/SFTP 直连带宽差）。已有方案是用阿里云 OSS 做中转"网盘"：电脑 A 用 ossbrowser 上传到 OSS，服务器再下载。但服务器上使用 ossutil CLI 操作不直观，缺少图形化工具。

### 1.2 目标

提供一个**自托管的网页工具**，部署在服务器（或任意机器）上，通过浏览器完成：

- 浏览 OSS bucket 中的文件（目录式导航）
- 浏览本机（容器挂载目录）文件系统
- 双向传输：本地 ↔ OSS，**分片并发 + 断点续传**，解决大文件传输慢、易中断的问题

同一份程序既可以跑在服务器上（服务器 ↔ OSS），也可以跑在电脑 A 上（电脑 A ↔ OSS），两端体验一致。

### 1.3 成功标准

- 大文件（GB 级）传输跑满可用带宽；中途断网/服务重启后可从断点继续
- 部署只需 `docker compose up -d` 一条命令
- 非技术用户无需记任何命令即可完成上传下载

## 2. 用户与场景

| 场景 | 部署位置 | 使用方式 |
|---|---|---|
| 服务器 ↔ OSS | 服务器（Docker Compose） | 浏览器访问服务器 IP:端口 |
| 电脑 A ↔ OSS | 电脑 A（二进制或 Docker） | 浏览器访问 localhost |

用户是单一管理员（工具作者本人及少量同事），无多租户需求。

## 3. 功能需求

### FR-1 登录认证（P0）

- 独立登录页，账号密码由部署者通过环境变量设置（`OSS_WEB_USER` / `OSS_WEB_PASSWORD`），**不使用阿里云账号**
- 登录成功后签发 session cookie（HttpOnly、SameSite=Lax、24h 过期），内存态存储
- 所有 API 和页面鉴权；未登录访问 API 返回 401，前端自动跳登录视图
- 提供退出登录
- 登录接口做简单限流（如每分钟 10 次），防爆破

### FR-2a OSS bucket 浏览（P0）

- 登录后 OSS 侧首先列出当前账号下的所有 bucket（`ListBuckets`，含地域、存储类型、创建时间）
- 点击 bucket 进入其文件列表；面包屑「bucket列表 / bucket名 / 前缀…」任意一级可点击回退
- bucket 模块与 bucket 内对象模块边界清晰，接口与前端组件均可独立演进
- 支持按名称搜索过滤 bucket

### FR-2b OSS 对象浏览（P0）

- 目录式导航：`ListObjectsV2` + `Delimiter="/"`，前缀面包屑
- 列表字段：名称、大小、修改时间；目录在前
- 分页加载（每页 100 条，"加载更多"）
- 支持：新建文件夹（空对象占位）、删除文件/文件夹（删除前缀下所有对象需二次确认）、按名称前缀搜索过滤
- 刷新当前目录
- 不同地域的 bucket 自动路由到对应 region 的客户端（V4 签名要求）

### FR-3 本地文件系统浏览（P0）

- 浏览根目录限定为 `LOCAL_ROOT`（容器内挂载点，默认 `/data`）
- 列表字段：名称、大小、修改时间、是否目录；目录在前，支持排序
- **安全约束**：解析符号链接后路径必须仍在 `LOCAL_ROOT` 内，越界返回 403
- 支持新建文件夹、删除（二次确认）

### FR-4 传输任务（P0，核心）

- 双栏选择文件后发起传输：
  - 上传：本地选中文件/文件夹 → OSS 目标前缀
  - 下载：OSS 选中文件/文件夹 → 本地目标目录
  - 选中文件夹时递归展开为文件列表
- **分片并发传输**：使用 OSS Go SDK V2 的 Uploader/Downloader
  - 分片大小默认 8MB（可配置）
  - 单文件分片并发数默认 4（可配置）
  - 文件级并发：同时传输最多 2 个文件（可配置）
- **断点续传**：开启 SDK checkpoint，checkpoint 文件存于 `CHECKPOINT_DIR`（默认 `LOCAL_ROOT/.oss-checkpoints`）
  - 任务失败/取消后可"重试"，SDK 自动从断点继续
  - 服务重启后 checkpoint 仍在磁盘，重新发起同一传输即可续传
- **进度与速度**：任务面板实时显示每个任务的百分比、已传/总字节、瞬时速度、ETA
- **取消**：取消进行中的任务（当前分片完成后停止，checkpoint 保留）
- 任务状态机：`pending → running → done / error / cancelled`
- 任务记录内存态保存（上限 200 条，超出淘汰最旧），重启清空列表但 checkpoint 不丢

### FR-5 浏览器 ↔ OSS 直传（P1）

- **浏览器上传**：拖拽/选择文件 → 经服务器中转流式上传到 OSS（`PutObject`），适合临时小文件
- **浏览器下载**：单个 OSS 文件生成**预签名 URL**（15 分钟有效）直接下载，不占服务器带宽

### FR-6 任务通知（P2，可裁剪）

- 任务完成/失败时前端轮询即可感知，不做系统级通知

## 4. 非功能需求

### 4.1 性能

- 传输吞吐优先：分片并发 + 文件并发，支持阿里云 ECS 同地域**内网 Endpoint**（免流量费、更快），配置项一键切换
- 大目录递归展开需异步进行并显示进度（避免长时间无响应）
- 内存占用有界：任何情况下不将整个文件读入内存（全部流式/分片）

### 4.2 安全

- 凭证只通过环境变量注入，仓库与镜像中不出现任何密钥；提供 `.env.example` 模板
- session token 加密随机生成；密码比较使用 constant-time
- 本地文件访问严格限制在 `LOCAL_ROOT`
- 安全响应头（X-Content-Type-Options、X-Frame-Options 等）
- README 明确提示：不要裸暴露公网，建议前置 HTTPS 反代或仅内网监听

### 4.3 可用性与部署

- 单容器交付：Docker 多阶段构建（Node 构建前端 → Go 编译 → Alpine 运行）
- `docker compose up -d` 一键部署；也可直接 `go build` 出单二进制裸机运行
- 前端产物 `go:embed` 进二进制，无外部静态文件依赖

### 4.4 兼容性

- 浏览器：现代 Chrome / Edge / Firefox 最新两个大版本
- Go 1.27；OSS Go SDK V2（`github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss`，V4 签名）

## 5. 配置项

| 环境变量 | 必填 | 默认 | 说明 |
|---|---|---|---|
| `OSS_WEB_USER` | 是 | — | 登录用户名 |
| `OSS_WEB_PASSWORD` | 是 | — | 登录密码 |
| `OSS_ACCESS_KEY_ID` | 是 | — | RAM 用户 AK（建议最小权限，仅授权目标 bucket） |
| `OSS_ACCESS_KEY_SECRET` | 是 | — | RAM 用户 SK |
| `OSS_REGION` | 是 | — | Bucket 所在地域，如 `cn-hangzhou`（V4 签名必填） |
| `OSS_BUCKET` | 否 | — | 默认选中的 bucket（前端进入时预选）；不填则先展示 bucket 列表 |
| `OSS_ENDPOINT` | 否 | 按 Region 推导公网域名 | 自定义 Endpoint；填内网域名可提速降本 |
| `OSS_USE_INTERNAL_ENDPOINT` | 否 | `false` | `true` 时使用内网域名（ECS 同地域场景） |
| `LOCAL_ROOT` | 否 | `/data` | 本地文件浏览根目录 |
| `CHECKPOINT_DIR` | 否 | `$LOCAL_ROOT/.oss-checkpoints` | 断点续传 checkpoint 目录 |
| `TRANSFER_PART_SIZE_MB` | 否 | `8` | 分片大小（MB） |
| `TRANSFER_PARALLEL_NUM` | 否 | `4` | 单文件分片并发数 |
| `TRANSFER_FILE_CONCURRENCY` | 否 | `2` | 同时传输的文件数 |
| `PORT` | 否 | `8080` | HTTP 监听端口 |

## 6. API 设计（概要）

统一前缀 `/api`，除 `/api/login` 外均需 session。错误格式：`{"error": "message"}`。

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/login` | 登录，设置 cookie |
| POST | `/api/logout` | 退出 |
| GET | `/api/oss/buckets` | 列出账号下所有 bucket（名称/地域/存储类型/创建时间） |
| GET | `/api/oss/list?bucket=&prefix=&continuation-token=` | 列 OSS 目录 |
| POST | `/api/oss/mkdir` | 新建 OSS 文件夹（body 含 bucket） |
| DELETE | `/api/oss/object` | 删除对象 / 递归删除前缀（body 含 bucket） |
| POST | `/api/oss/presign` | 生成下载预签名 URL（body 含 bucket） |
| GET | `/api/local/list?path=` | 列本地目录 |
| POST | `/api/local/mkdir` / DELETE `/api/local` | 本地新建/删除 |
| POST | `/api/transfer` | 创建传输任务（direction + bucket + 文件清单） |
| GET | `/api/jobs` / `/api/jobs/{id}` | 任务列表 / 详情（含进度、bucket） |
| POST | `/api/jobs/{id}/cancel` | 取消 |
| POST | `/api/jobs/{id}/retry` | 重试（断点续传） |
| POST | `/api/browser-upload?bucket=&prefix=` | 浏览器文件中转上传 |

> 除 `/api/oss/buckets` 外，所有 OSS 对象相关 API 均必须携带 bucket 参数（query 或 body），缺失返回 400。

## 7. 界面设计（概要）

单页应用，两个视图：

1. **登录视图**：居中卡片，用户名/密码/登录按钮
2. **主视图**：
   - 顶栏：产品名、退出登录
   - 左右双栏：左「本地文件」，右「OSS」，各含路径面包屑、工具栏（刷新/新建文件夹/删除/搜索）
   - OSS 栏两级视图：bucket 列表（卡片/表格）↔ bucket 内对象列表，面包屑「bucket列表 / bucket名 / 前缀…」任意一级可回退
   - 中部传输按钮：「上传到 OSS →」「← 下载到本地」
   - 底部任务面板（可折叠）：任务列表 + 进度条 + 速度 + 取消/重试
   - 文件拖拽到 OSS 栏触发浏览器直传

## 8. 边界与异常

- **网络中断**：SDK 分片重试 + checkpoint；任务置为 error，用户点重试续传
- **OSS 报错**：透出 SDK 错误信息（含 EC 错误码）到任务面板
- **磁盘写满**：下载任务 error 并提示
- **同名冲突**：默认覆盖（checkpoint 保证一致性）；后续版本可选"跳过/重命名"
- **空文件夹**：OSS 端用 `/` 结尾空对象表示；传输时递归展开仅含真实文件
- **凭证错误**：启动时调用一次 `ListObjectsV2` 做连通性自检，失败则在日志明确报错并退出

## 9. 范围外（本期不做）

- 多账号切换（多 bucket 已支持）
- 多用户、权限分级
- 文件在线预览、编辑
- 分享链接
- STS 临时授权下发给浏览器直传（如需，后续迭代）

## 10. 里程碑

1. M1：后端骨架（配置、鉴权、OSS/本地列表 API）+ 前端双栏浏览
2. M2：传输任务系统（分片、并发、进度、取消/重试）+ 任务面板
3. M3：浏览器直传/预签名下载、Docker 交付、文档完善、端到端验证

## 参考资料

- [OSS Go SDK V2 使用手册](https://help.aliyun.com/zh/oss/developer-reference/manual-for-go-sdk-v2/)
- [OSS API 按功能列表](https://help.aliyun.com/zh/oss/developer-reference/list-of-operations-by-function)
