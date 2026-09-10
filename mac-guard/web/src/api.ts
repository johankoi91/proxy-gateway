import type { BackupMetadata, LifecycleAction, NodeConfiguration, NodeUpdate, Snapshot } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, cache: "no-store" });
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { error?: string };
    throw new Error(body.error || `${response.status} ${response.statusText}`);
  }
  return response.json() as Promise<T>;
}

function normalizeSnapshot(snapshot: Snapshot): Snapshot {
  return {
    ...snapshot,
    components: snapshot.components || [],
    stages: snapshot.stages || [],
    tests: snapshot.tests || [],
    alerts: snapshot.alerts || [],
    events: snapshot.events || [],
    connections: snapshot.connections ? {
      ...snapshot.connections,
      activeConnections: snapshot.connections.activeConnections || []
    } : {
      health: "unknown",
      message: "连接数据尚未就绪",
      activeConnections: [],
      violationCount: 0
    },
    runtimeConfig: snapshot.runtimeConfig ? {
      ...snapshot.runtimeConfig,
      components: snapshot.runtimeConfig.components || []
    } : snapshot.runtimeConfig
  };
}

async function snapshotRequest(path: string, init?: RequestInit) {
  return normalizeSnapshot(await request<Snapshot>(path, init));
}

export const api = {
  snapshot: () => snapshotRequest("/api/snapshot"),
  runTests: () => snapshotRequest("/api/tests/run", { method: "POST" }),
  restart: () => request<LifecycleAction>("/api/actions/restart", { method: "POST" }),
  start: () => request<LifecycleAction>("/api/actions/start", { method: "POST" }),
  pause: () => request<LifecycleAction>("/api/actions/pause", { method: "POST" }),
  action: (id: string) => request<LifecycleAction>(`/api/actions/${encodeURIComponent(id)}`),
  node: () => request<NodeConfiguration>("/api/node"),
  updateNode: (configuration: NodeUpdate) => request<NodeConfiguration>("/api/node", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(configuration)
  }),
  importNode: (configuration: unknown) => request<NodeConfiguration>("/api/node/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(configuration)
  }),
  backups: () => request<BackupMetadata[]>("/api/backups"),
  createBackup: () => request<BackupMetadata>("/api/backups", { method: "POST" }),
  restoreBackup: (id: string) => request<NodeConfiguration>(`/api/backups/${encodeURIComponent(id)}/restore`, { method: "POST" })
};
