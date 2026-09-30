import { useEffect, useRef, useState } from "react";
import { api, formatBytes, Job } from "../api";

const STATUS_TEXT: Record<string, string> = {
  pending: "排队中",
  running: "进行中",
  done: "完成",
  error: "失败",
  cancelled: "已取消",
};

export default function JobsPanel({ refreshKey }: { refreshKey: number }) {
  const [jobs, setJobs] = useState<Job[]>([]);
  const [collapsed, setCollapsed] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [detail, setDetail] = useState<Job | null>(null);
  const prevRef = useRef<Map<string, { transferred: number; at: number }>>(new Map());
  const [speeds, setSpeeds] = useState<Map<string, number>>(new Map());

  const toggleExpand = async (id: string) => {
    if (expanded === id) {
      setExpanded(null);
      setDetail(null);
      return;
    }
    setExpanded(id);
    try {
      setDetail(await api.job(id));
    } catch {
      setDetail(null);
    }
  };

  useEffect(() => {
    let alive = true;
    const tick = async () => {
      try {
        const { jobs } = await api.jobs();
        if (!alive) return;
        const now = Date.now();
        const next = new Map(speeds);
        for (const j of jobs) {
          const prev = prevRef.current.get(j.id);
          if (prev && j.status === "running") {
            const dt = (now - prev.at) / 1000;
            if (dt > 0) next.set(j.id, Math.max(0, (j.transferred - prev.transferred) / dt));
          } else if (j.status !== "running") {
            next.delete(j.id);
          }
          prevRef.current.set(j.id, { transferred: j.transferred, at: now });
        }
        setSpeeds(next);
        setJobs(jobs);
      } catch {
        /* 401 由全局 handler 统一处理 */
      }
    };
    tick();
    const timer = setInterval(tick, 2000);
    return () => {
      alive = false;
      clearInterval(timer);
    };
    // speeds map 不列入依赖；在 tick 内部更新
  }, [refreshKey]);

  const activeCount = jobs.filter((j) => j.status === "running" || j.status === "pending").length;

  return (
    <footer className={`jobs-panel ${collapsed ? "collapsed" : ""}`}>
      <div className="jobs-header" onClick={() => setCollapsed(!collapsed)}>
        <span>
          传输任务{activeCount > 0 ? `（${activeCount} 个进行中）` : ""}
        </span>
        <span>{collapsed ? "▲ 展开" : "▼ 收起"}</span>
      </div>
      {!collapsed && (
        <div className="jobs-body">
          {jobs.length === 0 && <div className="empty">暂无任务</div>}
          {jobs.map((j) => {
            const pct = j.total > 0 ? Math.min(100, (j.transferred / j.total) * 100) : j.status === "done" ? 100 : 0;
            const speed = speeds.get(j.id) ?? 0;
            const eta =
              j.status === "running" && speed > 0
                ? Math.round((j.total - j.transferred) / speed)
                : null;
            return (
              <div key={j.id} className="job-row">
                <span className="job-dir">
                  {j.direction === "upload" ? "上传" : "下载"} · {j.bucket}
                </span>
                <div className="job-main">
                  <div className="job-bar">
                    <div
                      className={`job-bar-fill status-${j.status}`}
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <div className="job-meta">
                    <span>{pct.toFixed(1)}%</span>
                    <span>
                      {formatBytes(j.transferred)} / {formatBytes(j.total)}
                    </span>
                    {j.status === "running" && speed > 0 && <span>{formatBytes(speed)}/s</span>}
                    {eta !== null && <span>剩余 ~{eta}s</span>}
                    <span className={`status-text status-${j.status}`}>{STATUS_TEXT[j.status] ?? j.status}</span>
                  </div>
                  {j.error && <div className="error-text">{j.error}</div>}
                  {expanded === j.id && detail?.items && (
                    <div className="job-items">
                      {detail.items.map((it, i) => (
                        <div key={i} className="job-item">
                          <span className={`status-text status-${it.status}`}>
                            {STATUS_TEXT[it.status] ?? it.status}
                          </span>
                          <span className="job-item-name" title={`${it.src} → ${it.dst}`}>
                            {it.src}
                          </span>
                          <span>
                            {formatBytes(it.transferred)} / {formatBytes(it.size)}
                          </span>
                          {it.error && <span className="error-text">{it.error}</span>}
                        </div>
                      ))}
                    </div>
                  )}
                </div>
                <div className="job-actions">
                  {(j.status === "running" || j.status === "pending") && (
                    <button className="btn btn-small" onClick={() => api.cancelJob(j.id)}>
                      取消
                    </button>
                  )}
                  {(j.status === "error" || j.status === "cancelled") && (
                    <button className="btn btn-small" onClick={() => api.retryJob(j.id)}>
                      重试
                    </button>
                  )}
                  <button className="btn btn-small" onClick={() => toggleExpand(j.id)}>
                    {expanded === j.id ? "收起明细" : "明细"}
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </footer>
  );
}
