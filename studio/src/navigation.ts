import type { FileItem, FileSection } from "./types";

export interface FileTree {
  name: string;
  path: string;
  folders: FileTree[];
  files: FileItem[];
}

export interface FileLabelParts {
  prefix: string;
  marker: string;
  suffix: string;
}

export function fileLabelParts(label: string): FileLabelParts {
  const extension = label.indexOf(".", 1);
  if (extension < 0) return { prefix: label, marker: "", suffix: "" };
  return {
    prefix: label.slice(0, extension),
    marker: label.slice(extension),
    suffix: "",
  };
}

const groupOrder: Record<FileSection, string[]> = {
  content: ["Written pages", "Slide decks", "Spreadsheets", "Assets", "Project"],
  templates: ["Page template", "Deck template", "Design system", "Components", "Examples", "About templates", "Template files"],
};

const groupRoots: Record<string, string> = {
  "Written pages": "documents/",
  "Slide decks": "decks/",
  Spreadsheets: "spreadsheets/",
  Assets: "assets/",
  "Page template": "theme/",
  "Deck template": "theme/",
  "Design system": "theme/",
  Components: "theme/components/",
  Examples: "theme/examples/",
  "About templates": "theme/",
  "Template files": "theme/",
};

function visible(file: FileItem, activeSection: FileSection) {
  if (file.hidden || file.section !== activeSection || file.path.startsWith("outputs/")) return false;
  return !["AGENTS.md", "CLAUDE.md", ".mcp.json", "stamp.yaml"].includes(file.path);
}

export function visibleFiles(files: FileItem[], activeSection: FileSection) {
  const labels = ["Structure", "Styles"];
  return files.filter((file) => visible(file, activeSection)).sort((a, b) => {
    const aGroup = groupOrder[activeSection].indexOf(a.group);
    const bGroup = groupOrder[activeSection].indexOf(b.group);
    const groupDifference = (aGroup < 0 ? 99 : aGroup) - (bGroup < 0 ? 99 : bGroup);
    if (groupDifference) return groupDifference;
    const aLabel = labels.indexOf(a.label);
    const bLabel = labels.indexOf(b.label);
    if (aLabel >= 0 || bLabel >= 0) return (aLabel < 0 ? 99 : aLabel) - (bLabel < 0 ? 99 : bLabel);
    return a.path.localeCompare(b.path);
  });
}

function relativeParts(file: FileItem) {
  const root = groupRoots[file.group] || "";
  const relative = root && file.path.startsWith(root) ? file.path.slice(root.length) : file.path;
  return relative.split("/").filter(Boolean);
}

export function fileTree(files: FileItem[], group: string): FileTree {
  const root: FileTree = { name: group, path: "", folders: [], files: [] };
  for (const file of files.filter((candidate) => candidate.group === group)) {
    const parts = relativeParts(file);
    let node = root;
    for (const folder of parts.slice(0, -1)) {
      const path = node.path ? `${node.path}/${folder}` : folder;
      let child = node.folders.find((candidate) => candidate.name === folder);
      if (!child) {
        child = { name: folder, path, folders: [], files: [] };
        node.folders.push(child);
      }
      node = child;
    }
    node.files.push(file);
  }
  const sort = (node: FileTree) => {
    node.folders.sort((a, b) => a.name.localeCompare(b.name));
    node.files.sort((a, b) => a.label.localeCompare(b.label));
    node.folders.forEach(sort);
  };
  sort(root);
  return root;
}

export function groupedFiles(files: FileItem[], activeSection: FileSection) {
  const visible = visibleFiles(files, activeSection);
  return groupOrder[activeSection]
    .filter((group) => visible.some((file) => file.group === group))
    .map((group) => fileTree(visible, group));
}
