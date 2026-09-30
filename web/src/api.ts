export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

let onUnauthorized: () => void = () => {};
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(path, init);
  if (res.status === 401) {
    onUnauthorized();
    throw new ApiError(401, "unauthorized");
  }
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const data = await res.json();
      if (data && data.error) msg = data.error;
    } catch {
      /* 保留默认错误信息 */
    }
    throw new ApiError(res.status, msg);
  }
  return res.json() as Promise<T>;
}

function post<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export interface BucketInfo {
  name: string;
  region: string;
  location: string;
  storageClass: string;
  createdAt: string;
}

export interface OssEntry {
  key: string;
  name: string;
  isDir: boolean;
  size: number;
  lastModified: string;
}

export interface OssListResult {
  prefix: string;
  entries: OssEntry[];
  isTruncated: boolean;
  nextToken?: string;
}

export interface LocalEntry {
  name: string;
  path: string;
  isDir: boolean;
  size: number;
  modTime: string;
}

export interface JobItem {
  bucket: string;
  src: string;
  dst: string;
  size: number;
  transferred: number;
  status: string;
  error?: string;
}

export interface Job {
  id: string;
  bucket: string;
  direction: "upload" | "download";
  status: "pending" | "running" | "done" | "error" | "cancelled";
  total: number;
  transferred: number;
  error?: string;
  createdAt: string;
  items?: JobItem[];
}

export interface AppConfig {
  defaultBucket: string;
  region: string;
  localRoot: string;
}

export const api = {
  login: (username: string, password: string) =>
    post<{ user: string }>("/api/login", { username, password }),
  logout: () => request<{ status: string }>("/api/logout", { method: "POST" }),
  config: () => request<AppConfig>("/api/config"),

  // bucket 模块
  listBuckets: () =>
    request<{ buckets: BucketInfo[]; defaultBucket: string }>("/api/oss/buckets"),

  // bucket 内对象模块
  ossList: (bucket: string, prefix: string, token = "") =>
    request<OssListResult>(
      `/api/oss/list?bucket=${encodeURIComponent(bucket)}&prefix=${encodeURIComponent(prefix)}&continuation-token=${encodeURIComponent(token)}`
    ),
  ossMkdir: (bucket: string, prefix: string) =>
    post<{ status: string }>("/api/oss/mkdir", { bucket, prefix }),
  ossDelete: (bucket: string, keys: string[], recursive: boolean) =>
    request<{ status: string }>("/api/oss/object", {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ bucket, keys, recursive }),
    }),
  ossPresign: (bucket: string, key: string) =>
    post<{ url: string }>("/api/oss/presign", { bucket, key }),

  // 本地文件模块
  localList: (path: string) =>
    request<{ entries: LocalEntry[] }>(`/api/local/list?path=${encodeURIComponent(path)}`),
  localMkdir: (path: string) => post<{ status: string }>("/api/local/mkdir", { path }),
  localDelete: (paths: string[]) =>
    request<{ status: string }>("/api/local", {
      method: "DELETE",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ paths }),
    }),

  // 传输任务模块
  transfer: (body: {
    direction: "upload" | "download";
    bucket: string;
    localPaths?: string[];
    ossPrefix?: string;
    ossKeys?: string[];
    localDir?: string;
  }) => post<Job>("/api/transfer", body),
  jobs: () => request<{ jobs: Job[] }>("/api/jobs"),
  job: (id: string) => request<Job>(`/api/jobs/${id}`),
  cancelJob: (id: string) =>
    request<{ status: string }>(`/api/jobs/${id}/cancel`, { method: "POST" }),
  retryJob: (id: string) =>
    request<{ status: string }>(`/api/jobs/${id}/retry`, { method: "POST" }),

  // 浏览器直传（服务器中转流式 PutObject）
  browserUpload: async (bucket: string, prefix: string, file: File): Promise<{ key: string }> => {
    const res = await fetch(
      `/api/browser-upload?bucket=${encodeURIComponent(bucket)}&prefix=${encodeURIComponent(prefix)}&filename=${encodeURIComponent(file.name)}`,
      { method: "POST", body: file }
    );
    if (res.status === 401) {
      onUnauthorized();
      throw new ApiError(401, "unauthorized");
    }
    if (!res.ok) {
      const data = await res.json().catch(() => null);
      throw new ApiError(res.status, data?.error ?? `HTTP ${res.status}`);
    }
    return res.json();
  },
};

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n;
  let u = -1;
  do {
    v /= 1024;
    u++;
  } while (v >= 1024 && u < units.length - 1);
  return `${v.toFixed(1)} ${units[u]}`;
}

export function formatTime(iso: string): string {
  if (!iso || iso.startsWith("0001-")) return "-";
  const d = new Date(iso);
  return d.toLocaleString();
}
