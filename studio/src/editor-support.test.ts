import { describe, expect, it } from "vitest";
import { languageForPath, tailwindToken } from "./editor-support";

describe("editor language selection", () => {
  it.each([
    ["theme/page.html.tmpl", "html"],
    ["theme/components/Card.tsx", "typescript"],
    ["theme/tailwind.css", "css"],
    ["stamp.json", "json"],
    ["documents/report.page.md", "stamp"],
    ["decks/kickoff.deck.md", "stamp"],
    ["README.md", "markdown"],
    ["assets/photo.png", "plaintext"],
  ])("maps %s to %s", (path, language) => expect(languageForPath(path)).toBe(language));
});

describe("Tailwind completion context", () => {
  it("replaces only the current utility inside class", () => {
    const line = `<section class="grid gap-">`;
    const cursor = line.indexOf('"', 16) + 1;
    expect(tailwindToken(line, cursor)).toEqual({ token: "gap-", startColumn: cursor - 4 });
  });

  it("supports className and ignores prose", () => {
    const line = `<div className='text-`;
    expect(tailwindToken(line, line.length + 1)).toEqual({ token: "text-", startColumn: line.length - 4 });
    expect(tailwindToken("Write text-sm here", 19)).toBeNull();
  });

  it("supports TSX template strings", () => {
    const line = "<div className={`grid gap-";
    expect(tailwindToken(line, line.length + 1)).toEqual({ token: "gap-", startColumn: line.length - 3 });
  });
});
