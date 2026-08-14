import type { FileItem } from "./types";

export function previewURL(file: FileItem | null | undefined, path: string | null, revision: number, props: Record<string, string> = {}): string | null {
  if (file?.component) {
    const query = new URLSearchParams({ name: file.component, at: String(revision) });
    Object.entries(props).forEach(([key, value]) => query.set(`prop.${key}`, value));
    return `api/component-preview?${query}`;
  }
  if (!path) return null;
  return `api/preview?path=${encodeURIComponent(path)}&at=${revision}`;
}

export function usesPdfCanvas(file: FileItem | null | undefined, path: string | null): boolean {
  if (!file || file.component || !path) return false;
  const lower = path.toLowerCase();
  return lower.endsWith(".page.md") || lower.endsWith(".deck.md") || lower.endsWith(".doc.md") || lower.endsWith(".pdf");
}
