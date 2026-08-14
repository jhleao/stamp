import type * as Monaco from "monaco-editor/esm/vs/editor/editor.api";

export const stampLanguageId = "stamp";

const stampLanguage: Monaco.languages.IMonarchLanguage = {
  defaultToken: "",
  tokenPostfix: "",
  brackets: [
    { open: "{", close: "}", token: "delimiter.bracket.stamp" },
    { open: "[", close: "]", token: "delimiter.bracket.stamp" },
    { open: "(", close: ")", token: "delimiter.bracket.stamp" },
  ],
  tokenizer: {
    root: [
      [/^\s*<!--/, { token: "comment.stamp", next: "@comment" }],
      [/^\s*```[\w-]*\s*$/, { token: "string.code.fence.stamp", next: "@codeblock" }],
      [/^(\s*)(#{1,6})(\s+)(.*)$/, ["", "markup.heading.marker.stamp", "", "markup.heading.stamp"]],
      [/^(\s*)(>)(\s?)/, ["", "comment.quote.stamp", ""]],
      [/^(\s*)([-+*]|\d+\.)(\s+)/, ["", "keyword.list.stamp", ""]],
      [/^\s*[A-Za-z_][\w.-]*(?=\s*:)/, "key.frontmatter.stamp"],
      [/<\/?[A-Z][\w.-]*/, { token: "tag.component.stamp", next: "@tag" }],
      [/<\/?[a-z][\w.-]*/, { token: "tag.html.stamp", next: "@tag" }],
      [/`[^`\n]+`/, "string.code.inline.stamp"],
      [/\*\*[^*\n]+\*\*|__[^_\n]+__/, "markup.bold.stamp"],
      [/\*[^*\n]+\*|_[^_\n]+_/, "markup.italic.stamp"],
      [/!?\[[^\]\n]*\]\([^\)\n]+\)/, "string.link.stamp"],
      [/\{[^}\n]*\}/, "expression.stamp"],
    ],
    tag: [
      [/\bclass(?:Name)?\s*=\s*"[^"]*"/, "string.tailwind.stamp"],
      [/\bclass(?:Name)?\s*=\s*'[^']*'/, "string.tailwind.stamp"],
      [/\bclass(?:Name)?\s*=\s*\{\s*`[^`]*`\s*\}/, "string.tailwind.stamp"],
      [/[A-Za-z_:][\w:.-]*/, "attribute.name.stamp"],
      [/=/, "delimiter.equals.stamp"],
      [/"[^"\n]*"|'[^'\n]*'/, "string.attribute.stamp"],
      [/\{[^}\n]*\}/, "expression.stamp"],
      [/\/?>/, { token: "delimiter.tag.stamp", next: "@pop" }],
      [/\s+/, ""],
    ],
    comment: [
      [/-->/, { token: "comment.stamp", next: "@pop" }],
      [/./, "comment.stamp"],
    ],
    codeblock: [
      [/^\s*```\s*$/, { token: "string.code.fence.stamp", next: "@pop" }],
      [/.*$/, "string.code.block.stamp"],
    ],
  },
};

let registered = false;

export function registerStampLanguage(monaco: typeof Monaco) {
  if (registered) return;
  registered = true;
  monaco.languages.register({
    id: stampLanguageId,
    aliases: ["Stamp", "stamp"],
    extensions: [".page.md", ".deck.md"],
  });
  monaco.languages.setLanguageConfiguration(stampLanguageId, {
    comments: { blockComment: ["<!--", "-->"] },
    brackets: [["{", "}"], ["[", "]"], ["(", ")"]],
    autoClosingPairs: [
      { open: "{", close: "}" },
      { open: "[", close: "]" },
      { open: "(", close: ")" },
      { open: "\"", close: "\"" },
      { open: "'", close: "'" },
      { open: "`", close: "`" },
    ],
    surroundingPairs: [
      { open: "{", close: "}" },
      { open: "[", close: "]" },
      { open: "(", close: ")" },
      { open: "\"", close: "\"" },
      { open: "'", close: "'" },
      { open: "`", close: "`" },
      { open: "**", close: "**" },
      { open: "_", close: "_" },
    ],
  });
  monaco.languages.setMonarchTokensProvider(stampLanguageId, stampLanguage);
}
