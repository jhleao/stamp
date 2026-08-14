import { describe, expect, it } from "vitest";
import { fileLabelParts, fileSelectionOrder, fileTree, groupedFiles, visibleFiles } from "./navigation";
import type { FileItem } from "./types";

const item = (path: string, group = "Written pages", label = path.split("/").at(-1)!): FileItem => ({
  path, group, label, section: "content", editable: true, previewable: true,
});

describe("Studio navigation", () => {
  it("isolates the full extension tail so the basename stays prominent", () => {
    expect(fileLabelParts("phase-two.page.md")).toEqual({ prefix: "phase-two", marker: ".page.md", suffix: "" });
    expect(fileLabelParts("page.html.tmpl")).toEqual({ prefix: "page", marker: ".html.tmpl", suffix: "" });
    expect(fileLabelParts("README.md")).toEqual({ prefix: "README", marker: ".md", suffix: "" });
  });

  it("turns nested project paths into folders without changing file identity", () => {
    const clickup = item("documents/Clients/ClickUp/phase-2.page.md");
    const tree = fileTree([item("documents/overview.page.md"), clickup], "Written pages");
    expect(tree.files.map((file) => file.path)).toEqual(["documents/overview.page.md"]);
    expect(tree.folders[0].name).toBe("Clients");
    expect(tree.folders[0].path).toBe("documents/Clients");
    expect(tree.folders[0].folders[0].name).toBe("ClickUp");
    expect(tree.folders[0].folders[0].path).toBe("documents/Clients/ClickUp");
    expect(tree.folders[0].folders[0].files[0]).toBe(clickup);
  });

  it("keeps directories ahead of files and names them alphabetically", () => {
    const tree = fileTree([
      item("documents/Zeta/z.page.md"), item("documents/Alpha/a.page.md"),
      item("documents/z.page.md"), item("documents/a.page.md"),
    ], "Written pages");
    expect(tree.folders.map((folder) => folder.name)).toEqual(["Alpha", "Zeta"]);
    expect(tree.files.map((file) => file.label)).toEqual(["a.page.md", "z.page.md"]);
  });

  it("preserves section ordering and filters internal files", () => {
    const files = [
      item("assets/photo.png", "Assets"),
      item("decks/talk.deck.md", "Slide decks"),
      item("documents/note.page.md"),
      { ...item("outputs/note.pdf", "Project"), previewable: false },
    ];
    expect(groupedFiles(files, "content").map((group) => group.name)).toEqual(["Written pages", "Slide decks", "Assets"]);
    expect(visibleFiles(files, "content")).toHaveLength(3);
  });

  it("uses rendered tree order for shift-selection ranges", () => {
    const files = [
      item("documents/root.page.md"),
      item("documents/Beta/b.page.md"),
      item("documents/Alpha/nested/a.page.md"),
      item("documents/Alpha/z.page.md"),
      item("decks/talk.deck.md", "Slide decks"),
    ];
    expect(fileSelectionOrder(files, "content").map((file) => file.path)).toEqual([
      "documents/Alpha/nested/a.page.md",
      "documents/Alpha/z.page.md",
      "documents/Beta/b.page.md",
      "documents/root.page.md",
      "decks/talk.deck.md",
    ]);
  });
});
