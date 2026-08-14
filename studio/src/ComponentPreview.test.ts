import { describe, expect, it } from "vitest";
import { componentControls } from "./ComponentPreview";

describe("componentControls", () => {
  it("finds unique component props and assigns useful preview defaults", () => {
    expect(componentControls(`const full = props.full; return <div>{props.no}{props.lead}{props.no}</div>`)).toEqual([
      { name: "full", value: "", boolean: true },
      { name: "lead", value: "The outcome is speed without surrendering auditability.", boolean: false },
      { name: "no", value: "01", boolean: false },
    ]);
  });
});
