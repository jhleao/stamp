import { useEffect, useRef, useState } from "preact/hooks";
import { GlobalWorkerOptions, getDocument, type PDFDocumentProxy, type PDFPageProxy } from "pdfjs-dist";
import pdfWorkerURL from "pdfjs-dist/build/pdf.worker.min.mjs?url";
import { clampZoom } from "./preview-zoom";

GlobalWorkerOptions.workerSrc = pdfWorkerURL;

interface Props {
  url: string;
  filename: string;
  onReady: () => void;
}

interface PageCanvasProps {
  page: PDFPageProxy;
  availableWidth: number;
  zoom: number;
}

function PageCanvas({ page, availableWidth, zoom }: PageCanvasProps) {
  const canvas = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const target = canvas.current;
    if (!target || availableWidth <= 0) return;
    const natural = page.getViewport({ scale: 1 });
    const cssWidth = Math.min(availableWidth, natural.width) * (zoom / 100);
    const pixelRatio = Math.min(window.devicePixelRatio || 1, 2);
    const viewport = page.getViewport({ scale: (cssWidth / natural.width) * pixelRatio });
    target.width = Math.ceil(viewport.width);
    target.height = Math.ceil(viewport.height);
    target.style.width = `${cssWidth}px`;
    target.style.height = `${cssWidth * (natural.height / natural.width)}px`;
    const context = target.getContext("2d", { alpha: false });
    if (!context) return;
    const task = page.render({ canvas: target, canvasContext: context, viewport });
    return () => { task.cancel(); };
  }, [page, availableWidth, zoom]);

  return <article class="pdf-sheet"><canvas ref={canvas} /></article>;
}

function DownloadIcon() {
  return <svg viewBox="0 0 16 16" aria-hidden="true">
    <path d="M8 2v8m0 0 3-3m-3 3L5 7M3 13h10" />
  </svg>;
}

export function PdfPreview({ url, filename, onReady }: Props) {
  const container = useRef<HTMLDivElement>(null);
  const [document, setDocument] = useState<PDFDocumentProxy | null>(null);
  const [pages, setPages] = useState<PDFPageProxy[]>([]);
  const [availableWidth, setAvailableWidth] = useState(0);
  const [zoom, setZoom] = useState(100);
  const [error, setError] = useState("");

  useEffect(() => {
    const node = container.current;
    if (!node) return;
    const update = () => setAvailableWidth(Math.max(0, node.clientWidth - 32));
    update();
    const observer = new ResizeObserver(update);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    let cancelled = false;
    setDocument(null);
    setPages([]);
    setError("");
    const task = getDocument({ url });
    task.promise.then(async (loaded) => {
      const loadedPages = await Promise.all(Array.from({ length: loaded.numPages }, (_, index) => loaded.getPage(index + 1)));
      if (cancelled) return;
      setDocument(loaded);
      setPages(loadedPages);
      onReady();
    }).catch((cause) => {
      if (cancelled) return;
      setError(cause instanceof Error ? cause.message : "Preview could not be rendered.");
      onReady();
    });
    return () => {
      cancelled = true;
      task.destroy();
    };
  }, [url]);

  return <div class="pdf-preview-container">
    <div ref={container} class="pdf-preview" aria-busy={!document && !error}>
      <div class="pdf-zoom" role="group" aria-label="PDF zoom">
        <button aria-label="Zoom out" title="Zoom out" disabled={zoom <= 50} onClick={() => setZoom(clampZoom(zoom - 10))}>−</button>
        <button class="pdf-zoom-value" aria-label="Reset zoom" title="Fit preview" disabled={zoom === 100} onClick={() => setZoom(100)}>{zoom}%</button>
        <button aria-label="Zoom in" title="Zoom in" disabled={zoom >= 200} onClick={() => setZoom(clampZoom(zoom + 10))}>+</button>
      </div>
      {error ? <div class="empty-pane">{error}</div> : pages.map((page) =>
        <PageCanvas key={page.pageNumber} page={page} availableWidth={availableWidth} zoom={zoom} />)}
    </div>
    {document && !error && <a class="pdf-download" href={url} download={filename} aria-label={`Download ${filename}`} title={`Download ${filename}`}>
      <DownloadIcon />
    </a>}
  </div>;
}
