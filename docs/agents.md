# Agents

Stamp needs no MCP server or separately installed skill. An agent works on the
same ordinary files as a person and calls the same CLI.

```sh
stamp skill
```

This prints the canonical project guide as Markdown. New and cloned projects
also contain that guide as `AGENTS.md`; `CLAUDE.md` is a symlink to it so Claude
Code discovers the same instructions automatically.

In Studio, the robot button at the top right of the sidebar copies a complete
session-opening prompt with the workspace's absolute path. Paste it into a
coding agent; the prompt tells the agent to run `stamp skill` before editing.

The safe loop is:

```text
stamp pull → edit files → inspect in Studio → ask for approval → stamp push
```

Never force a conflicted push without the person's explicit approval.
