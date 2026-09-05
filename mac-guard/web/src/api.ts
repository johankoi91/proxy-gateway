import type { Snapshot } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, { ...init, cache: "no-store" });
  if (!response.ok) {
    const body = await response.json().catch(() => ({})) as { error?: string };
    throw new Error(body.error || `${response.status} ${response.statusText}`);
  }
  return response.json() as Promise<T>;
}

export const api = {
  snapshot: () => request<Snapshot>("/api/snapshot"),
  runTests: () => request<Snapshot>("/api/tests/run", { method: "POST" }),
  restart: () => request<Snapshot>("/api/actions/restart", { method: "POST" }),
  start: () => request<Snapshot>("/api/actions/start", { method: "POST" }),
  pause: () => request<Snapshot>("/api/actions/pause", { method: "POST" })
};
