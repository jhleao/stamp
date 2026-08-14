import { describe, expect, it } from "vitest";
import { syncActions } from "./sync-policy";

describe("syncActions", () => {
  it.each([
    ["up-to-date", false, false],
    ["local-ahead", false, true],
    ["remote-ahead", true, false],
    ["diverged", true, true],
    ["unavailable", false, false],
  ] as const)("maps %s to remote-aware actions", (state, canPull, canPush) => {
    expect(syncActions(state)).toEqual({ canPull, canPush });
  });
});
