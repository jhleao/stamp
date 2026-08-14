import { useEffect, useRef } from "preact/hooks";
import * as monaco from "monaco-editor/esm/vs/editor/editor.api";
import "monaco-editor/esm/vs/language/css/monaco.contribution";
import "monaco-editor/esm/vs/language/html/monaco.contribution";
import "monaco-editor/esm/vs/language/json/monaco.contribution";
import "monaco-editor/esm/vs/language/typescript/monaco.contribution";
import "monaco-editor/esm/vs/basic-languages/markdown/markdown.contribution";
import "monaco-editor/esm/vs/basic-languages/typescript/typescript.contribution";
import EditorWorker from "monaco-editor/esm/vs/editor/editor.worker?worker";
import CssWorker from "monaco-editor/esm/vs/language/css/css.worker?worker";
import HtmlWorker from "monaco-editor/esm/vs/language/html/html.worker?worker";
import JsonWorker from "monaco-editor/esm/vs/language/json/json.worker?worker";
import TypeScriptWorker from "monaco-editor/esm/vs/language/typescript/ts.worker?worker";
import "monaco-editor/min/vs/editor/editor.main.css";
import { languageForPath, tailwindToken } from "./editor-support";
import { registerStampLanguage } from "./stamp-language";

declare global {
  interface Window { MonacoEnvironment?: monaco.Environment }
}

self.MonacoEnvironment = {
  getWorker(_moduleId, label) {
    if (label === "json") return new JsonWorker();
    if (label === "typescript" || label === "javascript") return new TypeScriptWorker();
    if (label === "css" || label === "scss" || label === "less") return new CssWorker();
    if (label === "html" || label === "handlebars" || label === "razor") return new HtmlWorker();
    return new EditorWorker();
  },
};

const tailwindUtilities = [
  "block", "inline", "inline-block", "hidden", "flex", "inline-flex", "grid", "contents",
  "relative", "absolute", "fixed", "sticky", "inset-0", "top-0", "right-0", "bottom-0", "left-0",
  "flex-row", "flex-col", "flex-wrap", "items-start", "items-center", "items-end", "items-stretch",
  "justify-start", "justify-center", "justify-between", "justify-end", "self-start", "self-center", "self-end",
  "grid-cols-1", "grid-cols-2", "grid-cols-3", "grid-cols-4", "col-span-2", "col-span-3",
  "gap-1", "gap-2", "gap-3", "gap-4", "gap-6", "gap-8", "gap-12",
  "m-0", "mx-auto", "mt-1", "mt-2", "mt-4", "mt-6", "mt-8", "mb-2", "mb-4", "mb-6", "mb-8",
  "p-0", "p-2", "p-3", "p-4", "p-6", "p-8", "px-2", "px-3", "px-4", "px-6", "px-8",
  "py-1", "py-2", "py-3", "py-4", "py-6", "py-8", "space-y-2", "space-y-4", "space-y-6",
  "w-full", "w-auto", "h-full", "h-auto", "min-h-screen", "max-w-sm", "max-w-md", "max-w-lg", "max-w-xl", "max-w-2xl", "max-w-4xl", "max-w-6xl",
  "overflow-hidden", "overflow-auto", "object-cover", "object-contain", "aspect-video", "aspect-square",
  "text-left", "text-center", "text-right", "text-xs", "text-sm", "text-base", "text-lg", "text-xl", "text-2xl", "text-3xl", "text-4xl", "text-5xl",
  "font-normal", "font-medium", "font-semibold", "font-bold", "italic", "uppercase", "tracking-tight", "tracking-wide",
  "leading-none", "leading-tight", "leading-snug", "leading-normal", "leading-relaxed", "truncate", "whitespace-nowrap",
  "text-black", "text-white", "text-neutral-500", "text-neutral-700", "text-neutral-900",
  "bg-white", "bg-black", "bg-transparent", "bg-neutral-50", "bg-neutral-100", "bg-neutral-900",
  "border", "border-0", "border-neutral-200", "border-neutral-800", "rounded", "rounded-md", "rounded-lg", "rounded-full",
  "shadow-sm", "shadow-md", "opacity-50", "opacity-75", "print:hidden", "break-before-page", "break-after-page", "break-inside-avoid",
  "hover:opacity-75", "sm:grid-cols-2", "md:grid-cols-2", "lg:grid-cols-3",
];
let projectUtilities: string[] = [];

let configured = false;
function configureMonaco() {
  if (configured) return;
  configured = true;
  registerStampLanguage(monaco);
  monaco.languages.typescript.typescriptDefaults.setCompilerOptions({
    allowNonTsExtensions: true,
    jsx: monaco.languages.typescript.JsxEmit.React,
    jsxFactory: "h",
    jsxFragmentFactory: "Fragment",
    target: monaco.languages.typescript.ScriptTarget.ES2020,
  });
  monaco.languages.typescript.typescriptDefaults.setDiagnosticsOptions({
    noSemanticValidation: true,
    noSyntaxValidation: false,
  });
  monaco.editor.defineTheme("stamp-light", {
    base: "vs", inherit: true,
    rules: [
      { token: "comment", foreground: "83909d", fontStyle: "italic" },
      { token: "keyword", foreground: "b64f12" },
      { token: "keyword.ts", foreground: "b64f12" },
      { token: "string", foreground: "3d6f92" },
      { token: "number", foreground: "a35a2c" },
      { token: "type.identifier", foreground: "536f8e" },
      { token: "tag", foreground: "b64f12" },
      { token: "attribute.name", foreground: "536f8e" },
      { token: "tag.component.stamp", foreground: "b64f12", fontStyle: "bold" },
      { token: "tag.html.stamp", foreground: "8a5a3b" },
      { token: "delimiter.tag.stamp", foreground: "9aa3ac" },
      { token: "attribute.name.stamp", foreground: "536f8e" },
      { token: "string.attribute.stamp", foreground: "3d6f92" },
      { token: "string.tailwind.stamp", foreground: "277096" },
      { token: "expression.stamp", foreground: "805b9b" },
      { token: "markup.heading.stamp", foreground: "30343a", fontStyle: "bold" },
      { token: "markup.heading.marker.stamp", foreground: "b64f12" },
      { token: "markup.bold.stamp", foreground: "30343a", fontStyle: "bold" },
      { token: "markup.italic.stamp", foreground: "4c5965", fontStyle: "italic" },
      { token: "keyword.list.stamp", foreground: "b64f12" },
      { token: "key.frontmatter.stamp", foreground: "536f8e" },
      { token: "string.code.inline.stamp", foreground: "8a5a3b" },
    ],
    colors: { "editor.background": "#f9fafb", "editor.foreground": "#30343a", "editorLineNumber.foreground": "#b1b8c0", "editorLineNumber.activeForeground": "#66717d", "editor.lineHighlightBackground": "#f1f4f5", "editor.selectionBackground": "#e8ded7", "editorCursor.foreground": "#c45516", "editorIndentGuide.background1": "#e5e9ec" },
  });
  monaco.editor.defineTheme("stamp-dark", {
    base: "vs-dark", inherit: true,
    rules: [
      { token: "comment", foreground: "707982", fontStyle: "italic" },
      { token: "keyword", foreground: "e66a22" },
      { token: "keyword.ts", foreground: "e66a22" },
      { token: "string", foreground: "8eb7d1" },
      { token: "number", foreground: "d28b5b" },
      { token: "type.identifier", foreground: "9ab0cb" },
      { token: "tag", foreground: "e66a22" },
      { token: "attribute.name", foreground: "9ab0cb" },
      { token: "tag.component.stamp", foreground: "e66a22", fontStyle: "bold" },
      { token: "tag.html.stamp", foreground: "c78a68" },
      { token: "delimiter.tag.stamp", foreground: "67717b" },
      { token: "attribute.name.stamp", foreground: "9ab0cb" },
      { token: "string.attribute.stamp", foreground: "8eb7d1" },
      { token: "string.tailwind.stamp", foreground: "7eb6d5" },
      { token: "expression.stamp", foreground: "c5a5d9" },
      { token: "markup.heading.stamp", foreground: "e8ebee", fontStyle: "bold" },
      { token: "markup.heading.marker.stamp", foreground: "e66a22" },
      { token: "markup.bold.stamp", foreground: "e8ebee", fontStyle: "bold" },
      { token: "markup.italic.stamp", foreground: "b3bbc3", fontStyle: "italic" },
      { token: "keyword.list.stamp", foreground: "e66a22" },
      { token: "key.frontmatter.stamp", foreground: "9ab0cb" },
      { token: "string.code.inline.stamp", foreground: "d39a79" },
    ],
    colors: { "editor.background": "#222529", "editor.foreground": "#d9dde1", "editorLineNumber.foreground": "#4f565e", "editorLineNumber.activeForeground": "#9ba4ad", "editor.lineHighlightBackground": "#292d31", "editor.selectionBackground": "#49372e", "editorCursor.foreground": "#e66a22", "editorIndentGuide.background1": "#34393f" },
  });
  const tailwindCompletions: monaco.languages.CompletionItemProvider = {
    triggerCharacters: ["\"", "'", " ", ":", "-"],
    provideCompletionItems(model, position) {
      const match = tailwindToken(model.getLineContent(position.lineNumber), position.column);
      if (!match) return { suggestions: [] };
      const range = new monaco.Range(position.lineNumber, match.startColumn, position.lineNumber, position.column);
      return { suggestions: [...new Set([...projectUtilities, ...tailwindUtilities])].map((utility) => ({
        label: utility, insertText: utility, detail: "Tailwind utility", kind: monaco.languages.CompletionItemKind.Value, range,
      })) };
    },
  };
  for (const language of ["html", "typescript", "stamp"]) {
    monaco.languages.registerCompletionItemProvider(language, tailwindCompletions);
  }
}

export interface MonacoEditorHandle { format: () => Promise<void>; focus: () => void }

export function MonacoEditor({ value, path, appearance, utilities = [], onChange, onReady, onFocusChange }: {
  value: string;
  path: string;
  appearance: "light" | "dark";
  onChange: (value: string) => void;
  onReady?: (handle: MonacoEditorHandle | null) => void;
  onFocusChange?: (focused: boolean) => void;
  utilities?: string[];
}) {
  const host = useRef<HTMLDivElement>(null);
  const editor = useRef<monaco.editor.IStandaloneCodeEditor>();
  const applying = useRef(false);

  useEffect(() => { projectUtilities = utilities; }, [utilities]);

  useEffect(() => {
    configureMonaco();
    const uri = monaco.Uri.parse(`file:///stamp/${path.replace(/^\/+/, "")}`);
    const existing = monaco.editor.getModel(uri);
    existing?.dispose();
    const model = monaco.editor.createModel(value, languageForPath(path), uri);
    const instance = monaco.editor.create(host.current!, {
      model,
      theme: appearance === "dark" ? "stamp-dark" : "stamp-light",
      automaticLayout: true,
      minimap: { enabled: false },
      overviewRulerLanes: 0,
      hideCursorInOverviewRuler: true,
      scrollBeyondLastLine: false,
      wordWrap: "on",
      lineNumbersMinChars: 3,
      folding: true,
      glyphMargin: false,
      renderLineHighlight: "line",
      renderWhitespace: "selection",
      fontFamily: '"Geist Mono", monospace',
      fontSize: 12,
      lineHeight: 20,
      tabSize: 2,
      formatOnPaste: true,
      formatOnType: true,
      padding: { top: 14, bottom: 18 },
      scrollbar: { verticalScrollbarSize: 8, horizontalScrollbarSize: 8 },
    });
    editor.current = instance;
    const subscription = instance.onDidChangeModelContent(() => {
      if (!applying.current) onChange(instance.getValue());
    });
    const focusSubscription = instance.onDidFocusEditorText(() => onFocusChange?.(true));
    const blurSubscription = instance.onDidBlurEditorText(() => onFocusChange?.(false));
    onReady?.({ format: async () => { await instance.getAction("editor.action.formatDocument")?.run(); instance.focus(); }, focus: () => instance.focus() });
    return () => {
      onReady?.(null);
      subscription.dispose();
      focusSubscription.dispose();
      blurSubscription.dispose();
      instance.dispose();
      model.dispose();
      editor.current = undefined;
    };
  }, [path]);

  useEffect(() => {
    const instance = editor.current;
    if (!instance || instance.getValue() === value) return;
    applying.current = true;
    instance.setValue(value);
    applying.current = false;
  }, [value]);

  useEffect(() => { monaco.editor.setTheme(appearance === "dark" ? "stamp-dark" : "stamp-light"); }, [appearance]);

  return <div ref={host} class="monaco-host" aria-label="Source editor" />;
}
