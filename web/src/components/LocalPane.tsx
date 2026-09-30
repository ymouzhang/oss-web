import { useCallback, useEffect, useState } from "react";
import { api, LocalEntry } from "../api";
import FileTable, { Row } from "./FileTable";

interface Props {
  path: string;
  onPathChange: (p: string) => void;
  selection: string[];
  onSelectionChange: (ids: string[]) => void;
}

export default function LocalPane({ path, onPathChange, selection, onSelectionChange }: Props) {
  const [entries, setEntries] = useState<LocalEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");

  const refresh = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const res = await api.localList(path);
      setEntries(res.entries ?? []);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [path]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const segments = path ? path.split("/").filter(Boolean) : [];
  const crumbs = [{ label: "本地根目录", value: "" }].concat(
    segments.map((s, i) => ({ label: s, value: segments.slice(0, i + 1).join("/") }))
  );

  const rows: Row[] = entries.map((e) => ({
    id: e.path,
    name: e.name,
    isDir: e.isDir,
    size: e.size,
    time: e.modTime,
  }));

  const mkdir = async () => {
    const name = prompt("新建文件夹名称：");
    if (!name) return;
    try {
      await api.localMkdir(path ? `${path}/${name}` : name);
      refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const remove = async () => {
    const targets = entries.filter((e) => selection.includes(e.path));
    if (targets.length === 0) return;
    if (!confirm(`确认删除 ${targets.length} 个本地文件/文件夹？此操作不可恢复。`)) return;
    try {
      await api.localDelete(targets.map((t) => t.path));
      onSelectionChange([]);
      refresh();
    } catch (e) {
      setError((e as Error).message);
    }
  };

  return (
    <section className="pane">
      <div className="pane-header">
        <h2>本地文件</h2>
        <div className="toolbar">
          <button className="btn" onClick={refresh} title="刷新">
            ⟳
          </button>
          <button className="btn" onClick={mkdir}>
            新建文件夹
          </button>
          <button className="btn btn-danger" onClick={remove} disabled={selection.length === 0}>
            删除
          </button>
          <input
            className="filter"
            placeholder="搜索…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
      </div>
      <nav className="breadcrumb">
        {crumbs.map((c, i) => (
          <span key={c.value}>
            {i > 0 && <span className="sep">/</span>}
            <a onClick={() => onPathChange(c.value)}>{c.label}</a>
          </span>
        ))}
      </nav>
      {error && <div className="error-text">{error}</div>}
      <div className="pane-body">
        <FileTable
          rows={rows}
          selection={selection}
          onSelectionChange={onSelectionChange}
          onOpen={(r) => onPathChange(r.id)}
          loading={loading}
          filter={filter}
        />
      </div>
    </section>
  );
}
