import type { ProjectSnapshot, SyncDetails, SyncSnapshot } from "./types";

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, options);
  const type = response.headers.get("content-type") || "";
  const body = type.includes("application/json") ? await response.json() : await response.text();
  if (!response.ok) {
    const message = typeof body === "object" && body && "error" in body ? String(body.error) : String(body || response.statusText);
    throw new Error(message);
  }
  return body as T;
}

export const api = {
  project: () => request<ProjectSnapshot>("api/project"),
  sync: () => request<SyncSnapshot>("api/sync"),
  syncDetails: () => request<SyncDetails>("api/sync-details"),
  readFile: (path: string) => request<string>(`api/file?path=${encodeURIComponent(path)}`),
  writeFile: (path: string, body: string) => request<{ ok: boolean; warning?: string }>(`api/file?path=${encodeURIComponent(path)}`, {
    method: "PUT",
    headers: { "Content-Type": "text/plain" },
    body,
  }),
  push: (space: string, message: string, forceWithLease = "") => request<{ message: string }>("api/push", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ space, message, forceWithLease }),
  }),
  pull: () => request<{ message: string }>("api/pull", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ mode: "replace" }),
  }),
  createComponent: (name: string) => request<{ path: string; message: string }>("api/components", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name }),
  }),
};
