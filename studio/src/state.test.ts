import { describe, expect, it } from "vitest";
import { selectPreview } from "./state";
import type { FileItem } from "./types";

function file(overrides: Partial<FileItem>): FileItem {
  return {
    path: "documents/report.page.md",
    editable: true,
    previewable: true,
    section: "content",
    group: "Written pages",
    label: "report.page.md",
    ...overrides,
  };
}

describe("selectPreview", () => {
  it("previews content directly", () => {
    expect(selectPreview(file({}))).toBe("documents/report.page.md");
  });

  it("previews a template through its example", () => {
    expect(selectPreview(file({ path: "theme/page.css", previewable: false, previewPath: "theme/examples/example.page.md" })))
      .toBe("theme/examples/example.page.md");
  });

  it("clears files without a visual representation", () => {
    expect(selectPreview(file({ path: "theme/README.md", previewable: false }))).toBeNull();
  });
});
