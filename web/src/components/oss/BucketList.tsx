import { ReactNode, useEffect, useState } from "react";
import { api, BucketInfo, formatTime } from "../../api";

interface Props {
  breadcrumb: ReactNode;
  onEnter: (bucket: string) => void;
}

// BucketList 是 OSS 侧的第一级视图：列出账号下所有 bucket，点击进入对象列表。
export default function BucketList({ breadcrumb, onEnter }: Props) {
  const [buckets, setBuckets] = useState<BucketInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");

  const refresh = async () => {
    setLoading(true);
    setError("");
    try {
      const res = await api.listBuckets();
      setBuckets(res.buckets ?? []);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    refresh();
  }, []);

  const visible = filter
    ? buckets.filter((b) => b.name.toLowerCase().includes(filter.toLowerCase()))
    : buckets;

  return (
    <>
      <div className="pane-header">
        <h2>OSS Buckets</h2>
        <div className="toolbar">
          <button className="btn" onClick={refresh} title="刷新">
            ⟳
          </button>
          <input
            className="filter"
            placeholder="搜索 bucket…"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
          />
        </div>
      </div>
      {breadcrumb}
      {error && <div className="error-text">{error}</div>}
      <div className="pane-body">
        <table className="file-table">
          <thead>
            <tr>
              <th>名称</th>
              <th className="col-region">地域</th>
              <th className="col-storage">存储类型</th>
              <th className="col-time">创建时间</th>
            </tr>
          </thead>
          <tbody>
            {visible.map((b) => (
              <tr key={b.name} onDoubleClick={() => onEnter(b.name)}>
                <td className="col-name" onClick={() => onEnter(b.name)}>
                  <span className="file-icon">🪣</span>
                  <span className="file-name bucket-link">{b.name}</span>
                </td>
                <td className="col-region">{b.region}</td>
                <td className="col-storage">{b.storageClass}</td>
                <td className="col-time">{formatTime(b.createdAt)}</td>
              </tr>
            ))}
            {!loading && visible.length === 0 && (
              <tr>
                <td colSpan={4} className="empty">
                  没有 bucket
                </td>
              </tr>
            )}
            {loading && (
              <tr>
                <td colSpan={4} className="empty">
                  加载中…
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </>
  );
}
