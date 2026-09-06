import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Activity,
  Check,
  Clipboard,
  Download,
  KeyRound,
  Plus,
  RefreshCw,
  Server,
  ShieldCheck,
  Trash2,
  Wifi,
  X
} from "lucide-react";
import { api } from "./api";
import type { Client, GatewayStatus, PeerStatus } from "./types";

type ConfigMode = "wireguard" | "vless";

export default function App() {
  const [status, setStatus] = useState<GatewayStatus | null>(null);
  const [clients, setClients] = useState<Client[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<Client | null>(null);
  const [mode, setMode] = useState<ConfigMode>("wireguard");
  const [showCreate, setShowCreate] = useState(false);

  const load = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true);
    try {
      const [nextStatus, nextClients] = await Promise.all([api.status(), api.clients()]);
      setStatus(nextStatus);
      setClients(nextClients);
      setError("");
    } catch (err) {
      setError(err instanceof Error ? err.message : "无法加载网关状态");
    } finally {
      if (!quiet) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = window.setInterval(() => void load(true), 5000);
    return () => window.clearInterval(timer);
  }, [load]);

  const peerByID = useMemo(() => {
    const map = new Map<string, PeerStatus>();
    status?.peers.forEach((peer) => map.set(peer.id, peer));
    return map;
  }, [status]);

  async function removeClient(client: Client) {
    if (!window.confirm(`删除客户端“${client.name}”？现有配置将立即失效。`)) return;
    try {
      await api.deleteClient(client.id);
      if (selected?.id === client.id) setSelected(null);
      await load(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除失败");
    }
  }

  return (
    <div className="app-shell">
      <header className="topbar">
        <div className="brand">
          <ShieldCheck size={22} />
          <span>VPN Gateway</span>
        </div>
        <button className="icon-button" onClick={() => void load()} title="刷新状态" aria-label="刷新状态">
          <RefreshCw size={18} className={loading ? "spin" : ""} />
        </button>
      </header>

      <main>
        <section className="overview-band">
          <div className="section-inner overview-grid">
            <div className="headline-block">
              <span className="eyebrow">公网出口</span>
              <h1>{status?.publicIp || "等待连接"}</h1>
              <p>{status ? `${status.egressInterface} · WireGuard UDP ${status.wireGuardPort} · Reality TCP ${status.realityPort}` : "正在读取网关状态"}</p>
            </div>
            <div className="health-list">
              <HealthRow label="WireGuard" ok={Boolean(status?.wireGuardUp)} icon={<Wifi size={18} />} />
              <HealthRow label="VLESS Reality" ok={Boolean(status?.xrayUp)} icon={<KeyRound size={18} />} />
              <HealthRow label="隧道 DNS" ok={Boolean(status?.dnsUp)} icon={<Server size={18} />} />
              <HealthRow label="IPv4 转发" ok={Boolean(status?.ipv4Forwarding)} icon={<Activity size={18} />} />
            </div>
          </div>
        </section>

        <section className="clients-section">
          <div className="section-inner">
            <div className="section-heading">
              <div>
                <span className="eyebrow">访问控制</span>
                <h2>客户端</h2>
              </div>
              <button className="primary-button" onClick={() => setShowCreate(true)}>
                <Plus size={17} /> 添加客户端
              </button>
            </div>

            {error && <div className="error-banner">{error}</div>}

            <div className="client-table-wrap">
              <table className="client-table">
                <thead>
                  <tr>
                    <th>名称</th>
                    <th>状态</th>
                    <th>隧道地址</th>
                    <th>远端</th>
                    <th>接收</th>
                    <th>发送</th>
                    <th><span className="sr-only">操作</span></th>
                  </tr>
                </thead>
                <tbody>
                  {clients.map((client) => {
                    const peer = peerByID.get(client.id);
                    return (
                      <tr key={client.id}>
                        <td>
                          <button className="client-name" onClick={() => { setSelected(client); setMode("wireguard"); }}>
                            {client.name}
                          </button>
                          <small>{formatDate(client.createdAt)}</small>
                        </td>
                        <td><StatusPill connected={Boolean(peer?.connected)} /></td>
                        <td><code>{client.ipv4}</code></td>
                        <td className="endpoint">{peer?.endpoint || "-"}</td>
                        <td>{formatBytes(peer?.receiveBytes || 0)}</td>
                        <td>{formatBytes(peer?.transmitBytes || 0)}</td>
                        <td>
                          <div className="row-actions">
                            <button className="icon-button" onClick={() => { setSelected(client); setMode("wireguard"); }} title="查看配置" aria-label={`查看 ${client.name} 配置`}>
                              <KeyRound size={17} />
                            </button>
                            <button className="icon-button danger" onClick={() => void removeClient(client)} title="删除客户端" aria-label={`删除 ${client.name}`}>
                              <Trash2 size={17} />
                            </button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                  {!loading && clients.length === 0 && (
                    <tr><td colSpan={7} className="empty-state">尚未创建客户端</td></tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </section>
      </main>

      {selected && (
        <ConfigDialog client={selected} mode={mode} onMode={setMode} onClose={() => setSelected(null)} />
      )}
      {showCreate && (
        <CreateDialog
          onClose={() => setShowCreate(false)}
          onCreated={async (client) => { setShowCreate(false); await load(true); setSelected(client); setMode("wireguard"); }}
        />
      )}
    </div>
  );
}

function HealthRow({ label, ok, icon }: { label: string; ok: boolean; icon: React.ReactNode }) {
  return (
    <div className="health-row">
      <span className="health-icon">{icon}</span>
      <span>{label}</span>
      <span className={ok ? "health-ok" : "health-down"}>{ok ? "正常" : "异常"}</span>
    </div>
  );
}

function StatusPill({ connected }: { connected: boolean }) {
  return <span className={`status-pill ${connected ? "online" : "offline"}`}><span />{connected ? "在线" : "离线"}</span>;
}

function ConfigDialog({ client, mode, onMode, onClose }: { client: Client; mode: ConfigMode; onMode: (mode: ConfigMode) => void; onClose: () => void }) {
  const [copied, setCopied] = useState(false);
  const base = `/api/clients/${client.id}`;
  const qr = mode === "wireguard" ? `${base}/wireguard.png` : `${base}/vless.png`;
  const config = mode === "wireguard" ? `${base}/wireguard.conf` : `${base}/vless.txt`;
  const clash = `${base}/clash.yaml`;
  const vpsConfig = `${base}/vps-config.json`;

  async function copyConfig() {
    await navigator.clipboard.writeText(await api.text(config));
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  }

  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <section className="modal" role="dialog" aria-modal="true" aria-labelledby="config-title">
        <header className="modal-header">
          <div><span className="eyebrow">客户端配置</span><h2 id="config-title">{client.name}</h2></div>
          <button className="icon-button" onClick={onClose} title="关闭" aria-label="关闭"><X size={19} /></button>
        </header>
        <div className="segmented-control" role="tablist">
          <button className={mode === "wireguard" ? "active" : ""} onClick={() => onMode("wireguard")}>WireGuard</button>
          <button className={mode === "vless" ? "active" : ""} onClick={() => onMode("vless")}>VLESS Reality</button>
        </div>
        <div className="qr-panel">
          <img src={qr} alt={`${client.name} ${mode} 配置二维码`} />
          <div className="config-meta">
            <div><span>IPv4</span><code>{client.ipv4}</code></div>
            <div><span>IPv6</span><code>{client.ipv6}</code></div>
          </div>
        </div>
        <footer className="modal-actions">
          <button className="secondary-button" onClick={() => void copyConfig()}>{copied ? <Check size={17} /> : <Clipboard size={17} />}{copied ? "已复制" : "复制配置"}</button>
          {mode === "vless" && <a className="secondary-button" href={clash} download><Download size={17} />Clash YAML</a>}
          {mode === "vless" && <a className="secondary-button" href={vpsConfig} download><Download size={17} />VPS 配置</a>}
          <a className="primary-button" href={config} download><Download size={17} />下载配置</a>
        </footer>
      </section>
    </div>
  );
}

function CreateDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (client: Client) => void }) {
  const [name, setName] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  async function submit(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError("");
    try {
      onCreated(await api.createClient(name));
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建失败");
      setBusy(false);
    }
  }

  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <form className="modal compact" role="dialog" aria-modal="true" onSubmit={submit}>
        <header className="modal-header"><div><span className="eyebrow">访问控制</span><h2>添加客户端</h2></div><button type="button" className="icon-button" onClick={onClose} title="关闭" aria-label="关闭"><X size={19} /></button></header>
        <label className="field-label" htmlFor="client-name">客户端名称</label>
        <input id="client-name" value={name} onChange={(event) => setName(event.target.value)} maxLength={48} autoFocus placeholder="例如 pixel-phone" />
        {error && <div className="inline-error">{error}</div>}
        <footer className="modal-actions"><button type="button" className="secondary-button" onClick={onClose}>取消</button><button className="primary-button" disabled={busy || !name.trim()}>{busy ? "正在应用配置" : "创建客户端"}</button></footer>
      </form>
    </div>
  );
}

function formatBytes(value: number) {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index < 2 ? 0 : 1)} ${units[index]}`;
}

function formatDate(value: string) {
  return new Intl.DateTimeFormat("zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }).format(new Date(value));
}
