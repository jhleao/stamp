import { describe, expect, it } from "vitest";
import { clampZoom } from "./preview-zoom";

describe("clampZoom", () => {
  it("keeps PDF zoom between 50 and 200 percent", () => {
    expect(clampZoom(20)).toBe(50);
    expect(clampZoom(130)).toBe(130);
    expect(clampZoom(240)).toBe(200);
  });
});
