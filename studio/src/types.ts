export type FileSection = "content" | "templates";

export interface FileItem {
  path: string;
  editable: boolean;
  previewable: boolean;
  section: FileSection;
  group: string;
  label: string;
  previewPath?: string;
  component?: string;
  templateLabel?: string;
  hidden?: boolean;
}

export interface ProjectSnapshot {
  project: { id: string; name: string };
  workspacePath: string;
  state: { baseVersion?: string };
  status: { dirty: boolean };
  files: FileItem[];
  tailwindClasses?: string[];
}

export interface SyncSnapshot {
  state: "local-only" | "local-ahead" | "remote-ahead" | "diverged" | "up-to-date" | "unavailable";
  provider?: "drive" | "notion";
  localChanged: boolean;
  remoteChanged: boolean;
  driveName?: string;
  driveUrl?: string;
  baseVersion?: string;
  remoteVersion?: string;
  firstPush: boolean;
  message?: string;
}

export interface FileChange {
  path: string;
  kind: "added" | "modified" | "removed";
}

export interface SyncDetails {
  local: FileChange[];
  remote: FileChange[];
  error?: string;
}
