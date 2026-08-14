const componentTag = /<\/?[A-Z][\w.-]*(?:\s[^<>]*?)?\/?>/g;

function tagBalance(line: string) {
  let opens = 0;
  let closes = 0;
  for (const match of line.matchAll(componentTag)) {
    const tag = match[0];
    if (tag.startsWith("</")) closes++;
    else if (!tag.endsWith("/>")) opens++;
  }
  return { opens, closes };
}

export function formatStampDocument(source: string, indentSize = 2) {
  const lines = source.split("\n");
  let depth = 0;
  let fenced = false;

  return lines.map((line) => {
    const trimmed = line.trim();
    const fence = /^```/.test(trimmed);
    if (fenced) {
      if (fence) fenced = false;
      return line.replace(/\s+$/, "");
    }

    const balance = tagBalance(trimmed);
    const leadingClose = trimmed.startsWith("</") ? 1 : 0;
    const lineDepth = Math.max(0, depth - leadingClose);
    const formatted = trimmed === "" ? "" : `${" ".repeat(lineDepth * indentSize)}${trimmed}`;

    depth = Math.max(0, depth + balance.opens - balance.closes);
    if (fence) fenced = true;
    return formatted;
  }).join("\n");
}
