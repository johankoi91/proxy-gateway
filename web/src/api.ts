import type { Client, GatewayStatus } from "./types";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers
    },
    cache: "no-store"
  });
  if (!response.ok) {
    const body = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(body.error || response.statusText);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

export const api = {
  status: () => request<GatewayStatus>("/api/status"),
  clients: () => request<Client[]>("/api/clients"),
  createClient: (name: string) =>
    request<Client>("/api/clients", { method: "POST", body: JSON.stringify({ name }) }),
  deleteClient: (id: string) => request<void>(`/api/clients/${id}`, { method: "DELETE" }),
  text: async (path: string) => {
    const response = await fetch(path, { cache: "no-store" });
    if (!response.ok) throw new Error("无法读取配置");
    return response.text();
  }
};
