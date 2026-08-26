import { beforeAll, describe, expect, it } from "vitest";
import type * as Monaco from "monaco-editor/esm/vs/editor/editor.api";

let monaco: typeof Monaco;
let stampLanguageId: string;

beforeAll(async () => {
  window.matchMedia = () => ({
    matches: false,
    media: "",
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent: () => false,
  });
  monaco = await import("monaco-editor/esm/vs/editor/editor.api");
  const stamp = await import("./stamp-language");
  stampLanguageId = stamp.stampLanguageId;
  stamp.registerStampLanguage(monaco);
});

function tokenTypes(source: string) {
  return monaco.editor.tokenize(source, stampLanguageId).flatMap((line) => line.map((token) => token.type));
}

describe("Stamp language highlighting", () => {
  it("distinguishes components, props, and Tailwind from prose", () => {
    const types = tokenTypes('<MetricCard value="94%" className="grid gap-4">Hello</MetricCard>');
    expect(types).toContain("tag.component.stamp");
    expect(types).toContain("attribute.name.stamp");
    expect(types).toContain("delimiter.equals.stamp");
    expect(types).toContain("string.attribute.stamp");
    expect(types).toContain("string.tailwind.stamp");
  });

  it("keeps Markdown structure visible", () => {
    const types = tokenTypes("# Evaluation\n\n- **Measured** against `baseline`");
    expect(types).toContain("markup.heading.stamp");
    expect(types).toContain("keyword.list.stamp");
    expect(types).toContain("markup.bold.stamp");
    expect(types).toContain("string.code.inline.stamp");
  });
});
