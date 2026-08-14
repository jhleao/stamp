import { useEffect, useRef, useState } from "preact/hooks";
import { api } from "./api";
import {
  connected, draftDirty, externalChange, notice, previewPath, previewRevision, previewStale, project, renderState, saveState, section,
  selectedFile, selectedPath, selectPreview, source, sourceShare, sync, syncReview, theme,
} from "./state";
import type { FileChange, FileItem, FileSection } from "./types";
import { MonacoEditor, type MonacoEditorHandle } from "./MonacoEditor";
import { fileLabelParts, groupedFiles, visibleFiles, type FileTree } from "./navigation";
import { previewURL, usesPdfCanvas } from "./preview";
import { PdfPreview } from "./PdfPreview";
import { ComponentPreview } from "./ComponentPreview";
import { syncActions } from "./sync-policy";
import { agentPrompt } from "./agent-prompt";

const syncLabels = {
  "local-only": "Local only",
  "local-ahead": "Ready to push",
  "remote-ahead": "Update available",
  diverged: "Changes on both sides",
  "up-to-date": "Up to date",
  unavailable: "Sync unavailable",
};

function SyncIcon({ direction }: { direction: "pull" | "push" }) {
  const down = direction === "pull";
  return <svg aria-hidden="true" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="square" stroke-linejoin="miter">
    <path d={down ? "M8 2.5v8M4.75 7.5 8 10.75l3.25-3.25M3 13.5h10" : "M8 13.5v-8M4.75 8.5 8 5.25l3.25 3.25M3 2.5h10"} />
  </svg>;
}

function DriveMark() {
  return <svg class="drive-mark" aria-hidden="true" viewBox="0 0 24 24">
    <path fill="#0F9D58" d="M8.1 3h7.8l5.1 8.8h-7.8z" />
    <path fill="#F4B400" d="M8.1 3 3 11.8l3.9 6.7 5.1-8.8z" />
    <path fill="#4285F4" d="M6.9 18.5h10.2l3.9-6.7H10.8z" />
  </svg>;
}

function ExternalLinkIcon() {
  return <svg aria-hidden="true" viewBox="0 0 16 16" width="12" height="12" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="square" stroke-linejoin="miter">
    <path d="M6.25 3.25h-3v9.5h9.5v-3M8.25 2.75h5v5M13.25 2.75 7 9" />
  </svg>;
}

function CopyPathIcon() {
  return <svg aria-hidden="true" viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="square" stroke-linejoin="miter">
    <rect x="5.25" y="5.25" width="7.5" height="7.5" />
    <path d="M10.75 5.25v-2h-7.5v7.5h2" />
  </svg>;
}

function AgentIcon() {
  return <svg aria-hidden="true" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="square" stroke-linejoin="miter">
    <rect x="3" y="4.25" width="10" height="8" />
    <path d="M8 2v2.25M5.5 8h.01M10.5 8h.01M6 10.25h4M1.75 6.5H3M13 6.5h1.25" />
  </svg>;
}

function RefreshIcon() {
  return <svg aria-hidden="true" viewBox="0 0 16 16" width="11" height="11" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="square" stroke-linejoin="miter">
    <path d="M12.75 5.25V2.5l-1.3 1.3A5.25 5.25 0 1 0 13 9" />
    <path d="M9.75 5.25h3v-3" />
  </svg>;
}

function PaneToggleIcon({ side, collapsed }: { side: "source" | "preview"; collapsed: boolean }) {
  const pointsRight = side === "source" ? collapsed : !collapsed;
  return <svg aria-hidden="true" viewBox="0 0 16 16" width="13" height="13" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="square" stroke-linejoin="miter">
    <rect x="2.25" y="2.25" width="11.5" height="11.5" />
    <path d={side === "source" ? "M5.25 2.25v11.5" : "M10.75 2.25v11.5"} />
    <path d={pointsRight ? "m7 5.5 2.5 2.5L7 10.5" : "m9 5.5L6.5 8 9 10.5"} />
  </svg>;
}

function ThemeIcon() {
  const dark = theme.value === "dark";
  return <svg aria-hidden="true" viewBox="0 0 16 16" width="14" height="14" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="square">
    {dark ? <>
      <circle cx="8" cy="8" r="2.75" />
      <path d="M8 1.25v1.5M8 13.25v1.5M1.25 8h1.5M13.25 8h1.5M3.23 3.23l1.06 1.06M11.71 11.71l1.06 1.06M12.77 3.23l-1.06 1.06M4.29 11.71l-1.06 1.06" />
    </> : <path d="M13.7 10.15A5.75 5.75 0 0 1 5.85 2.3 5.75 5.75 0 1 0 13.7 10.15Z" />}
  </svg>;
}

let noticeTimer = 0;
function showNotice(message: string, error = false) {
  notice.value = { message, error };
  window.clearTimeout(noticeTimer);
  noticeTimer = window.setTimeout(() => { notice.value = null; }, error ? 9000 : 3500);
}

async function copyWorkspacePath() {
  const path = project.value?.workspacePath;
  if (!path) return;
  try {
    await navigator.clipboard.writeText(path);
    showNotice("Workspace path copied");
  } catch {
    showNotice("Could not copy workspace path", true);
  }
}

async function copyAgentInstructions() {
  const path = project.value?.workspacePath;
  if (!path) return;
  try {
    await navigator.clipboard.writeText(agentPrompt(path));
    showNotice("Agent instructions copied");
  } catch {
    showNotice("Could not copy agent instructions", true);
  }
}

function FileButton({ file, depth, onSelect }: { file: FileItem; depth: number; onSelect: (file: FileItem) => void }) {
  const label = fileLabelParts(file.label);
  return <button
    class={`file-row ${selectedPath.value === file.path ? "is-active" : ""}`}
    style={{ "--tree-depth": depth }}
    title={file.path}
    onClick={() => onSelect(file)}
  >
    <span>{label.prefix}{label.marker && <span class="file-type-marker">{label.marker}</span>}{label.suffix}</span>
    {file.path.endsWith(".css") && <small>CSS</small>}
    {file.path.endsWith(".tmpl") && <small>HTML</small>}
  </button>;
}

function Folder({ tree, depth, onSelect }: { tree: FileTree; depth: number; onSelect: (file: FileItem) => void }) {
  const containsSelection = Boolean(selectedPath.value && [...tree.files, ...tree.folders.flatMap(allFiles)].some((file) => file.path === selectedPath.value));
  return <details class="tree-folder" open={containsSelection || undefined}>
    <summary style={{ "--tree-depth": depth }} title={tree.path}>{tree.name}</summary>
    {tree.folders.map((folder) => <Folder key={folder.path} tree={folder} depth={depth + 1} onSelect={onSelect} />)}
    {tree.files.map((file) => <FileButton key={file.path} file={file} depth={depth + 1} onSelect={onSelect} />)}
  </details>;
}

function allFiles(tree: FileTree): FileItem[] {
  return [...tree.files, ...tree.folders.flatMap(allFiles)];
}

function FileList({ onSelect, onCreateComponent }: { onSelect: (file: FileItem) => void; onCreateComponent: (name: string) => Promise<boolean> }) {
  const groups = groupedFiles(project.value?.files || [], section.value);
  const usesTailwind = project.value?.files.some((file) => file.path === "theme/tailwind.css");
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  if (!groups.length) {
    return <p class="px-3 py-6 text-[0.72rem] leading-relaxed text-faint">No {section.value} yet. Ask your agent to make the first one.</p>;
  }
  return <div class="pb-3">
    {groups.map((group) => <div key={group.name}>
        <div class="file-group">
          <span>{group.name}</span>
          {group.name.endsWith("template") && <small>{usesTailwind ? "Tailwind" : "Structure + styles"}</small>}
          {group.name === "Components" && <button class="component-add" aria-label="Add component" title="Add component" onClick={() => setCreating(true)}>+</button>}
        </div>
        {group.name === "Components" && creating && <form class="component-create" onSubmit={async (event) => {
          event.preventDefault();
          if (!name.trim()) return;
          if (await onCreateComponent(name.trim())) {
            setName("");
            setCreating(false);
          }
        }}>
          <input autoFocus value={name} pattern="[a-z][a-z0-9-]*" placeholder="metric-card" aria-label="Component name" onInput={(event) => setName(event.currentTarget.value.toLowerCase().replace(/\s+/g, "-"))} />
          <button type="submit">Add</button>
          <button type="button" onClick={() => setCreating(false)}>Cancel</button>
        </form>}
        {group.folders.map((folder) => <Folder key={folder.path} tree={folder} depth={0} onSelect={onSelect} />)}
        {group.files.map((file) => <FileButton key={file.path} file={file} depth={0} onSelect={onSelect} />)}
      </div>)}
  </div>;
}

function Sidebar({ onSelect, onSectionChange, onCreateComponent, onPush, onPull, onRefreshSync }: {
  onSelect: (file: FileItem) => void;
  onSectionChange: (nextSection: FileSection) => void;
  onCreateComponent: (name: string) => Promise<boolean>;
  onPush: () => void;
  onPull: () => void;
  onRefreshSync: () => Promise<void>;
}) {
  const syncState = sync.value?.state;
  const backend = "Google Drive";
  const [refreshing, setRefreshing] = useState(false);
  const { canPull, canPush } = syncActions(syncState);
  return <aside class="flex min-w-0 flex-col bg-rail">
    <header class="drive-identity">
      <DriveMark />
      <div class="drive-copy">
        <div class="drive-project-row">
          <strong class="drive-project">{sync.value?.driveName || project.value?.project.name || "Opening project…"}</strong>
          {sync.value?.driveUrl && <a class="drive-external" href={sync.value.driveUrl} target="_blank" rel="noreferrer" aria-label={`Open project in ${backend}`} title={`Open in ${backend}`}><ExternalLinkIcon /></a>}
        </div>
        <div class="sync-status-row">
          <span class={`sync-status sync-${syncState || "checking"}`}>
            {!connected.value ? "Disconnected" : refreshing ? `Checking ${backend}…` : syncState ? syncLabels[syncState] : `Checking ${backend}…`}
          </span>
          <button class={`sync-refresh ${refreshing ? "is-refreshing" : ""}`} disabled={refreshing} aria-label={`Refresh ${backend} status`} title={`Refresh ${backend} status`} onClick={async () => {
            setRefreshing(true);
            try { await onRefreshSync(); } finally { setRefreshing(false); }
          }}><RefreshIcon /></button>
        </div>
      </div>
      <div class="sidebar-tools">
        <button class="sidebar-tool" aria-label="Copy workspace path" title="Copy workspace path" disabled={!project.value?.workspacePath} onClick={copyWorkspacePath}>
          <CopyPathIcon />
        </button>
        <button class="sidebar-tool" aria-label="Copy agent instructions" title="Copy agent instructions" disabled={!project.value?.workspacePath} onClick={copyAgentInstructions}>
          <AgentIcon />
        </button>
        <button class="sidebar-tool" aria-label={`Switch to ${theme.value === "dark" ? "light" : "dark"} mode`}
          title={`Switch to ${theme.value === "dark" ? "light" : "dark"} mode`}
          onClick={() => { theme.value = theme.value === "dark" ? "light" : "dark"; }}>
          <ThemeIcon />
        </button>
      </div>
    </header>
    <div class="sync-actions">
      <button class={`sync-button ${canPull ? syncState === "diverged" ? "is-attention" : "is-suggested" : ""}`} disabled={!canPull} onClick={onPull}><SyncIcon direction="pull" />Pull</button>
      <button class={`sync-button ${canPush ? "is-suggested" : ""}`} disabled={!canPush} onClick={onPush}><SyncIcon direction="push" />Push</button>
    </div>
    <div class="mt-3 grid grid-cols-2 bg-canvas p-0.5" role="tablist" aria-label="Project area">
      {(["content", "templates"] as const).map((item) => <button
        role="tab"
        aria-selected={section.value === item}
        class={`area-tab ${section.value === item ? "is-active" : ""}`}
        onClick={() => onSectionChange(item)}
      >{item[0].toUpperCase() + item.slice(1)}</button>)}
    </div>
    <nav class="mt-1 min-h-0 flex-1 overflow-auto" aria-label={`${section.value} files`}>
      <FileList onSelect={onSelect} onCreateComponent={onCreateComponent} />
    </nav>
  </aside>;
}

function TemplateRelationship({ file, onOpen }: { file: FileItem; onOpen: () => void }) {
  if (!file.templateLabel) return null;
  return <button class="template-link" onClick={onOpen} title={`Open ${file.templateLabel}`}>
    Uses {file.templateLabel.toLowerCase()}
  </button>;
}

function Workspace({ onSourceInput, onOpenTemplate, onSave, onPreview, onReload, onFocusChange }: {
  onSourceInput: (value: string) => void;
  onOpenTemplate: () => void;
  onSave: () => void;
  onPreview: () => void;
  onReload: () => void;
  onFocusChange: (focused: boolean) => void;
}) {
  const file = selectedFile.value;
  const preview = previewPath.value;
  const editor = useRef<MonacoEditorHandle | null>(null);
  const [collapsedPane, setCollapsedPane] = useState<"source" | "preview" | null>(null);
  const previewSource = previewURL(file, preview, previewRevision.value);
  return <section id="workbench" class={`workbench ${collapsedPane ? `is-${collapsedPane}-collapsed` : ""}`} style={{ "--source-share": `${sourceShare.value}%` }}>
    <section class="source-pane relative min-h-0 min-w-0 bg-source">
      <header class="pane-heading">
        <div class="flex min-w-0 items-center gap-2">
          <span class="truncate">{file?.label || "Choose a file"}</span>
          {file && <TemplateRelationship file={file} onOpen={onOpenTemplate} />}
        </div>
        <div class="pane-actions">
          {externalChange.value && <span class="text-muted">File changed</span>}
          {externalChange.value && <button class="is-visible" onClick={onReload}>Reload</button>}
          {file?.editable && <button class="is-visible" disabled={!draftDirty.value} title="Save (Command + S)" onClick={onSave}>Save</button>}
          {(previewStale.value || externalChange.value) && (file?.previewable || file?.component) && <button class="is-visible preview-action" onClick={onPreview}>{draftDirty.value ? "Save & preview" : "Refresh preview"}</button>}
          {file?.editable && <button title="Format document (Shift + Option + F)" onClick={() => editor.current?.format()}>Format</button>}
          <span>{saveState.value}</span>
          <button class="pane-toggle is-visible" aria-label={collapsedPane === "preview" ? "Expand preview" : "Collapse preview"}
            title={collapsedPane === "preview" ? "Expand preview" : "Collapse preview"}
            onClick={() => setCollapsedPane(collapsedPane === "preview" ? null : "preview")}>
            <PaneToggleIcon side="preview" collapsed={collapsedPane === "preview"} />
          </button>
        </div>
      </header>
      {file?.editable ? <MonacoEditor value={source.value} path={file.path} appearance={theme.value} utilities={project.value?.tailwindClasses}
        onChange={onSourceInput} onFocusChange={onFocusChange} onReady={(handle) => { editor.current = handle; }} />
        : <div class="empty-pane">Choose something to work on.</div>}
    </section>
    <Splitter />
    <section class="preview-pane relative min-h-0 min-w-0 bg-preview">
      <header class="pane-heading justify-between">
        <div class="flex min-w-0 items-center gap-2">
          <button class="pane-toggle" aria-label={collapsedPane === "source" ? "Expand source" : "Collapse source"}
            title={collapsedPane === "source" ? "Expand source" : "Collapse source"}
            onClick={() => setCollapsedPane(collapsedPane === "source" ? null : "source")}>
            <PaneToggleIcon side="source" collapsed={collapsedPane === "source"} />
          </button>
          <span class="truncate">{file?.component ? file.label : file?.section === "templates" && preview ? `Example · ${preview.split("/").at(-1)}` : ""}</span>
        </div>
        <span>{renderState.value}</span>
      </header>
      {previewStale.value && (file?.previewable || file?.component) ? <div class="stale-preview">
        <span>Preview paused while you edit.</span>
        <button class="preview-action" onClick={onPreview}>{draftDirty.value ? "Save & preview" : "Refresh preview"}</button>
      </div> : previewSource && file?.component ? <ComponentPreview
        key={`${previewSource}-${previewRevision.value}`}
        url={previewSource}
        source={source.value}
        title={`${file.label} component preview`}
        onReady={() => { renderState.value = ""; }}
      /> : previewSource && usesPdfCanvas(file, preview) ? <PdfPreview
        key={`${previewSource}-${previewRevision.value}`}
        url={previewSource}
        onReady={() => { renderState.value = ""; }}
      /> : previewSource ? <iframe
        key={`${previewSource}-${previewRevision.value}`}
        class="h-[calc(100%-32px)] w-full border-0 bg-preview"
        title="Document preview"
        src={previewSource}
        onLoad={() => { renderState.value = ""; }}
      /> : <div class="empty-pane top-8">{file ? "This file has no visual preview." : "Select content to preview it."}</div>}
      {renderState.value && <div class="render-overlay" role="status" aria-live="polite">
        <span class="render-pulse" aria-hidden="true" />
        <strong>Building preview</strong>
        <span>Typesetting the latest saved version…</span>
      </div>}
    </section>
  </section>;
}

function Splitter() {
  const ref = useRef<HTMLDivElement>(null);
  const setShare = (value: number) => { sourceShare.value = Math.max(25, Math.min(75, value)); };
  const onPointerDown = (event: PointerEvent) => {
    const splitter = ref.current!;
    splitter.setPointerCapture(event.pointerId);
    const move = (moveEvent: PointerEvent) => {
      const rect = splitter.parentElement!.getBoundingClientRect();
      const narrow = matchMedia("(max-width: 900px)").matches;
      setShare(narrow ? ((moveEvent.clientY - rect.top) / rect.height) * 100 : ((moveEvent.clientX - rect.left) / rect.width) * 100);
    };
    const done = () => splitter.removeEventListener("pointermove", move);
    splitter.addEventListener("pointermove", move);
    splitter.addEventListener("pointerup", done, { once: true });
  };
  const onKeyDown = (event: KeyboardEvent) => {
    const narrow = matchMedia("(max-width: 900px)").matches;
    const decrease = narrow ? event.key === "ArrowUp" : event.key === "ArrowLeft";
    const increase = narrow ? event.key === "ArrowDown" : event.key === "ArrowRight";
    if (!decrease && !increase) return;
    event.preventDefault();
    setShare(sourceShare.value + (increase ? 3 : -3));
  };
  return <div ref={ref} class="splitter" role="separator" aria-label="Resize source and preview"
    aria-orientation="vertical" aria-valuemin={25} aria-valuemax={75} aria-valuenow={Math.round(sourceShare.value)}
    tabIndex={0} onPointerDown={onPointerDown} onKeyDown={onKeyDown} />;
}

function SyncFact({ label, value, attention = false }: { label: string; value: string; attention?: boolean }) {
  return <div class={`sync-fact ${attention ? "is-attention" : ""}`}><span>{label}</span><strong>{value}</strong></div>;
}

function ChangeReview({ title, changes, empty }: { title: string; changes: FileChange[] | undefined; empty: string }) {
  return <section class="change-review">
    <header><strong>{title}</strong><span>{changes ? `${changes.length} file${changes.length === 1 ? "" : "s"}` : "Inspecting…"}</span></header>
    {!changes ? <div class="change-review-loading"><span />Comparing file versions…</div>
      : changes.length === 0 ? <p>{empty}</p>
      : <ul>{changes.map((change) => <li key={`${change.kind}:${change.path}`}>
        <span class={`change-kind is-${change.kind}`}>{change.kind[0].toUpperCase()}</span>
        <code title={change.path}>{change.path}</code>
      </li>)}</ul>}
  </section>;
}

function SyncEffects({ items }: { items: string[] }) {
  return <div class="sync-effects"><strong>What happens</strong><ul>{items.map((item) => <li>{item}</li>)}</ul></div>;
}

function PushDialog({ dialog, onConfirm }: { dialog: preact.RefObject<HTMLDialogElement>; onConfirm: (message: string, forceWithLease: string) => void }) {
  const message = useRef<HTMLInputElement>(null);
  const confirmation = useRef<HTMLInputElement>(null);
  const diverged = sync.value?.state === "diverged";
  const backend = "Google Drive";
  return <dialog ref={dialog} class="stamp-dialog" onClose={(event) => event.currentTarget.querySelector("form")?.reset()}>
    <form method="dialog" class="p-6">
      <span class="dialog-kicker">Push to {backend}</span>
      <h2>{diverged ? `Replace the ${backend} version?` : "Share these local changes?"}</h2>
      <p>{diverged ? `Local and ${backend} both changed. This publishes your local workspace over the version shown below.` : "This creates a complete shared version. Your local files remain unchanged."}</p>
      <div class="sync-facts">
        <SyncFact label="Destination" value={sync.value?.driveName || project.value?.project.name || `New ${backend} project`} />
        <SyncFact label="Local" value={sync.value?.localChanged ? "Changes ready" : "No changes"} />
        <SyncFact label={backend} value={sync.value?.remoteChanged ? `Changed since local v${sync.value?.baseVersion || "—"}` : `Current · v${sync.value?.baseVersion || "—"}`} attention={Boolean(sync.value?.remoteChanged)} />
      </div>
      <div class="change-reviews">
        <ChangeReview title="Local changes to publish" changes={syncReview.value?.local} empty="No file changes found." />
        {diverged && <ChangeReview title={`${backend} changes being replaced`} changes={syncReview.value?.remote} empty={`Only ${backend} version metadata changed.`} />}
      </div>
      <SyncEffects items={[
        `The complete local project becomes the new canonical ${backend} version.`,
        `PDFs and other generated outputs are rebuilt and mirrored to ${backend}.`,
        `The previous ${backend} revision remains recoverable through Stamp's source history.`,
      ]} />
      <label class="dialog-field"><span>Version note</span><input ref={message} required placeholder="Updated the quarterly summary" /></label>
      {diverged && <label class="dialog-confirm"><input ref={confirmation} type="checkbox" required /><span>I reviewed the {backend} drift and want to replace that version.</span></label>}
      <div class="dialog-actions"><button type="button" onClick={() => dialog.current?.close("cancel")}>Cancel</button><button type="button" class="primary" onClick={() => {
        if (!message.current?.value.trim()) return message.current?.reportValidity();
        if (diverged && !confirmation.current?.checked) return confirmation.current?.reportValidity();
        onConfirm(message.current?.value.trim() || "", diverged ? sync.value?.remoteVersion || "" : "");
      }}>{diverged ? `Replace ${backend} version` : "Push version"}</button></div>
    </form>
  </dialog>;
}

function PullDialog({ dialog, onConfirm }: { dialog: preact.RefObject<HTMLDialogElement>; onConfirm: () => void }) {
  const confirmation = useRef<HTMLInputElement>(null);
  const localChanged = Boolean(sync.value?.localChanged);
  const backend = "Google Drive";
  return <dialog ref={dialog} class="stamp-dialog" onClose={(event) => event.currentTarget.querySelector("form")?.reset()}><form method="dialog" class="p-6">
    <span class="dialog-kicker">Pull from {backend}</span>
    <h2>Replace this local workspace?</h2>
    <p>The {backend} version becomes your working copy. Stamp saves a recovery copy before replacing any local changes.</p>
    <div class="sync-facts">
      <SyncFact label="Source" value={sync.value?.driveName || project.value?.project.name || backend} />
      <SyncFact label={backend} value={`Newer than local v${sync.value?.baseVersion || "—"}`} attention />
      <SyncFact label="Local" value={localChanged ? "Changes moved to recovery" : "No changes to preserve"} />
    </div>
    <div class="change-reviews">
      <ChangeReview title={`Incoming changes from ${backend}`} changes={syncReview.value?.remote} empty={`Only ${backend} version metadata changed.`} />
      {localChanged && <ChangeReview title="Local changes being replaced" changes={syncReview.value?.local} empty="No local file changes found." />}
    </div>
    <SyncEffects items={[
      `The ${backend} project becomes the complete local working copy.`,
      localChanged ? "Your current local project is saved under .stamp/recovery/ first." : "No recovery archive is needed because local files are unchanged.",
      `Generated outputs are rebuilt from the ${backend} source.`,
    ]} />
    <label class="dialog-confirm"><input ref={confirmation} type="checkbox" required /><span>I understand the {backend} version will replace this workspace.</span></label>
    <div class="dialog-actions"><button type="button" onClick={() => dialog.current?.close("cancel")}>Cancel</button><button type="button" class="primary" onClick={() => {
      if (!confirmation.current?.checked) return confirmation.current?.reportValidity();
      onConfirm();
    }}>Pull {backend} version</button></div>
  </form></dialog>;
}

export function App() {
  const editorFocused = useRef(false);
  const savedSource = useRef("");
  const pushDialog = useRef<HTMLDialogElement>(null);
  const pullDialog = useRef<HTMLDialogElement>(null);

  const regeneratePreview = () => {
    const file = selectedFile.value;
    previewPath.value = file ? selectPreview(file) : null;
    if (!previewPath.value && !file?.component) return;
    renderState.value = "Rendering…";
    previewRevision.value += 1;
    previewStale.value = false;
  };

  const refresh = async (reconcileSource = false) => {
    const [nextProject, nextSync] = await Promise.all([api.project(), api.sync()]);
    project.value = nextProject;
    sync.value = nextSync;
    connected.value = true;
    const current = nextProject.files.find((file) => file.path === selectedPath.value);
    if (!current) {
      const initial = nextProject.files.find((file) => file.section === "content" && file.path.endsWith(".page.md"))
        || nextProject.files.find((file) => file.section === "content" && file.previewable);
      if (initial) await chooseFile(initial);
    } else {
      previewPath.value = selectPreview(current);
      if (reconcileSource && current.editable) {
        const diskSource = await api.readFile(current.path);
        if (diskSource !== savedSource.current) {
          if (editorFocused.current || draftDirty.value) {
            externalChange.value = true;
            previewStale.value = true;
          } else {
            source.value = diskSource;
            savedSource.current = diskSource;
            externalChange.value = false;
            previewStale.value = true;
            regeneratePreview();
          }
        }
      }
    }
  };

  const save = async (): Promise<boolean> => {
    const file = selectedFile.value;
    if (!file?.editable || !draftDirty.value) return true;
    if (externalChange.value) {
      showNotice("This file changed outside Studio. Reload it before saving so those changes are not overwritten.", true);
      return false;
    }
    saveState.value = "Saving…";
    try {
      const result = await api.writeFile(file.path, source.value);
      savedSource.current = source.value;
      draftDirty.value = false;
      externalChange.value = false;
      saveState.value = "Saved";
      window.setTimeout(() => { if (saveState.value === "Saved") saveState.value = ""; }, 1200);
      sync.value = await api.sync();
      if (result.warning) showNotice(result.warning, true);
      return true;
    } catch (error) {
      saveState.value = "Save failed";
      showNotice((error as Error).message, true);
      return false;
    }
  };

  const saveAndPreview = async () => {
    if (await save()) regeneratePreview();
  };

  const reloadSource = async () => {
    const file = selectedFile.value;
    if (!file?.editable) return;
    const diskSource = await api.readFile(file.path);
    source.value = diskSource;
    savedSource.current = diskSource;
    draftDirty.value = false;
    externalChange.value = false;
    saveState.value = "";
    previewStale.value = true;
    regeneratePreview();
  };

  const chooseFile = async (file: FileItem) => {
    if (draftDirty.value) {
      showNotice("Save or reload this draft before opening another file.");
      return;
    }
    selectedPath.value = file.path;
    section.value = file.section;
    source.value = file.editable ? await api.readFile(file.path) : "";
    savedSource.current = source.value;
    previewPath.value = selectPreview(file);
    previewRevision.value += 1;
    renderState.value = previewPath.value || file.component ? "Rendering…" : "";
    draftDirty.value = false;
    externalChange.value = false;
    previewStale.value = false;
  };

  const onSourceInput = (value: string) => {
    source.value = value;
    draftDirty.value = true;
    previewStale.value = true;
    renderState.value = "";
    saveState.value = "Unsaved";
  };

  const openRelatedTemplate = () => {
    const label = selectedFile.value?.templateLabel;
    const target = project.value?.files.find((file) => file.section === "templates" && file.group === label && file.label === "Structure");
    if (target) chooseFile(target);
  };

  const changeSection = async (nextSection: FileSection) => {
    if (section.value === nextSection) return;
    if (draftDirty.value) {
      showNotice("Save or reload this draft before changing sections.");
      return;
    }
    section.value = nextSection;
    const candidates = visibleFiles(project.value?.files || [], nextSection);
    const preferred = nextSection === "templates"
      ? candidates.find((file) => file.group === "Page template" && file.label === "Structure")
      : candidates.find((file) => file.previewable);
    const next = preferred || candidates[0];
    if (next) await chooseFile(next);
    else {
      selectedPath.value = null;
      source.value = "";
      previewPath.value = null;
    }
  };

  const createComponent = async (name: string) => {
    try {
      const result = await api.createComponent(name);
      await refresh();
      const created = project.value?.files.find((file) => file.path === result.path);
      if (created) await chooseFile(created);
      showNotice(result.message);
      return true;
    } catch (error) {
      showNotice((error as Error).message, true);
      return false;
    }
  };

  const runSyncAction = async (action: () => Promise<{ message: string }>, dialog: HTMLDialogElement | null) => {
    try {
      if (!(await save())) return;
      const result = await action();
      dialog?.close();
      showNotice(result.message);
      await refresh(true);
    } catch (error) { showNotice((error as Error).message, true); }
  };

  const openSyncDialog = (dialog: HTMLDialogElement | null) => {
    if (!dialog) return;
    syncReview.value = null;
    dialog.showModal();
    api.syncDetails().then((details) => { syncReview.value = details; }).catch((error) => {
      syncReview.value = { local: [], remote: [], error: (error as Error).message };
      showNotice(`Could not inspect file changes: ${(error as Error).message}`, true);
    });
  };

  const refreshSyncStatus = async () => {
    try {
      sync.value = await api.sync();
      connected.value = true;
      showNotice("Google Drive status refreshed");
    } catch (error) {
      connected.value = false;
      showNotice((error as Error).message, true);
    }
  };

  useEffect(() => {
    const storedTheme = localStorage.getItem("stamp-theme");
    theme.value = storedTheme === "light" ? "light" : "dark";
    sourceShare.value = Number(localStorage.getItem("stamp-source-share")) || 42;
    refresh().catch((error) => { connected.value = false; showNotice(error.message, true); });
    const events = new EventSource("api/events");
    events.addEventListener("ready", () => { connected.value = true; });
    events.addEventListener("change", () => refresh(true).catch(() => { connected.value = false; }));
    events.addEventListener("error", () => { connected.value = false; });
    return () => events.close();
  }, []);

  useEffect(() => {
    const keydown = (event: KeyboardEvent) => {
      if (!(event.metaKey || event.ctrlKey) || event.key.toLowerCase() !== "s") return;
      event.preventDefault();
      save();
    };
    window.addEventListener("keydown", keydown);
    return () => window.removeEventListener("keydown", keydown);
  }, []);

  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (!draftDirty.value) return;
      event.preventDefault();
      event.returnValue = "";
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, []);

  useEffect(() => {
    document.documentElement.dataset.theme = theme.value;
    localStorage.setItem("stamp-theme", theme.value);
  }, [theme.value]);
  useEffect(() => { localStorage.setItem("stamp-source-share", String(sourceShare.value)); }, [sourceShare.value]);

  return <>
    <main class="app-shell">
      <Sidebar onSelect={chooseFile} onSectionChange={changeSection} onCreateComponent={createComponent} onPush={() => openSyncDialog(pushDialog.current)} onPull={() => openSyncDialog(pullDialog.current)} onRefreshSync={refreshSyncStatus} />
      <Workspace onSourceInput={onSourceInput} onOpenTemplate={openRelatedTemplate} onSave={save} onPreview={saveAndPreview} onReload={reloadSource}
        onFocusChange={(focused) => {
          editorFocused.current = focused;
          if (!focused && externalChange.value && !draftDirty.value) reloadSource().catch((error) => showNotice(error.message, true));
        }} />
    </main>
    <PushDialog dialog={pushDialog} onConfirm={(message, forceWithLease) => runSyncAction(() => api.push(message, forceWithLease), pushDialog.current)} />
    <PullDialog dialog={pullDialog} onConfirm={() => runSyncAction(api.pull, pullDialog.current)} />
    {notice.value && <aside class={`notice ${notice.value.error ? "is-error" : ""}`} role="status">{notice.value.message}</aside>}
  </>;
}
