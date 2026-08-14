export function agentPrompt(workspacePath: string): string {
  return `Let's start a work session on the Stamp workspace at ${workspacePath}.
Please run "stamp skill" to learn how to work with Stamp.
Let me know when you're ready to begin.`;
}
