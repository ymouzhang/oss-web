import { useCallback, useEffect, useState } from "react";
import { api, setUnauthorizedHandler, AppConfig } from "./api";
import LoginView from "./components/LoginView";
import LocalPane from "./components/LocalPane";
import OssPane from "./components/oss/OssPane";
import JobsPanel from "./components/JobsPanel";

export default function App() {
  const [authed, setAuthed] = useState<boolean | null>(null);
  const [cfg, setCfg] = useState<AppConfig | null>(null);
  const [error, setError] = useState("");

  const [localSel, setLocalSel] = useState<string[]>([]);
  const [ossSel, setOssSel] = useState<string[]>([]);
  const [localPath, setLocalPath] = useState("");
  const [ossBucket, setOssBucket] = useState(""); // 空串 = bucket 列表视图
  const [ossPrefix, setOssPrefix] = useState("");
  const [jobsKey, setJobsKey] = useState(0);

  useEffect(() => {
    setUnauthorizedHandler(() => setAuthed(false));
    api
      .config()
      .then((c) => {
        setCfg(c);
        setOssBucket(c.defaultBucket);
        setAuthed(true);
      })
      .catch(() => setAuthed(false));
  }, []);

  const onLogin = useCallback(() => {
    api.config().then((c) => {
      setCfg(c);
      setOssBucket(c.defaultBucket);
      setAuthed(true);
    });
  }, []);

  const doTransfer = async (direction: "upload" | "download") => {
    setError("");
    try {
      if (direction === "upload") {
        if (!ossBucket) {
          setError("请先在右侧选择一个 bucket");
          return;
        }
        if (localSel.length === 0) {
          setError("请先在左侧选择要上传的本地文件/文件夹");
          return;
        }
        await api.transfer({ direction, bucket: ossBucket, localPaths: localSel, ossPrefix });
        setLocalSel([]);
      } else {
        if (!ossBucket) {
          setError("请先在右侧选择一个 bucket");
          return;
        }
        if (ossSel.length === 0) {
          setError("请先在右侧选择要下载的 OSS 文件/文件夹");
          return;
        }
        await api.transfer({ direction, bucket: ossBucket, ossKeys: ossSel, localDir: localPath });
        setOssSel([]);
      }
      setJobsKey((k) => k + 1);
    } catch (e) {
      setError((e as Error).message);
    }
  };

  const logout = async () => {
    await api.logout().catch(() => {});
    setAuthed(false);
  };

  if (authed === null) return <div className="loading">加载中…</div>;
  if (!authed) return <LoginView onLogin={onLogin} />;

  return (
    <div className="app">
      <header className="topbar">
        <span className="brand">OSS Web 传输工具</span>
        {cfg && <span className="bucket-info">默认 region：{cfg.region}</span>}
        <button className="btn btn-ghost" onClick={logout}>
          退出登录
        </button>
      </header>
      {error && (
        <div className="error-banner" onClick={() => setError("")}>
          {error}（点击关闭）
        </div>
      )}
      <main className="panes">
        <LocalPane
          path={localPath}
          onPathChange={setLocalPath}
          selection={localSel}
          onSelectionChange={setLocalSel}
        />
        <div className="transfer-buttons">
          <button className="btn btn-primary" onClick={() => doTransfer("upload")}>
            上传到 OSS →
          </button>
          <button className="btn btn-primary" onClick={() => doTransfer("download")}>
            ← 下载到本地
          </button>
        </div>
        <OssPane
          bucket={ossBucket}
          onBucketChange={setOssBucket}
          prefix={ossPrefix}
          onPrefixChange={setOssPrefix}
          selection={ossSel}
          onSelectionChange={setOssSel}
        />
      </main>
      <JobsPanel refreshKey={jobsKey} />
    </div>
  );
}
