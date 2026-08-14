import { describe, expect, it } from "vitest";
import { agentPrompt } from "./agent-prompt";

describe("agentPrompt", () => {
  it("introduces Stamp and includes the exact workspace path", () => {
    expect(agentPrompt("/Users/example/My project")).toBe(`Let's start a work session on the Stamp workspace at /Users/example/My project.
Please run "stamp skill" to learn how to work with Stamp.
Let me know when you're ready to begin.`);
  });
});
