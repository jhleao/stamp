# Themes and components

Stamp Studio is built with Preact, and reusable document components use the
same familiar TSX shape. Stamp compiles them inside a restricted renderer to
inert HTML and local CSS: a shared document still cannot access the DOM, read
files, call the network, or attach event handlers.

In Studio, **Content** contains the words and data your team changes.
**Templates** contains presentation: Page and Deck structures, one Tailwind
design system, reusable components, and realistic examples. Generated CSS is
hidden. Selecting any template file previews the matching example
automatically. Use the `+` beside Components to create a new shared component.

A theme is a folder you can copy, hand to an agent, or snapshot into a project.
It has no install step, package manager, or live dependency.

```text
my-theme/
  page.html.tmpl       # outer frame for written pages
  tailwind.css          # tokens and print primitives
  page.css              # generated; never hand-edit
  deck.html.tmpl       # outer frame for decks
  deck.css              # generated; never hand-edit
  components/          # one Preact-style .tsx component per file
  examples/            # realistic visual tests
  assets/
  fonts/
  outputs/              # generated project previews
```

Every project owns this folder. Edit components, examples, assets, and Tailwind
through Studio's Templates section, then preview the affected project files.
There is no separate theme package, installation, or upgrade lifecycle.

## Content stays readable

Markdown carries the words and data:

```md
---
title: Q3 board update
period: Q3
---

# Quarter at a glance

<metric-card value="$4.2M" change="18%">
Annual recurring revenue
</metric-card>
```

The appearance lives in `components/metric-card.tsx`, using Tailwind
utilities:

```tsx
export default function MetricCard({ props, children }) {
  return (
    <figure className="grid grid-cols-[1fr_auto] gap-3 border-y border-zinc-300 py-5">
      <strong className="text-4xl tracking-tight">{props.value}</strong>
      <span className="text-sm text-zinc-500">{props.change}</span>
      <figcaption className="col-span-2 text-sm">{children}</figcaption>
    </figure>
  );
}
```

A component receives `props` for tag attributes, `children` for its rendered
body, `meta` for YAML front matter, and `format` for its rendering context.
`format` is `page` for written documents, `deck` for slides, and `component`
in Studio's isolated component preview. Use it when one semantic component
needs different geometry in a page and a deck:

```tsx
export default function Section({ format, children }) {
  const geometry = format === "deck"
    ? "h-[7.5in] w-[13.333in] p-[.8in]"
    : "min-h-[297mm] w-[210mm] p-[20mm]";
  return <section className={geometry}>{children}</section>;
}
```

The outer `page.html.tmpl` and `deck.html.tmpl` shells also receive `.Format`.
Ordinary TypeScript conditions and array mapping handle variants and repeated
local data. Components may use other
components, and may emit inline SVG for diagrams and charts. CSS supports the
browser's full print, grid, flex, SVG, and paged-media layout system.

The authoring model is intentionally Preact-like—named components, props,
children, conditions, and nested composition—but the output stays portable,
reviewable, and safe to share through Drive.

## A good agent request

> Open this Stamp project's theme. Make the examples look like our brand, create reusable
> components for repeated patterns, add a dense and a long-content example,
> then inspect every result in Studio. Keep document Markdown about
> content, not layout.

The generated `README.md` inside every theme gives an agent the same contract.
