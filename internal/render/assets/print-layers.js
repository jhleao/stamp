(() => {
  // Paged.js caps print boxes to the viewport until a print job supplies its paper size.
  for (const page of document.querySelectorAll(".pagedjs_page")) {
    const size = getComputedStyle(page);
    for (const box of [page, page.querySelector(".pagedjs_sheet")]) {
      if (!box) continue;
      box.style.setProperty("max-width", "none", "important");
      box.style.setProperty("max-height", "none", "important");
      box.style.setProperty(
        "width",
        size.getPropertyValue("--pagedjs-width"),
        "important",
      );
      box.style.setProperty(
        "height",
        size.getPropertyValue("--pagedjs-height"),
        "important",
      );
    }
  }
  const paintAttribute = "data-stamp-print-paint";
  const textAttribute = "data-stamp-print-text";
  const roots = [...document.querySelectorAll(".pagedjs_sheet")];
  if (!roots.length) {
    const slides = [...document.querySelectorAll(".stamp-deck > slide")];
    roots.push(...(slides.length ? slides : [document.body]));
  }

  function hasEffect(style) {
    return (
      style.boxShadow !== "none" ||
      style.filter !== "none" ||
      (style.backdropFilter && style.backdropFilter !== "none") ||
      (style.maskImage && style.maskImage !== "none") ||
      style.clipPath !== "none" ||
      Number(style.opacity) < 1 ||
      style.mixBlendMode !== "normal" ||
      /gradient\(/.test(style.backgroundImage)
    );
  }

  function requiresRasterText(style) {
    return (
      style.filter !== "none" ||
      (style.maskImage && style.maskImage !== "none") ||
      style.clipPath !== "none" ||
      Number(style.opacity) < 1 ||
      style.mixBlendMode !== "normal" ||
      style.textShadow !== "none" ||
      style.backgroundClip === "text" ||
      style.webkitBackgroundClip === "text"
    );
  }

  const pages = roots.filter((root) => {
    const nodes = [root, ...root.querySelectorAll("*")];
    return (
      root.querySelector(
        "svg mask, svg filter, svg pattern, svg linearGradient, svg radialGradient",
      ) ||
      nodes.some((node) =>
        [null, "::before", "::after"].some((pseudo) => {
          const style = getComputedStyle(node, pseudo);
          return (
            node.getClientRects().length > 0 &&
            style.display !== "none" &&
            style.visibility !== "hidden" &&
            Number(style.opacity) > 0 &&
            (!pseudo ||
              (style.content !== "none" && style.content !== "normal")) &&
            hasEffect(style)
          );
        }),
      )
    );
  });

  for (const root of pages) {
    root.setAttribute("data-stamp-print-page", "");
    const unsafe = new Set();
    for (const node of [root, ...root.querySelectorAll("*")]) {
      const style = getComputedStyle(node);
      node.setAttribute(paintAttribute, "");
      const raster =
        unsafe.has(node.parentElement) ||
        requiresRasterText(style) ||
        node.namespaceURI !== "http://www.w3.org/1999/xhtml";
      if (raster) unsafe.add(node);
      node.style.setProperty("--stamp-print-fill", style.webkitTextFillColor);
      node.style.setProperty(
        "--stamp-print-decoration",
        style.textDecorationColor,
      );
      if (!raster) node.setAttribute(textAttribute, "");
      for (const name of ["before", "after"]) {
        const pseudo = getComputedStyle(node, "::" + name);
        node.style.setProperty(
          "--stamp-print-" + name + "-fill",
          pseudo.webkitTextFillColor,
        );
        if (
          !raster &&
          !requiresRasterText(pseudo) &&
          !pseudo.content.includes("url(")
        )
          node.setAttribute("data-stamp-print-" + name, "");
      }
    }
  }

  const style = document.createElement("style");
  style.setAttribute("data-pagedjs-ignore", "");
  style.textContent = `
    [${paintAttribute}] { -webkit-text-fill-color: var(--stamp-print-fill) !important; }
    [${paintAttribute}]::before { -webkit-text-fill-color: var(--stamp-print-before-fill) !important; }
    [${paintAttribute}]::after { -webkit-text-fill-color: var(--stamp-print-after-fill) !important; }
    [${textAttribute}] { -webkit-text-fill-color: transparent !important; text-decoration-color: transparent !important; }
    [data-stamp-print-before]::before, [data-stamp-print-after]::after { -webkit-text-fill-color: transparent !important; }
  `;
  document.head.appendChild(style);

  window.__stampPrintLayers = {
    bounds: pages.map((root) => {
      const box = root.getBoundingClientRect();
      return {
        x: box.x + scrollX,
        y: box.y + scrollY,
        width: box.width,
        height: box.height,
      };
    }),
    finish(images) {
      if (images.length !== pages.length)
        throw new Error("Missing print background.");
      style.textContent = `
        [${paintAttribute}], [${paintAttribute}]::before, [${paintAttribute}]::after {
          background: none !important; border-color: transparent !important;
          outline-color: transparent !important; box-shadow: none !important;
          filter: none !important; backdrop-filter: none !important; mask: none !important;
          clip-path: none !important; opacity: 1 !important; mix-blend-mode: normal !important;
          text-shadow: none !important; -webkit-box-reflect: none !important;
          -webkit-text-fill-color: transparent !important; text-decoration-color: transparent !important;
        }
        [${textAttribute}] {
          -webkit-text-fill-color: var(--stamp-print-fill) !important;
          text-decoration-color: var(--stamp-print-decoration) !important;
        }
        [data-stamp-print-before]::before { -webkit-text-fill-color: var(--stamp-print-before-fill) !important; }
        [data-stamp-print-after]::after { -webkit-text-fill-color: var(--stamp-print-after-fill) !important; }
        img[${paintAttribute}], svg[${paintAttribute}], canvas[${paintAttribute}],
        video[${paintAttribute}], iframe[${paintAttribute}], object[${paintAttribute}], embed[${paintAttribute}] {
          visibility: hidden !important;
        }
        [data-stamp-print-page] {
          position: relative !important; isolation: isolate !important;
        }
      `;
      pages.forEach((root, index) => {
        const image = document.createElement("img");
        image.src = "data:image/png;base64," + images[index];
        image.alt = "";
        image.setAttribute("aria-hidden", "true");
        image.setAttribute("data-stamp-print-background", "");
        image.style.cssText =
          "position:absolute;inset:0;width:100%;height:100%;max-width:none;z-index:-2147483647;pointer-events:none;";
        root.prepend(image);
      });
    },
  };
  return window.__stampPrintLayers.bounds;
})();
