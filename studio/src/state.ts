import { computed, signal } from "@preact/signals";
import type { FileItem, FileSection, ProjectSnapshot, PushProgress, SyncDetails, SyncSnapshot } from "./types";

export const project = signal<ProjectSnapshot | null>(null);
export const sync = signal<SyncSnapshot | null>(null);
export const syncReview = signal<SyncDetails | null>(null);
export const pushProgress = signal<PushProgress | null>(null);
export const section = signal<FileSection>("content");
export const selectedPath = signal<string | null>(null);
export const selectedFile = computed<FileItem | null>(() => project.value?.files.find((file) => file.path === selectedPath.value) || null);
export const source = signal("");
export const previewPath = signal<string | null>(null);
export const previewRevision = signal(0);
export const saveState = signal("");
export const renderState = signal("");
export const draftDirty = signal(false);
export const previewStale = signal(false);
export const externalChange = signal(false);
export const connected = signal(true);
export const theme = signal<"light" | "dark">("dark");
export const sourceShare = signal(42);
export const notice = signal<{ message: string; error: boolean } | null>(null);

export function selectPreview(file: FileItem): string | null {
  return file.previewPath || (file.previewable ? file.path : null);
}
