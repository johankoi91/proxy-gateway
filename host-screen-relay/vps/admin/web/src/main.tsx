import React, { FormEvent, useCallback, useEffect, useState } from "react";
import ReactDOM from "react-dom/client";
import { Download, Plus, RefreshCw, Trash2, X } from "lucide-react";
import "./style.css";

type ServiceStatus = { wireGuard: boolean; hostRelay: boolean; hostBundle: boolean };
type Client = {
  name: string;
  address: string;
  lastHandshake: number;
  received: number;
  sent: number;
};

async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...options,
    headers: { "Content-Type": "application/json", ...options.headers },
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `请求失败 (${response.status})`);
  }
  return response.status === 204 ? (undefined as T) : response.json();
}

function relativeTime(stamp: number): string {
  if (!stamp) return "从未连接";
  const seconds = Math.floor(Date.now() / 1000 - stamp);
  if (seconds < 90) return "刚刚";
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86400) return `${Math.floor(seconds / 3600)} 小时前`;
  return new Date(stamp * 1000).toLocaleString("zh-CN");
}

function bytes(value: number): string {
  if (!value) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1);
  return `${(value / 1024 ** index).toFixed(index ? 1 : 0)} ${units[index]}`;
}

function StatusItem({ healthy, label, good, bad }: { healthy?: boolean; label: string; good: string; bad: string }) {
  return (
    <div className="status-item">
      <span className={`dot ${healthy === undefined ? "" : healthy ? "ok" : "bad"}`} />
      <strong>{label}</strong>
      <span>{healthy === undefined ? "检查中" : healthy ? good : bad}</span>
    </div>
  );
}

function App() {
  const [service, setService] = useState<ServiceStatus>();
  const [clients, setClients] = useState<Client[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [error, setError] = useState("");

  const load = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [nextService, nextClients] = await Promise.all([
        api<ServiceStatus>("/api/status"),
        api<Client[]>("/api/clients"),
      ]);
      setService(nextService);
      setClients(nextClients);
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  async function createClient(event: FormEvent) {
    event.preventDefault();
    setLoading(true);
    setError("");
    try {
      await api<Client>("/api/clients", { method: "POST", body: JSON.stringify({ name: name.trim() }) });
      setCreating(false);
      setName("");
      await load();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "创建失败");
      setLoading(false);
    }
  }

  async function revoke(client: Client) {
    if (!window.confirm(`确认撤销 ${client.name}？撤销后原配置立即失效。`)) return;
    setLoading(true);
    setError("");
    try {
      await api<void>(`/api/clients/${encodeURIComponent(client.name)}`, { method: "DELETE" });
      await load();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "撤销失败");
      setLoading(false);
    }
  }

  return (
    <main>
      <header>
        <div>
          <h1>广州远程接入</h1>
          <p>为远端 Mac 创建 WireGuard 配置</p>
        </div>
        <button className="icon-button" title="刷新" aria-label="刷新" disabled={loading} onClick={() => void load()}>
          <RefreshCw size={21} className={loading ? "spinning" : ""} />
        </button>
      </header>

      <section className="status-bar" aria-label="服务状态">
        <StatusItem healthy={service?.wireGuard} label="WireGuard" good="正常" bad="异常" />
        <StatusItem healthy={service?.hostRelay} label="宿主转发" good="在线" bad="最近无连接" />
      </section>

      <section className="host-delivery">
        <div>
          <h2>宿主 Mac 安装</h2>
          <p>自带 WireGuard 组件和 PF/SNAT 管理服务，适用于 Apple Silicon Mac</p>
          <p className="delivery-note">用于当前宿主恢复或迁移，不要同时安装到两台宿主 Mac</p>
        </div>
        <div className="delivery-actions">
          <a className={`button-link ${service?.hostBundle ? "" : "disabled"}`} href={service?.hostBundle ? "/api/host/bundle.zip" : undefined}><Download size={18} />下载安装包</a>
          <a className={`text-link ${service?.hostBundle ? "" : "disabled"}`} href={service?.hostBundle ? "/api/host/config" : undefined}>仅下载配置</a>
        </div>
      </section>

      <section className="toolbar">
        <h2>远端 Mac</h2>
        <button onClick={() => setCreating(true)}><Plus size={19} />新增客户端</button>
      </section>

      <div className="message" role="status">{error}</div>
      <table>
        <thead><tr><th>名称</th><th>地址</th><th>最近连接</th><th>流量</th><th className="actions">操作</th></tr></thead>
        <tbody>
          {!loading && clients.length === 0 ? (
            <tr><td colSpan={5} className="empty">还没有远端客户端</td></tr>
          ) : clients.map((client) => (
            <tr key={client.name}>
              <td><strong>{client.name}</strong></td>
              <td><code>{client.address}</code></td>
              <td>{relativeTime(client.lastHandshake)}</td>
              <td>↓ {bytes(client.received)}　↑ {bytes(client.sent)}</td>
              <td className="actions">
                <a href={`/api/clients/${encodeURIComponent(client.name)}/client.conf`}><Download size={18} />下载配置</a>
                <button className="danger" title="撤销客户端" onClick={() => void revoke(client)}><Trash2 size={18} />撤销</button>
              </td>
            </tr>
          ))}
          {loading && clients.length === 0 && <tr><td colSpan={5} className="empty">正在加载...</td></tr>}
        </tbody>
      </table>

      {creating && (
        <div className="modal-backdrop" onMouseDown={() => setCreating(false)}>
          <form className="modal" onSubmit={createClient} onMouseDown={(event) => event.stopPropagation()}>
            <div className="modal-title"><h2>新增客户端</h2><button type="button" className="icon-button" aria-label="关闭" onClick={() => setCreating(false)}><X size={20} /></button></div>
            <label htmlFor="client-name">设备名称</label>
            <input id="client-name" autoFocus value={name} onChange={(event) => setName(event.target.value)} maxLength={48} placeholder="例如 alice-mac" required pattern="[A-Za-z0-9][A-Za-z0-9._-]{0,47}" />
            <div className="modal-actions"><button type="button" className="secondary" onClick={() => setCreating(false)}>取消</button><button type="submit" disabled={loading}>创建</button></div>
          </form>
        </div>
      )}
    </main>
  );
}

ReactDOM.createRoot(document.getElementById("root")!).render(<React.StrictMode><App /></React.StrictMode>);
