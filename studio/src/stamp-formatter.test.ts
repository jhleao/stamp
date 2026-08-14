import { describe, expect, it } from "vitest";
import { formatStampDocument } from "./stamp-formatter";

describe("Stamp document formatting", () => {
  it("indents Markdown and nested components", () => {
    expect(formatStampDocument(`<Slide title="Summary">
# Result
<Columns cols="2">
<Card>
Measured outcome
</Card>
</Columns>
</Slide>`)).toBe(`<Slide title="Summary">
  # Result
  <Columns cols="2">
    <Card>
      Measured outcome
    </Card>
  </Columns>
</Slide>`);
  });

  it("ignores self-closing and inline component pairs", () => {
    expect(formatStampDocument(`<Slide>
<Logo />
<Badge>Ready</Badge>
After
</Slide>`)).toBe(`<Slide>
  <Logo />
  <Badge>Ready</Badge>
  After
</Slide>`);
  });

  it("does not rewrite fenced code contents", () => {
    expect(formatStampDocument(`<Card>
\`\`\`tsx
  const value = 1;
\`\`\`
</Card>`)).toBe(`<Card>
  \`\`\`tsx
  const value = 1;
\`\`\`
</Card>`);
  });

  it("is idempotent", () => {
    const source = `<Slide>\n  <Card>\n    Copy\n  </Card>\n</Slide>`;
    expect(formatStampDocument(formatStampDocument(source))).toBe(source);
  });
});
