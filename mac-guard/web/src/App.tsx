import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Activity, AlertTriangle, BrainCircuit, CheckCircle2, ChevronRight,
  CircleHelp, Database, FileClock, Globe2, House, Network, Play,
  Power, PowerOff, RefreshCw, RotateCcw, Route, Server, Shield, ShieldCheck, Stethoscope,
  Wifi, XCircle
} from "lucide-react";
import { api } from "./api";
import type {
  ComponentStatus, GuardState, Health, PolicyRoute,
  PolicyV2Snapshot, Snapshot, Stage
} from "./types";

type View = "overview" | "connections" | "diagnostics" | "events";

const stateLabels: Record<GuardState, { label: string; detail: string }> = {
  boot_locked: { label: "正在启动", detail: "等待首轮网络检查" },
  tunnel_connecting: { label: "正在连接", detail: "正在建立东京线路" },
  healthy: { label: "网络保护正常", detail: "出口与防泄漏策略均符合预期" },
  degraded_locked: { label: "已安全断网", detail: "线路异常，Kill Switch 正在阻止本地回落" },
  unprotected: { label: "尚未启用断网保护", detail: "当前线路可用，但断线时仍可能回落到本地网络" },
  recovering: { label: "正在恢复", detail: "正在重启网络组件" },
  maintenance: { label: "已安全暂停", detail: "东京隧道已停止，Kill Switch 正在阻止本地公网" }
};

const routeLabels: Record<PolicyRoute, string> = {
  ai_fixed: "东京固定",
  japan: "全局日本"
};

const stageIcons: Record<string, React.ReactNode> = {
  network: <Wifi size={18} />, firewall: <Shield size={18} />,
  tunnel: <Route size={18} />, provider: <Network size={18} />,
  vps: <Server size={18} />, egress: <Globe2 size={18} />,
  dns: <Database size={18} />
};

export default function App() {
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null);
  const [view, setView] = useState<View>("overview");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<"refresh" | "tests" | "restart" | "start" | "pause" | "">("");

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setBusy("refresh");
    try {
      setSnapshot(await api.snapshot());
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "状态读取失败");
    } finally {
      if (!quiet) setBusy("");
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(true), 3000);
    return () => window.clearInterval(timer);
  }, [load]);

  const abnormalCount = useMemo(
    () => snapshot?.components.filter((item) => item.health === "failed" || item.health === "degraded").length || 0,
    [snapshot]
  );

  const headline = snapshot ? stateLabels[snapshot.state] : stateLabels.boot_locked;

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
      setSnapshot(await api.restart());
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
      setSnapshot(await api.start());
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
      setSnapshot(await api.pause());
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "安全暂停失败");
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="guard-shell">
      <header className="topbar">
        <div className="brand"><Shield size={21} /><span>LightNode Guard</span><small>V2</small></div>
        <div className="toolbar">
          <button className="icon-button" onClick={() => void load()} title="刷新状态" aria-label="刷新状态"><RefreshCw size={18} className={busy === "refresh" ? "spin" : ""} /></button>
          <button className="tool-button" onClick={() => void runTests()} disabled={Boolean(busy)}><Play size={16} />运行检查</button>
          {snapshot?.state === "maintenance"
            ? <button className="tool-button" onClick={() => void start()} disabled={Boolean(busy)}><Power size={16} />启动保护</button>
            : <button className="tool-button" onClick={() => void pause()} disabled={Boolean(busy) || !snapshot?.protected}><PowerOff size={16} />安全暂停</button>}
          <button className="tool-button" onClick={() => void restart()} disabled={Boolean(busy) || snapshot?.state === "maintenance"}><RotateCcw size={16} />重启线路</button>
        </div>
      </header>

      <nav className="app-tabs" aria-label="后台页面">
        <div className="page-width tab-row">
          <Tab active={view === "overview"} onClick={() => setView("overview")} icon={<House size={17} />} label="总览" />
          <Tab active={view === "connections"} onClick={() => setView("connections")} icon={<BrainCircuit size={17} />} label="连接观察" badge={snapshot?.policyV2?.violationCount} />
          <Tab active={view === "diagnostics"} onClick={() => setView("diagnostics")} icon={<Stethoscope size={17} />} label="系统检查" badge={abnormalCount} />
          <Tab active={view === "events"} onClick={() => setView("events")} icon={<FileClock size={17} />} label="事件记录" />
        </div>
      </nav>

      <main>
        {error && <div className="page-width error-banner"><XCircle size={18} />{error}</div>}
        {view === "overview" && <Overview snapshot={snapshot} headline={headline} abnormalCount={abnormalCount} onNavigate={setView} />}
        {view === "connections" && <div className="page-width view-content">{snapshot?.policyV2 ? <PolicyView policy={snapshot.policyV2} /> : <Unavailable title="V2 连接观察未启用" />}</div>}
        {view === "diagnostics" && <Diagnostics snapshot={snapshot} />}
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
  const policy = snapshot?.policyV2;
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
          <div className="mode-line"><span>{policy?.mode === "enforce" ? "严格执行" : "策略未加载"}</span><small>更新于 {formatTime(snapshot?.lastCheckAt || "")}</small></div>
        </div>
        <div className="health-grid">
          <HealthItem icon={<Globe2 size={18} />} label="东京出口" value={snapshot?.observedEgress || "不可用"} health={snapshot?.observedEgress === snapshot?.expectedEgress ? "healthy" : "failed"} />
          <HealthItem icon={<Route size={18} />} label="TUN 隧道" value={snapshot?.tunnelInterface || "未建立"} health={tunnel?.health || "unknown"} />
          <HealthItem icon={<Database size={18} />} label="DNS" value={dns?.health === "healthy" ? "经隧道" : "异常"} health={dns?.health || "unknown"} />
          <HealthItem icon={<Shield size={18} />} label="断网保护" value={snapshot?.protected ? "已启用" : "未启用"} health={pf?.health || "unknown"} />
        </div>
      </div>
    </section>

    <div className="page-width view-content overview-stack">
      <section>
        <SectionTitle icon={<Route size={19} />} title="流量去向" meta="全机统一策略" />
        <div className="route-board route-board-three">
          <TrafficRoute icon={<BrainCircuit size={20} />} name="Claude / GPT" now="东京 VPS" target="东京 VPS" health="healthy" />
          <TrafficRoute icon={<Wifi size={20} />} name="系统与其他软件" now="东京 VPS" target="东京 VPS" health="healthy" />
          <TrafficRoute icon={<Globe2 size={20} />} name="Google / YouTube" now="东京 VPS" target="东京 VPS" health="healthy" />
        </div>
      </section>

      <section>
        <SectionTitle icon={<AlertTriangle size={19} />} title="需要关注" meta={`${snapshot?.alerts.length || 0} 项`} />
        {snapshot?.alerts.length ? <div className="attention-list">{snapshot.alerts.slice(0, 4).map((alert) =>
          <button className={`attention-row severity-${alert.severity}`} key={alert.id} onClick={() => onNavigate(alert.component === "policy_v2" ? "connections" : "diagnostics")}>
            <AlertTriangle size={17} /><span><strong>{friendlyTitle(alert.title)}</strong><small>{friendlyMessage(alert.message)}</small></span><ChevronRight size={17} />
          </button>
        )}</div> : <div className="all-clear"><CheckCircle2 size={20} /><span>当前没有需要处理的问题</span></div>}
      </section>

      <section>
        <SectionTitle icon={<Activity size={19} />} title="运行概况" meta={`${abnormalCount} 个异常组件`} />
        <div className="overview-facts">
          <Fact label="V2 观测器" value={policy?.observerHealth === "healthy" ? "在线" : "未连接"} health={policy?.observerHealth || "unknown"} />
          <Fact label="活动连接" value={`${policy?.activeConnections.length || 0} 条`} />
          <Fact label="当前模式" value="整机日本" health="healthy" />
          <Fact label="策略违例" value={`${policy?.violationCount || 0} 条`} health={policy?.violationCount ? "failed" : "healthy"} />
        </div>
      </section>
    </div>
  </>;
}

function PolicyView({ policy }: { policy: PolicyV2Snapshot }) {
  return <section className="policy-workbench">
    <ViewTitle icon={<BrainCircuit size={22} />} title="连接观察" detail={`整机日本 · ${policy.observerMessage}`} health={policy.observerHealth} />
    <div className="policy-summary">
      <Fact label="活动连接" value={String(policy.activeConnections.length)} />
      <Fact label="路由模式" value="整机日本" health="healthy" />
      <Fact label="策略违例" value={String(policy.violationCount)} health={policy.violationCount ? "failed" : "healthy"} />
      <Fact label="最后采集" value={formatTime(policy.lastSuccessfulPollAt || "")} />
    </div>
    <div className="connection-table-heading"><span>实时连接</span><b>{policy.activeConnections.length}</b></div>
    <div className="policy-table-wrap"><table className="policy-table active-policy-table">
      <thead><tr><th>进程</th><th>目标</th><th>分类</th><th>实际链路</th><th>结果</th><th>流量</th></tr></thead>
      <tbody>{policy.activeConnections.slice(0, 100).map((connection) => <tr key={connection.id}>
        <td><strong>{processName(connection.processPath)}</strong><code>{connection.processPath || "unknown"}</code></td>
        <td><strong>{connection.host || connection.destinationIp || "unknown"}</strong><code>{connection.network}/{connection.destinationPort}</code></td>
        <td><RouteBadge route={connection.disposition} /></td><td><code>{connection.observedOutbound || "-"}</code></td>
        <td><HealthBadge health={connection.compliant ? "healthy" : "failed"} /></td><td>{formatBytes(connection.upload + connection.download)}</td>
      </tr>)}{!policy.activeConnections.length && <tr><td colSpan={6} className="empty">当前没有活动连接</td></tr>}</tbody>
    </table></div>
  </section>;
}

function Diagnostics({ snapshot }: { snapshot: Snapshot | null }) {
  return <div className="page-width view-content diagnostics-stack">
    <ViewTitle icon={<Stethoscope size={22} />} title="系统检查" detail={snapshot?.lastCheckAt ? `检查于 ${formatTime(snapshot.lastCheckAt)}` : "等待检查"} />
    <section><SectionTitle icon={<Route size={19} />} title="连接链路" meta={`每 ${Math.max(1, secondsUntil(snapshot?.nextCheckAt || ""))} 秒刷新`} /><div className="stage-flow">{(snapshot?.stages || defaultStages()).map((stage, index, values) => <StageNode key={stage.id} stage={stage} last={index === values.length - 1} />)}</div></section>
    <section><SectionTitle icon={<Activity size={19} />} title="组件状态" meta={`${snapshot?.components.length || 0} 项`} /><div className="component-table-wrap"><table className="component-table"><thead><tr><th>组件</th><th>状态</th><th>摘要</th><th>详情</th><th>耗时</th><th>检查时间</th></tr></thead><tbody>{(snapshot?.components || []).map((item) => <ComponentRow component={item} key={item.id} />)}{!snapshot?.components.length && <tr><td colSpan={6} className="empty">等待首轮组件检查</td></tr>}</tbody></table></div></section>
    <section><SectionTitle icon={<CheckCircle2 size={19} />} title="网络测试" meta={`${snapshot?.tests.length || 0} 项`} /><div className="test-grid">{(snapshot?.tests || []).map((test) => <article className="test-item" key={test.id}><div className="test-head"><HealthIcon health={test.health} size={18} /><strong>{test.name}</strong><span>{test.durationMs} ms</span></div><dl><div><dt>期望</dt><dd>{test.expected}</dd></div><div><dt>实际</dt><dd>{test.actual}</dd></div></dl><p>{test.detail}</p></article>)}{!snapshot?.tests.length && <div className="empty test-empty">尚无测试结果</div>}</div></section>
  </div>;
}

function Events({ snapshot }: { snapshot: Snapshot | null }) {
  return <div className="page-width view-content"><ViewTitle icon={<FileClock size={22} />} title="事件记录" detail={`${snapshot?.events.length || 0} 条状态变化`} /><div className="timeline">{(snapshot?.events || []).map((event) => <div className={`timeline-row event-${event.severity}`} key={event.id}><span className="timeline-dot" /><time>{formatDateTime(event.time)}</time><code>{event.component}</code><p>{event.message}</p></div>)}{!snapshot?.events.length && <div className="empty">等待状态变化</div>}</div></div>;
}

function ViewTitle({ icon, title, detail, health }: { icon: React.ReactNode; title: string; detail: string; health?: Health }) {
  return <div className="view-title"><div>{icon}<span><h1>{title}</h1><p>{detail}</p></span></div>{health && <HealthBadge health={health} />}</div>;
}

function SectionTitle({ icon, title, meta }: { icon: React.ReactNode; title: string; meta: string }) {
  return <div className="section-heading"><div>{icon}<h2>{title}</h2></div><span>{meta}</span></div>;
}

function HealthItem({ icon, label, value, health }: { icon: React.ReactNode; label: string; value: string; health: Health }) {
  return <div className="health-item"><span className={`health-dot health-${health}`}>{icon}</span><div><small>{label}</small><strong>{value}</strong></div></div>;
}

function TrafficRoute({ icon, name, now, target, health }: { icon: React.ReactNode; name: string; now: string; target: string; health: Health }) {
  return <article className="traffic-route"><span className={`route-icon health-${health}`}>{icon}</span><div className="route-copy"><strong>{name}</strong><span><small>现在</small>{now}</span><span><small>目标</small>{target}</span></div></article>;
}

function Fact({ label, value, health = "unknown" }: { label: string; value: string; health?: Health }) {
  return <div className="fact"><span>{label}</span><strong className={`health-${health}`}>{value}</strong></div>;
}

function StageNode({ stage, last }: { stage: Stage; last: boolean }) {
  return <div className="stage-wrap"><div className={`stage-node health-${stage.health}`} title={stage.summary}><span className="stage-icon">{stageIcons[stage.id] || <CircleHelp size={18} />}</span><strong>{stage.name}</strong><small>{stage.summary || "等待检查"}</small></div>{!last && <span className={`stage-connector health-${stage.health}`} />}</div>;
}

function ComponentRow({ component }: { component: ComponentStatus }) {
  return <tr><td><strong>{component.name}</strong><code>{component.id}</code></td><td><HealthBadge health={component.health} /></td><td>{component.summary}</td><td className="component-detail">{component.detail || "-"}</td><td>{component.latencyMs} ms</td><td>{formatTime(component.lastCheckedAt)}</td></tr>;
}

function RouteBadge({ route }: { route: PolicyRoute }) {
  return <span className={`policy-route route-${route}`}>{routeLabels[route]}</span>;
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

function StateIcon({ state }: { state?: GuardState }) {
  if (state === "healthy") return <CheckCircle2 size={30} />;
  if (state === "degraded_locked") return <ShieldCheck size={30} />;
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
  return ["network", "firewall", "tunnel", "provider", "vps", "egress", "dns"].map((id) => ({ id, name: id, health: "unknown", summary: "等待检查" }));
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

function secondsUntil(value: string) {
  if (!value) return 0;
  return Math.max(0, Math.round((new Date(value).getTime() - Date.now()) / 1000));
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
