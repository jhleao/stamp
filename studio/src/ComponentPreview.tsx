import { useEffect, useMemo, useState } from "preact/hooks";

export interface ComponentControl { name: string; value: string; boolean: boolean }

export function componentControls(source: string): ComponentControl[] {
  const names = [...source.matchAll(/\bprops\.([A-Za-z][A-Za-z0-9_-]*)/g)].map((match) => match[1]);
  return [...new Set(names)].sort().map((name) => ({
    name,
    boolean: ["full", "compact", "inverse", "featured"].includes(name),
    value: name === "cols" ? "3"
      : ["ratio", "divider", "full", "compact", "inverse", "featured"].includes(name) ? ""
      : ["index", "number", "no"].includes(name) ? "01"
      : ["title", "heading"].includes(name) ? "A clear component title"
      : ["label", "kicker", "eyebrow", "category"].includes(name) ? "Example"
      : name === "lead" ? "The outcome is speed without surrendering auditability."
      : "Supporting detail",
  }));
}

export function ComponentPreview({ url, source, title, onReady }: { url: string; source: string; title: string; onReady: () => void }) {
  const controls = useMemo(() => componentControls(source), [source]);
  const [values, setValues] = useState<Record<string, string>>({});
  useEffect(() => { setValues(Object.fromEntries(controls.map((control) => [control.name, control.value]))); }, [url, source]);
  const preview = useMemo(() => {
    const parsed = new URL(url, window.location.href);
    Object.entries(values).forEach(([key, value]) => parsed.searchParams.set(`prop.${key}`, value));
    return `${parsed.pathname}${parsed.search}`;
  }, [url, values]);
  return <div class="component-preview">
    {controls.length > 0 && <div class="component-controls" aria-label="Component preview props">
      <span class="component-controls-label">Props</span>
      {controls.map((control) => <label class="component-control" key={control.name}>
        {control.boolean ? <>
          <input type="checkbox" checked={Boolean(values[control.name])} onChange={(event) => setValues({ ...values, [control.name]: event.currentTarget.checked ? "true" : "" })} />
          <span>{control.name}</span>
        </> : <>
          <span>{control.name}</span>
          <input value={values[control.name] ?? control.value} onInput={(event) => setValues({ ...values, [control.name]: event.currentTarget.value })} />
        </>}
      </label>)}
    </div>}
    <div class="component-preview-canvas">
      <iframe class="component-preview-frame" title={title} src={preview} onLoad={onReady} />
    </div>
  </div>;
}
