import { ReactNode } from "react";
import { formatBytes, formatTime } from "../api";

export interface Row {
  id: string; // 当前列表内的唯一标识（路径或 key）
  name: string;
  isDir: boolean;
  size: number;
  time: string;
}

interface Props {
  rows: Row[];
  selection: string[];
  onSelectionChange: (ids: string[]) => void;
  onOpen: (row: Row) => void;
  loading: boolean;
  filter: string;
  rowActions?: (row: Row) => ReactNode;
}

export default function FileTable({
  rows,
  selection,
  onSelectionChange,
  onOpen,
  loading,
  filter,
  rowActions,
}: Props) {
  const visible = filter
    ? rows.filter((r) => r.name.toLowerCase().includes(filter.toLowerCase()))
    : rows;

  const toggle = (id: string) => {
    if (selection.includes(id)) {
      onSelectionChange(selection.filter((s) => s !== id));
    } else {
      onSelectionChange([...selection, id]);
    }
  };

  const allChecked = visible.length > 0 && visible.every((r) => selection.includes(r.id));
  const toggleAll = () => {
    if (allChecked) {
      onSelectionChange(selection.filter((s) => !visible.some((r) => r.id === s)));
    } else {
      const merged = new Set([...selection, ...visible.map((r) => r.id)]);
      onSelectionChange([...merged]);
    }
  };

  return (
    <table className="file-table">
      <thead>
        <tr>
          <th className="col-check">
            <input type="checkbox" checked={allChecked} onChange={toggleAll} />
          </th>
          <th>名称</th>
          <th className="col-size">大小</th>
          <th className="col-time">修改时间</th>
          {rowActions && <th className="col-actions">操作</th>}
        </tr>
      </thead>
      <tbody>
        {visible.map((r) => (
          <tr
            key={r.id}
            className={
              (selection.includes(r.id) ? "selected" : "") + (r.isDir ? " dir-row" : "")
            }
            onClick={() => (r.isDir ? onOpen(r) : toggle(r.id))}
          >
            <td className="col-check" onClick={(e) => e.stopPropagation()}>
              <input
                type="checkbox"
                checked={selection.includes(r.id)}
                onChange={() => toggle(r.id)}
              />
            </td>
            <td className="col-name">
              <span className="file-icon">{r.isDir ? "📁" : "📄"}</span>
              <span className="file-name" title={r.name}>
                {r.name}
              </span>
            </td>
            <td className="col-size">{r.isDir ? "-" : formatBytes(r.size)}</td>
            <td className="col-time">{formatTime(r.time)}</td>
            {rowActions && (
              <td className="col-actions" onClick={(e) => e.stopPropagation()}>
                {rowActions(r)}
              </td>
            )}
          </tr>
        ))}
        {!loading && visible.length === 0 && (
          <tr>
            <td colSpan={rowActions ? 5 : 4} className="empty">
              空目录
            </td>
          </tr>
        )}
        {loading && (
          <tr>
            <td colSpan={rowActions ? 5 : 4} className="empty">
              加载中…
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}
