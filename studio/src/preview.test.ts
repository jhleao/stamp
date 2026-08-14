import { describe, expect, it } from "vitest";
import { previewURL, usesPdfCanvas } from "./preview";
import type { FileItem } from "./types";

const file = (values: Partial<FileItem> = {}): FileItem => ({
  path: "documents/report.page.md",
  editable: true,
  previewable: true,
  section: "content",
  group: "Written pages",
  label: "Report",
  ...values,
});

describe("previewURL", () => {
  it("returns the PDF endpoint without browser-viewer fragments", () => {
    expect(previewURL(file(), "documents/report.page.md", 4)).toBe(
      "api/preview?path=documents%2Freport.page.md&at=4",
    );
  });

  it("keeps isolated components on their HTML preview", () => {
    expect(previewURL(file({ component: "metric-card" }), null, 2)).toBe(
      "api/component-preview?name=metric-card&at=2",
    );
  });

  it("adds component prop overrides without affecting document previews", () => {
    expect(previewURL(file({ component: "band" }), null, 2, { full: "true", no: "03" })).toBe(
      "api/component-preview?name=band&at=2&prop.full=true&prop.no=03",
    );
  });

  it("returns no preview without a path", () => {
    expect(previewURL(file(), null, 1)).toBeNull();
  });

  it("uses the canvas viewer only for paginated formats", () => {
    expect(usesPdfCanvas(file(), "documents/report.page.md")).toBe(true);
    expect(usesPdfCanvas(file({ path: "decks/talk.deck.md" }), "decks/talk.deck.md")).toBe(true);
    expect(usesPdfCanvas(file({ path: "spreadsheets/model.xlsx" }), "spreadsheets/model.xlsx")).toBe(false);
    expect(usesPdfCanvas(file({ component: "metric-card" }), "theme/examples/report.page.md")).toBe(false);
  });
});
