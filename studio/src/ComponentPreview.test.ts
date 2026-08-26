import { describe, expect, it } from "vitest";
import { componentControls, componentPropNames } from "./ComponentPreview";

describe("componentPropNames", () => {
  it("discovers props through access and destructuring", () => {
    expect(componentPropNames(`
      const { eyebrow, title: heading, subtitle = "Fallback", ...rest } = props;
      return <div>{props.count}{props?.compact}{props["aria-label"]}</div>;
    `)).toEqual(["aria-label", "compact", "count", "eyebrow", "subtitle", "title"]);
  });
});

describe("componentControls", () => {
  it("finds unique component props and assigns useful preview defaults", () => {
    expect(componentControls(`const full = props.full; return <div>{props.no}{props.lead}{props.no}</div>`)).toEqual([
      { name: "full", value: "", boolean: true },
      { name: "lead", value: "The outcome is speed without surrendering auditability.", boolean: false },
      { name: "no", value: "01", boolean: false },
    ]);
  });
});
