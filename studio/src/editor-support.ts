export function languageForPath(path: string) {
  if (path.endsWith(".tsx")) return "typescript";
  if (path.endsWith(".html.tmpl")) return "html"; // Document shells only.
  if (path.endsWith(".css")) return "css";
  if (path.endsWith(".json")) return "json";
  if (/\.(?:page|deck)\.md$/.test(path)) return "stamp";
  if (path.endsWith(".md")) return "markdown";
  return "plaintext";
}

export function tailwindToken(line: string, column: number) {
  const before = line.slice(0, Math.max(0, column - 1));
  const attribute = /\bclass(?:Name)?\s*=\s*\{?\s*["'`][^"'`]*$/.exec(before);
  if (!attribute) return null;
  const token = /[^\s"']*$/.exec(before)?.[0] || "";
  return { token, startColumn: column - token.length };
}
