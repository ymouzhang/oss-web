import { ReactNode } from "react";
import BucketList from "./BucketList";
import ObjectList from "./ObjectList";

interface Props {
  bucket: string; // 空串表示处于 bucket 列表视图
  onBucketChange: (b: string) => void;
  prefix: string;
  onPrefixChange: (p: string) => void;
  selection: string[];
  onSelectionChange: (ids: string[]) => void;
}

// OssPane 编排 OSS 侧两级视图：bucket 列表 ↔ bucket 内对象列表。
// 面包屑「bucket列表 / bucket名 / 前缀…」由本组件统一生成，
// 作为 prop 交给子视图渲染在各自的工具栏之下，保证任意一级可点击回退。
export default function OssPane({
  bucket,
  onBucketChange,
  prefix,
  onPrefixChange,
  selection,
  onSelectionChange,
}: Props) {
  const segments = prefix ? prefix.split("/").filter(Boolean) : [];
  const crumbs: { label: string; onClick: () => void }[] = [
    {
      label: "bucket 列表",
      onClick: () => {
        onBucketChange("");
        onPrefixChange("");
        onSelectionChange([]);
      },
    },
  ];
  if (bucket) {
    crumbs.push({ label: bucket, onClick: () => onPrefixChange("") });
    segments.forEach((s, i) => {
      crumbs.push({
        label: s,
        onClick: () => onPrefixChange(segments.slice(0, i + 1).join("/") + "/"),
      });
    });
  }

  const breadcrumb: ReactNode = (
    <nav className="breadcrumb">
      {crumbs.map((c, i) => (
        <span key={i}>
          {i > 0 && <span className="sep">/</span>}
          <a onClick={c.onClick}>{c.label}</a>
        </span>
      ))}
    </nav>
  );

  return (
    <section className="pane">
      {bucket === "" ? (
        <BucketList
          breadcrumb={breadcrumb}
          onEnter={(b) => {
            onBucketChange(b);
            onPrefixChange("");
            onSelectionChange([]);
          }}
        />
      ) : (
        <ObjectList
          breadcrumb={breadcrumb}
          bucket={bucket}
          prefix={prefix}
          onPrefixChange={onPrefixChange}
          selection={selection}
          onSelectionChange={onSelectionChange}
        />
      )}
    </section>
  );
}
