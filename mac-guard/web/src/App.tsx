import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Activity, AlertTriangle, Archive, BrainCircuit, CheckCircle2, ChevronDown, ChevronRight,
  CircleHelp, Database, FileClock, Globe2, House, Network, Play,
  Power, PowerOff, RefreshCw, RotateCcw, Route, Save, Server, Shield, ShieldCheck, Stethoscope,
  Settings2, Upload, Wifi, XCircle
} from "lucide-react";
import { api } from "./api";
import type {
  BackupMetadata, ComponentStatus, ControllerState, Health,
  ConnectionObservation, LifecycleAction, NodeConfiguration, NodeUpdate, RuntimeComponentConfiguration, Snapshot, Stage, TestResult
} from "./types";

type View = "overview" | "diagnostics" | "node" | "events";

const stateLabels: Record<ControllerState, { label: string; detail: string }> = {
  boot_locked: { label: "正在启动", detail: "等待首轮网络检查" },
  healthy: { label: "网络保护正常", detail: "出口与防泄漏策略均符合预期" },
  degraded_locked: { label: "已安全断网", detail: "线路异常，Kill Switch 正在阻止本地回落" },
  inconsistent_locked: { label: "状态不一致", detail: "保护标记与真实网络状态冲突，正在恢复安全状态" },
  unprotected: { label: "尚未启用断网保护", detail: "当前线路可用，但断线时仍可能回落到本地网络" },
  recovering: { label: "正在恢复", detail: "正在重启网络组件" },
  maintenance: { label: "已安全暂停", detail: "东京隧道已停止，Kill Switch 正在阻止本地公网" }
};

const stageIcons: Record<string, React.ReactNode> = {
  network: <Wifi size={18} />, firewall: <Shield size={18} />,
  tunnel: <Route size={18} />, provider: <Network size={18} />,
  vps: <Server size={18} />,
  dns: <Database size={18} />
};

const stageComponentIDs: Record<string, string[]> = {
  network: ["network_conflict"],
  firewall: ["firewall", "firewall_config", "firewall_binding", "pf_bootstrap_service"],
  tunnel: ["tunnel"],
  provider: ["provider", "provider_service", "config", "controller_service"],
  dns: ["dns", "ipv6"],
  vps: ["transport", "egress"]
};

const componentTestIDs: Record<string, string> = {
  transport: "vps_tcp", egress: "egress", dns: "dns", ipv6: "ipv6"
};

const runtimeConfigIcons: Record<string, React.ReactNode> = {
  firewall: <Shield size={18} />, tunnel: <Route size={18} />, "sing-box": <Network size={18} />,
  dns: <Database size={18} />, outbound: <Globe2 size={18} />, controller: <Stethoscope size={18} />
};

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [view, setView] = useState<View>("overview");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"refresh" | "tests" | "restart" | "start" | "pause" | "">("");
  const scrolling = useRef(false);
  const pendingSnapshot = useRef<Snapshot | null>(null);
  const scrollTimer = useRef<number | null>(null);

  const applySnapshot = useCallback((next: Snapshot, deferWhileScrolling: boolean) => {
    if (deferWhileScrolling && scrolling.current) {
      pendingSnapshot.current = next;
      return;
    }
    setSnapshot(next);
  }, []);

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setBusy("refresh");
    try {
      applySnapshot(await api.snapshot(), quiet);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "状态读取失败");
    } finally {
      if (!quiet) setBusy("");
    }
  }, [applySnapshot]);

  useEffect(() => {
    const handleScroll = () => {
      scrolling.current = true;
      if (scrollTimer.current !== null) window.clearTimeout(scrollTimer.current);
      scrollTimer.current = window.setTimeout(() => {
        scrolling.current = false;
        scrollTimer.current = null;
        if (pendingSnapshot.current) {
          setSnapshot(pendingSnapshot.current);
          pendingSnapshot.current = null;
        }
      }, 250);
    };
    window.addEventListener("scroll", handleScroll, { passive: true });
    return () => {
      window.removeEventListener("scroll", handleScroll);
      if (scrollTimer.current !== null) window.clearTimeout(scrollTimer.current);
    };
  }, []);

  useEffect(() => {
    void load();
    if (view === "diagnostics") return;
    const interval = view === "overview" ? 3000 : 10000;
    const timer = window.setInterval(() => void load(true), interval);
    return () => window.clearInterval(timer);
  }, [load, view]);

  const abnormalCount = useMemo(
    () => snapshot?.components.filter((item) => item.health === "failed" || item.health === "degraded").length || 0,
    [snapshot]
  );

  const headline = snapshot ? stateLabels[snapshot.state] : stateLabels.boot_locked;
  const actionRunning = snapshot?.action?.status === "running";

  async function finishLifecycleAction(initial: LifecycleAction) {
    let current = initial;
    setSnapshot((value) => value ? { ...value, action: current } : value);
    for (let attempt = 0; attempt < 200 && current.status === "running"; attempt += 1) {
      await new Promise((resolve) => window.setTimeout(resolve, 500));
      current = await api.action(current.id);
      setSnapshot((value) => value ? { ...value, action: current } : value);
    }
    setSnapshot(await api.snapshot());
    if (current.status === "running") throw new Error("网络操作等待超时，可刷新页面继续查看状态");
    if (current.status === "failed") throw new Error(current.message);
  }

  async function runTests() {
    setBusy("tests");
    try {
      setSnapshot(await api.runTests());
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "测试执行失败");
    } finally {
      setBusy("");
    }
  }

  async function restart() {
    if (!window.confirm("重启东京线路？当前 Kill Switch 状态不会改变。")) return;
    setBusy("restart");
    try {
      await finishLifecycleAction(await api.restart());
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "恢复动作失败");
    } finally {
      setBusy("");
    }
  }

  async function start() {
    setBusy("start");
    try {
      await finishLifecycleAction(await api.start());
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "启动保护失败");
    } finally {
      setBusy("");
    }
  }

  async function pause() {
    if (!window.confirm("安全暂停？系统会先请求 Claude 和 ChatGPT 正常退出，再停止东京隧道；Kill Switch 会继续阻断整机公网。")) return;
    setBusy("pause");
    try {
      await finishLifecycleAction(await api.pause());
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "安全暂停失败");
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="controller-shell">
      <header className="topbar">
        <div className="brand"><Shield size={21} /><span>LightNode Controller</span></div>
        <div className="toolbar">
          <button className="icon-button" onClick={() => void load()} title="刷新状态" aria-label="刷新状态"><RefreshCw size={18} className={busy === "refresh" ? "spin" : ""} /></button>
          <button className="tool-button" onClick={() => void runTests()} disabled={Boolean(busy)}><Play size={16} />运行检查</button>
          {snapshot?.state === "maintenance" || snapshot?.state === "inconsistent_locked"
            ? <button className="tool-button" onClick={() => void start()} disabled={Boolean(busy) || actionRunning}><Power size={16} />启动保护</button>
            : <button className="tool-button" onClick={() => void pause()} disabled={Boolean(busy) || actionRunning || !snapshot?.protected}><PowerOff size={16} />安全暂停</button>}
          <button className="tool-button" onClick={() => void restart()} disabled={Boolean(busy) || actionRunning || snapshot?.state === "maintenance"}><RotateCcw size={16} />重启线路</button>
        </div>
      </header>

      <nav className="app-tabs" aria-label="后台页面">
        <div className="page-width tab-row">
          <Tab active={view === "overview"} onClick={() => setView("overview")} icon={<House size={17} />} label="总览" />
          <Tab active={view === "diagnostics"} onClick={() => setView("diagnostics")} icon={<Stethoscope size={17} />} label="系统检查" badge={abnormalCount} />
          <Tab active={view === "node"} onClick={() => setView("node")} icon={<Server size={17} />} label="节点配置" />
          <Tab active={view === "events"} onClick={() => setView("events")} icon={<FileClock size={17} />} label="事件记录" />
        </div>
      </nav>

      <main>
        {error && <div className="page-width error-banner"><XCircle size={18} />{error}</div>}
        {snapshot?.action?.status === "running" && <div className="page-width action-banner"><RefreshCw size={17} className="spin" /><span><strong>{snapshot.action.message}</strong><small>操作 ID {snapshot.action.id}，关闭页面不会中止</small></span></div>}
        {view === "overview" && <Overview snapshot={snapshot} headline={headline} abnormalCount={abnormalCount} onNavigate={setView} />}
        {view === "diagnostics" && <Diagnostics snapshot={snapshot} onRefresh={() => void load()} refreshing={busy === "refresh"} />}
        {view === "node" && <NodeSettings />}
        {view === "events" && <Events snapshot={snapshot} />}
      </main>
    </div>
  );
}

function Tab({ active, onClick, icon, label, badge }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string; badge?: number }) {
  return <button className={active ? "tab-button active" : "tab-button"} onClick={onClick}>{icon}<span>{label}</span>{badge ? <b>{badge}</b> : null}</button>;
}

function Overview({ snapshot, headline, abnormalCount, onNavigate }: {
  snapshot: Snapshot | null;
  headline: { label: string; detail: string };
  abnormalCount: number;
  onNavigate: (view: View) => void;
}) {
  const connections = snapshot?.connections;
  const tone = snapshot?.state === "healthy" ? "healthy" : snapshot?.state === "degraded_locked" ? "degraded" : "warning";
  const pf = component(snapshot, "firewall");
  const dns = component(snapshot, "dns");
  const tunnel = component(snapshot, "tunnel");
  return <>
    <section className={`status-band tone-${tone}`}>
      <div className="page-width status-grid">
        <div className="state-block">
          <span className="eyebrow">当前状态</span>
          <div className="state-title"><StateIcon state={snapshot?.state} /><h1 className="status-headline">{headline.label}</h1></div>
          <p>{headline.detail}</p>
          <div className="mode-line"><span>整机东京 · 严格执行</span><small>更新于 {formatTime(snapshot?.lastCheckAt || "")}</small></div>
        </div>
        <div className="health-grid">
          <HealthItem icon={<Globe2 size={18} />} label="东京出口" value={snapshot?.observedEgress || "不可用"} health={snapshot?.observedEgress && snapshot.observedEgress === snapshot.expectedEgress ? "healthy" : "failed"} />
          <HealthItem icon={<Route size={18} />} label="TUN 隧道" value={snapshot?.tunnelInterface || "未建立"} health={tunnel?.health || "unknown"} />
          <HealthItem icon={<Database size={18} />} label="DNS" value={dns?.health === "healthy" ? "经隧道" : "异常"} health={dns?.health || "unknown"} />
          <HealthItem icon={<Shield size={18} />} label="断网保护" value={snapshot?.protected ? "已启用" : "未启用"} health={pf?.health || "unknown"} />
        </div>
        <div className="runtime-panel">
          <div className="runtime-panel-head"><Activity size={17} /><strong>运行概况</strong></div>
          <div className="runtime-mini-grid">
            <RuntimeFact label="连接观测" value={connections?.health === "healthy" ? "在线" : "未连接"} health={connections?.health || "unknown"} />
            <RuntimeFact label="活动连接" value={`${connections?.activeConnections.length || 0} 条`} />
            <RuntimeFact label="出口违例" value={`${connections?.violationCount || 0} 条`} health={connections?.violationCount ? "failed" : "healthy"} />
            <RuntimeFact label="异常组件" value={`${abnormalCount} 项`} health={abnormalCount ? "failed" : "healthy"} />
          </div>
        </div>
      </div>
    </section>

    <div className="page-width view-content overview-stack">
      <section>
        <SectionTitle icon={<AlertTriangle size={19} />} title="需要关注" meta={`${snapshot?.alerts.length || 0} 项`} />
        {snapshot?.alerts.length ? <div className="attention-list">{snapshot.alerts.slice(0, 4).map((alert) =>
          <button className={`attention-row severity-${alert.severity}`} key={alert.id} onClick={() => onNavigate(alert.component === "connection_observer" ? "overview" : "diagnostics")}>
            <AlertTriangle size={17} /><span><strong>{friendlyTitle(alert.title)}</strong><small>{friendlyMessage(alert.message)}</small></span><ChevronRight size={17} />
          </button>
        )}</div> : <div className="all-clear"><CheckCircle2 size={20} /><span>当前没有需要处理的问题</span></div>}
      </section>
      {connections ? <ConnectionSection observation={connections} /> : <Unavailable title="连接数据尚未就绪" />}
    </div>
  </>;
}

function ConnectionSection({ observation }: { observation: ConnectionObservation }) {
  return <section className="connection-workbench">
    <SectionTitle icon={<BrainCircuit size={19} />} title="实时连接" meta={`最后采集 ${formatTime(observation.lastSuccessfulPollAt || "")}`} />
    <div className="connection-table-wrap"><table className="connection-table active-connection-table">
      <thead><tr><th>进程</th><th>目标</th><th>实际出站</th><th>结果</th><th>流量</th></tr></thead>
      <tbody>{observation.activeConnections.slice(0, 100).map((connection) => <tr key={connection.id}>
        <td><strong>{processName(connection.processPath)}</strong><code>{connection.processPath || "unknown"}</code></td>
        <td><strong>{connection.host || connection.destinationIp || "unknown"}</strong><code>{connection.network}/{connection.destinationPort}</code></td>
        <td><code>{connection.observedOutbound || "-"}</code></td>
        <td><HealthBadge health={connection.compliant ? "healthy" : "failed"} /></td><td>{formatBytes(connection.upload + connection.download)}</td>
      </tr>)}{!observation.activeConnections.length && <tr><td colSpan={5} className="empty">当前没有活动连接</td></tr>}</tbody>
    </table></div>
  </section>;
}

function Diagnostics({ snapshot, onRefresh, refreshing }: { snapshot: Snapshot | null; onRefresh: () => void; refreshing: boolean }) {
  const runtime = snapshot?.runtimeConfig;
  const stages = snapshot?.stages || defaultStages();
  const components = snapshot?.components || [];
  const tests = snapshot?.tests || [];
  const [showRuntime, setShowRuntime] = useState(false);
  return <div className="page-width view-content diagnostics-stack">
    <ViewTitle
      icon={<Stethoscope size={22} />}
      title="系统检查"
      detail={snapshot?.lastCheckAt ? `检查于 ${formatTime(snapshot.lastCheckAt)}` : "等待检查"}
      action={<button className="secondary-command" type="button" onClick={onRefresh} disabled={refreshing}><RefreshCw size={16} className={refreshing ? "spin" : ""} />刷新状态</button>}
    />
    <section>
      <SectionTitle icon={<Route size={19} />} title="链路与组件" meta={`${components.length} 项组件 · ${tests.length} 项主动验证 · 手动刷新`} />
      <div className="diagnostic-stage-list">{stages.map((stage) => <DiagnosticStageRow
        key={stage.id}
        stage={stage}
        components={components.filter((item) => stageComponentIDs[stage.id]?.includes(item.id))}
        tests={tests}
      />)}</div>
    </section>
    <section>
      <ActionSectionTitle
        icon={<Settings2 size={19} />}
        title="组件启动与配置"
        action={showRuntime ? "收起配置" : `展开配置 (${runtime?.components.length || 0})`}
        expanded={showRuntime}
        onClick={() => setShowRuntime((current) => !current)}
      />
      {showRuntime && <div className="runtime-component-list">{(runtime?.components || []).map((configuration) => <RuntimeConfigRow configuration={configuration} key={configuration.id} />)}{!runtime?.components.length && <div className="empty">等待读取配置</div>}</div>}
    </section>
  </div>;
}

function Events({ snapshot }: { snapshot: Snapshot | null }) {
  return <div className="page-width view-content"><ViewTitle icon={<FileClock size={22} />} title="事件记录" detail={`${snapshot?.events.length || 0} 条状态变化`} /><div className="timeline">{(snapshot?.events || []).map((event) => <div className={`timeline-row event-${event.severity}`} key={event.id}><span className="timeline-dot" /><time>{formatDateTime(event.time)}</time><code>{event.component}</code><p>{event.message}</p></div>)}{!snapshot?.events.length && <div className="empty">等待状态变化</div>}</div></div>;
}

const emptyNodeForm: NodeUpdate = {
  server: "", port: 443, expectedEgress: "", flow: "xtls-rprx-vision",
  tlsServerName: "", utlsFingerprint: "chrome", uuid: "", realityPublicKey: "", realityShortId: ""
};

function NodeSettings() {
  const [configuration, setConfiguration] = useState<NodeConfiguration | null>(null);
  const [backups, setBackups] = useState<BackupMetadata[]>([]);
  const [form, setForm] = useState<NodeUpdate>(emptyNodeForm);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);

  const loadNode = useCallback(async () => {
    try {
      const [current, availableBackups] = await Promise.all([api.node(), api.backups()]);
      setConfiguration(current);
      setBackups(availableBackups);
      setForm({
        server: current.server, port: current.port, expectedEgress: current.expectedEgress,
        flow: current.flow, tlsServerName: current.tlsServerName, utlsFingerprint: current.utlsFingerprint,
        uuid: "", realityPublicKey: "", realityShortId: ""
      });
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "节点配置读取失败");
    }
  }, []);

  useEffect(() => { void loadNode(); }, [loadNode]);

  function update<K extends keyof NodeUpdate>(key: K, value: NodeUpdate[K]) {
    setForm((current) => ({ ...current, [key]: value }));
  }

  async function importConfiguration(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    setError("");
    setMessage("");
    try {
      if (file.size > 32 * 1024) throw new Error("VPS 配置文件超过 32 KB");
      const document = JSON.parse(await file.text()) as { schema?: string; version?: number; client?: { name?: string }; endpoint?: { address?: string; port?: number } };
      if (document.schema !== "lightnode.vps-config" || document.version !== 1) throw new Error("VPS 配置格式或版本不受支持");
      const target = `${document.endpoint?.address || "未知地址"}:${document.endpoint?.port || "-"}`;
      if (!window.confirm(`导入${document.client?.name ? `客户端“${document.client.name}”的` : ""}配置并切换到 ${target}？系统会先创建安全备份。`)) return;
      setBusy(true);
      const current = await api.importNode(document);
      setConfiguration(current);
      setForm({
        server: current.server, port: current.port, expectedEgress: current.expectedEgress,
        flow: current.flow, tlsServerName: current.tlsServerName, utlsFingerprint: current.utlsFingerprint,
        uuid: "", realityPublicKey: "", realityShortId: ""
      });
      setBackups(await api.backups());
      setMessage(`配置已导入并切换：${current.server}:${current.port}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "VPS 配置导入失败");
    } finally {
      setBusy(false);
    }
  }

  async function createBackup() {
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const backup = await api.createBackup();
      setBackups(await api.backups());
      setMessage(`已创建配置备份：${formatDateTime(backup.createdAt)}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "配置备份失败");
    } finally {
      setBusy(false);
    }
  }

  async function restoreBackup(backup: BackupMetadata) {
    if (!window.confirm(`恢复 ${formatDateTime(backup.createdAt)} 的配置？恢复前会再次创建安全备份。`)) return;
    setBusy(true);
    setError("");
    setMessage("");
    try {
      const current = await api.restoreBackup(backup.id);
      setConfiguration(current);
      setForm({
        server: current.server, port: current.port, expectedEgress: current.expectedEgress,
        flow: current.flow, tlsServerName: current.tlsServerName, utlsFingerprint: current.utlsFingerprint,
        uuid: "", realityPublicKey: "", realityShortId: ""
      });
      setBackups(await api.backups());
      setMessage(`配置已恢复：${current.server}:${current.port}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "配置恢复失败");
    } finally {
      setBusy(false);
    }
  }

  async function save(event: React.FormEvent) {
    event.preventDefault();
    if (!window.confirm(`保存并切换到 ${form.server}:${form.port}？切换失败时会恢复原节点并保持断网保护。`)) return;
    setBusy(true);
    setMessage("");
    try {
      const current = await api.updateNode(form);
      setConfiguration(current);
      setForm((value) => ({ ...value, uuid: "", realityPublicKey: "", realityShortId: "" }));
      setMessage(`节点已保存并切换：${current.server}:${current.port}`);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "节点保存失败");
    } finally {
      setBusy(false);
    }
  }

  return <div className="page-width view-content node-settings">
    <ViewTitle icon={<Server size={22} />} title="节点配置" detail={configuration ? `当前 ${configuration.server}:${configuration.port}` : "读取当前节点"} />
    {error && <div className="error-banner"><XCircle size={18} />{error}</div>}
    {message && <div className="success-banner"><CheckCircle2 size={18} />{message}</div>}
    <section className="node-import-section">
      <SectionTitle icon={<Upload size={18} />} title="VPS 配置与凭据轮换" meta="lightnode.vps-config · v1" />
      <div className="node-import-actions">
        <input ref={fileInput} className="sr-only" type="file" accept="application/json,.json" onChange={(event) => void importConfiguration(event)} />
        <button type="button" className="primary-command" disabled={busy} onClick={() => fileInput.current?.click()}><Upload size={17} />导入 VPS 配置</button>
        <button type="button" className="secondary-command" disabled={busy} onClick={() => void createBackup()}><Archive size={17} />立即备份</button>
      </div>
    </section>
    <form onSubmit={(event) => void save(event)}>
      <section className="node-form-section">
        <SectionTitle icon={<Globe2 size={18} />} title="服务器与出口" meta="当前东京节点为默认值" />
        <div className="node-form-grid">
          <Field label="VPS IPv4"><input value={form.server} onChange={(event) => update("server", event.target.value)} required /></Field>
          <Field label="VLESS 端口"><input type="number" min="1" max="65535" value={form.port} onChange={(event) => update("port", Number(event.target.value))} required /></Field>
          <Field label="期望出口 IPv4"><input value={form.expectedEgress} onChange={(event) => update("expectedEgress", event.target.value)} required /></Field>
          <Field label="Flow"><select value={form.flow} onChange={(event) => update("flow", event.target.value)}><option value="xtls-rprx-vision">xtls-rprx-vision</option></select></Field>
          <Field label="TLS Server Name"><input value={form.tlsServerName} onChange={(event) => update("tlsServerName", event.target.value)} required /></Field>
          <Field label="uTLS 指纹"><select value={form.utlsFingerprint} onChange={(event) => update("utlsFingerprint", event.target.value)}><option value="chrome">chrome</option><option value="safari">safari</option><option value="firefox">firefox</option></select></Field>
        </div>
      </section>
      <section className="node-form-section">
        <SectionTitle icon={<Shield size={18} />} title="访问凭据" meta="留空保留当前值" />
        <div className="node-form-grid node-secret-grid">
          <SecretField label="VLESS UUID" configured={configuration?.uuidConfigured}><input type="password" autoComplete="new-password" placeholder="保留当前值" value={form.uuid || ""} onChange={(event) => update("uuid", event.target.value)} /></SecretField>
          <SecretField label="Reality Public Key" configured={configuration?.realityPublicKeyConfigured}><input type="password" autoComplete="new-password" placeholder="保留当前值" value={form.realityPublicKey || ""} onChange={(event) => update("realityPublicKey", event.target.value)} /></SecretField>
          <SecretField label="Reality Short ID" configured={configuration?.realityShortIdConfigured}><input type="password" autoComplete="new-password" placeholder="保留当前值" value={form.realityShortId || ""} onChange={(event) => update("realityShortId", event.target.value)} /></SecretField>
        </div>
      </section>
      <div className="node-form-actions"><button className="primary-command" type="submit" disabled={busy || !configuration}><Save size={17} />{busy ? "正在验证并切换" : "保存并切换"}</button></div>
    </form>
    <section className="node-backup-section">
      <SectionTitle icon={<Archive size={18} />} title="配置备份" meta={`${backups.length} 份 · 仅存于本机`} />
      <div className="backup-list">
        {backups.map((backup) => <div className="backup-row" key={backup.id}>
          <div><strong>{formatDateTime(backup.createdAt)}</strong><small>{backup.reason}</small></div>
          <code>{backup.server}:{backup.port}</code>
          <button type="button" className="icon-command" disabled={busy} onClick={() => void restoreBackup(backup)} title="恢复此备份" aria-label={`恢复 ${formatDateTime(backup.createdAt)} 的备份`}><RotateCcw size={17} /></button>
        </div>)}
        {!backups.length && <div className="empty">尚无配置备份</div>}
      </div>
    </section>
  </div>;
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return <label className="field"><span>{label}</span>{children}</label>;
}

function SecretField({ label, configured, children }: { label: string; configured?: boolean; children: React.ReactNode }) {
  return <label className="field"><span>{label}<small className={configured ? "configured" : "missing"}>{configured ? "已配置" : "未配置"}</small></span>{children}</label>;
}

function ViewTitle({ icon, title, detail, health, action }: { icon: React.ReactNode; title: string; detail: string; health?: Health; action?: React.ReactNode }) {
  return <div className="view-title"><div>{icon}<span><h1>{title}</h1><p>{detail}</p></span></div><div className="view-title-actions">{health && <HealthBadge health={health} />}{action}</div></div>;
}

function SectionTitle({ icon, title, meta }: { icon: React.ReactNode; title: string; meta: string }) {
  return <div className="section-heading"><div>{icon}<h2>{title}</h2></div><span>{meta}</span></div>;
}

function ActionSectionTitle({ icon, title, action, expanded, onClick }: {
  icon: React.ReactNode; title: string; action: string; expanded: boolean; onClick: () => void;
}) {
  return <div className="section-heading"><div>{icon}<h2>{title}</h2></div><button className="section-toggle" type="button" aria-expanded={expanded} onClick={onClick}>{action}{expanded ? <ChevronDown size={15} /> : <ChevronRight size={15} />}</button></div>;
}

function HealthItem({ icon, label, value, health }: { icon: React.ReactNode; label: string; value: string; health: Health }) {
  return <div className="health-item"><span className={`health-dot health-${health}`}>{icon}</span><div><small>{label}</small><strong>{value}</strong></div></div>;
}

function RuntimeFact({ label, value, health = "unknown" }: { label: string; value: string; health?: Health }) {
  return <div className="runtime-fact"><span>{label}</span><strong className={`health-${health}`}>{value}</strong></div>;
}

function RuntimeConfigRow({ configuration }: { configuration: RuntimeComponentConfiguration }) {
  const command = configuration.executable ? formatCommand(configuration.executable, configuration.arguments || []) : `由 ${configuration.owner} 内部创建，无独立启动命令`;
  return <div className="runtime-component-row" data-component-config={configuration.id}>
    <div className="runtime-component-head"><span className="runtime-component-icon">{runtimeConfigIcons[configuration.id] || <Settings2 size={18} />}</span><div><strong>{configuration.name}</strong><small>{configuration.owner}{configuration.launchdLabel ? ` · ${configuration.launchdLabel}` : ""}</small></div></div>
    <div className="runtime-command"><span>启动</span><code>{command}</code></div>
    <div className="runtime-settings">{configuration.settings.map((setting) => <div key={`${configuration.id}-${setting.name}`}><span>{setting.name}</span><code>{setting.value}</code></div>)}</div>
  </div>;
}

function DiagnosticStageRow({ stage, components, tests }: { stage: Stage; components: ComponentStatus[]; tests: TestResult[] }) {
  const [opened, setOpened] = useState(false);
  const expanded = opened || stage.health === "failed" || stage.health === "degraded";
  const activeTestCount = components.filter((item) => componentTestIDs[item.id]).length;
  return <section className="diagnostic-stage" data-stage={stage.id}>
    <button className={`diagnostic-stage-head${expanded ? " expanded" : ""}`} type="button" aria-expanded={expanded} onClick={() => setOpened((current) => !current)}>
      <span className={`diagnostic-stage-icon health-${stage.health}`}>{stageIcons[stage.id] || <CircleHelp size={18} />}</span>
      <div><strong>{stage.name}</strong><small>{components.length} 项组件{activeTestCount ? ` · ${activeTestCount} 项主动验证` : ""}</small></div>
      <HealthBadge health={stage.health} />
      {expanded ? <ChevronDown size={16} /> : <ChevronRight size={16} />}
    </button>
    {expanded && <div className="diagnostic-component-list">{components.map((item) => {
      const testID = componentTestIDs[item.id];
      const test = testID ? tests.find((candidate) => candidate.id === testID) : undefined;
      return <div className="diagnostic-component-row" data-component={item.id} data-test={test?.id} key={item.id}>
        <HealthIcon health={item.health} size={15} />
        <div className="diagnostic-component-copy"><strong>{item.name}</strong><span>{item.summary}</span></div>
        <div className="diagnostic-evidence">
          <small>{test ? "主动验证" : "当前值"}</small>
          <code>{test ? `${test.expected} → ${test.actual || "-"}` : item.detail || "-"}</code>
        </div>
        <small>{test?.durationMs ?? item.latencyMs} ms · {formatTime(test?.runAt || item.lastCheckedAt)}</small>
      </div>;
    })}{!components.length && <div className="empty">等待组件检查</div>}</div>}
  </section>;
}

function HealthBadge({ health }: { health: Health }) {
  const label = { healthy: "正常", degraded: "注意", failed: "异常", unknown: "未知" }[health];
  return <span className={`health-badge health-${health}`}><HealthIcon health={health} size={14} />{label}</span>;
}

function HealthIcon({ health, size }: { health: Health; size: number }) {
  if (health === "healthy") return <CheckCircle2 size={size} />;
  if (health === "failed") return <XCircle size={size} />;
  if (health === "degraded") return <AlertTriangle size={size} />;
  return <CircleHelp size={size} />;
}

function StateIcon({ state }: { state?: ControllerState }) {
  if (state === "healthy") return <CheckCircle2 size={30} />;
  if (state === "degraded_locked") return <ShieldCheck size={30} />;
  if (state === "inconsistent_locked") return <AlertTriangle size={30} />;
  if (state === "unprotected") return <AlertTriangle size={30} />;
  return <CircleHelp size={30} />;
}

function Unavailable({ title }: { title: string }) {
  return <div className="unavailable"><CircleHelp size={24} /><strong>{title}</strong></div>;
}

function component(snapshot: Snapshot | null, id: string) {
  return snapshot?.components.find((item) => item.id === id);
}

function defaultStages(): Stage[] {
  return ["network", "firewall", "tunnel", "provider", "dns", "vps"].map((id) => ({ id, name: id, health: "unknown", summary: "等待检查" }));
}

function formatCommand(executable: string, args: string[]) {
  return [executable, ...args].map((value) => value.includes(" ") ? `'${value}'` : value).join(" ");
}

function friendlyTitle(value: string) {
  if (value.includes("出口未受保护")) return "断网保护尚未启用";
  if (value.includes("PF")) return "断网保护尚未完成";
  return value.replace("异常", "需要检查");
}

function friendlyMessage(value: string) {
  if (value.includes("PF Kill Switch 未确认")) return "东京线路断开时，系统还可能回到本地网络。";
  if (value.includes("PF Anchor") || value.includes("VPS Endpoint 未放行")) return "严格保护规则尚未安装，当前不符合专用机运行要求。";
  return value;
}

function formatTime(value: string) {
  if (!value) return "-";
  return new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(new Date(value));
}

function formatDateTime(value: string) {
  if (!value) return "-";
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false }).format(new Date(value));
}

function processName(path: string) {
  if (!path) return "unknown";
  return path.split("/").filter(Boolean).pop() || path;
}

function formatBytes(value: number) {
  if (value < 1024) return `${value} B`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
  return `${(value / (1024 * 1024)).toFixed(1)} MB`;
}
