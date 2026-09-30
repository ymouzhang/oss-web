import { ReactNode, useCallback, useEffect, useRef, useState } from "react";
import { api, OssEntry } from "../../api";
import FileTable, { Row } from "../FileTable";

interface Props {
  breadcrumb: ReactNode;
  bucket: string;
  prefix: string;
  onPrefixChange: (p: string) => void;
  selection: string[];
  onSelectionChange: (ids: string[]) => void;
}

// ObjectList 是 bucket 内的对象列表：前缀导航、分页、新建/删除、拖拽上传。
export default function ObjectList({
  breadcrumb,
  bucket,
  prefix,
  onPrefixChange,
  selection,
  onSelectionChange,
}: Props) {
  const [entries, setEntries] = useState<OssEntry[]>([]);
  const [nextToken, setNextToken] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");
  const [dragOver, setDragOver] = useState(false);
  const [uploadNote, setUploadNote] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);

  const load = useCallback(
    async (token: string, append: boolean) => {
      setLoading(true);
      setError("");
      try {
        const res = await api.ossList(bucket, prefix, token);
        setEntries((prev) => (append ? [...prev, ...(res.entries ?? [])] : res.entries ?? []));
        setNextToken(res.nextToken ?? "");
      } catch (e) {
        setError((e as Error).message);
      } finally {
        setLoading(false);
      }
    },
    [bucket, prefix]
  );

  useEffect(() => {
    load("", false);
  }, [load]);

  const rows: Row[] = entries.map((e) => ({
    id: e.key,
    name: e.name,
    isDir: e.isDir,
    size: e.size,
    time: e.lastModified,
  }));

  const mkdir = async () => {
    const name = prompt("新建文件夹名称：");
    if (!name) return;
    try {
      await api.ossMkdir(bucket, prefix + name);
      load("", false);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const remove = async () => {
    const targets = entries.filter((e) => selection.includes(e.key));
    if (targets.length === 0) return;
    const hasDir = targets.some((t) => t.isDir);
    const hint = hasDir
      ? `确认删除 ${targets.length} 个对象？其中包含文件夹，将递归删除其下所有对象，不可恢复。`
      : `确认删除 ${targets.length} 个对象？此操作不可恢复。`;
    if (!confirm(hint)) return;
    try {
      await api.ossDelete(bucket, targets.map((t) => t.key), true);
      onSelectionChange([]);
      load("", false);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const downloadViaUrl = async (row: Row) => {
    try {
      const { url } = await api.ossPresign(bucket, row.id);
      window.open(url, "_blank");
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const uploadFiles = async (files: FileList | File[]) => {
    setUploadNote("");
    for (const f of Array.from(files)) {
      setUploadNote(`正在上传 ${f.name} …`);
      try {
        await api.browserUpload(bucket, prefix, f);
      } catch (e) {
        setError(`上传 ${f.name} 失败：${(e as Error).message}`);
      }
    }
    setUploadNote("");
    load("", false);
  };

  return (
    <div
      className={`object-list ${dragOver ? "drag-over" : ""}`}
      onDragOver={(e) => {
        e.preventDefault();
        setDragOver(true);
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragOver(false);
        if (e.dataTransfer.files.length > 0) uploadFiles(e.dataTransfer.files);
      }}
    >
      <div className="pane-header">
        <h2>OSS · {bucket}</h2>
        <div className="toolbar">
          <button className="btn" onClick={() => load("", false)} title="刷新">
            ⟳
          </button>
          <button className="btn" onClick={mkdir}>
            新建文件夹
          </button>
          <button className="btn btn-danger" onClick={remove} disabled={selection.length === 0}>
            删除
          </button>
          <button className="btn" onClick={() => fileInput.current?.click()}>
            浏览器上传
          </button>
          <input
            ref={fileInput}
            type="file"
            multiple
            hidden
            onChange={(e) => {
              if (e.target.files) uploadFiles(e.target.files);
              e.target.value = "";
            }}
          />
          <input
            className="filter"
            placeholder="搜索…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
      </div>
      {breadcrumb}
      {error && <div className="error-text">{error}</div>}
      {uploadNote && <div className="info-text">{uploadNote}</div>}
      <div className="pane-body">
        <FileTable
          rows={rows}
          selection={selection}
          onSelectionChange={onSelectionChange}
          onOpen={(r) => onPrefixChange(r.id)}
          loading={loading}
          filter={filter}
          rowActions={(row) =>
            row.isDir ? null : (
              <button className="btn btn-small" onClick={() => downloadViaUrl(row)}>
                下载链接
              </button>
            )
          }
        />
        {nextToken && (
          <button className="btn load-more" onClick={() => load(nextToken, true)} disabled={loading}>
            加载更多
          </button>
        )}
      </div>
      <div className="pane-footer">拖拽文件到此面板可直接上传到当前目录</div>
    </div>
  );
}
