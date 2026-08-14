# Agents

Agents can edit a Stamp workspace as ordinary files or use the bundled MCP
server for the complete collaboration loop. No separate Stamp skill is
required.

Prepare the current project once:

```sh
stamp agent setup
stamp studio
```

Setup keeps `AGENTS.md` as the single instruction source, creates the Claude
Code compatibility link `CLAUDE.md -> AGENTS.md`, and merges this project-scoped
configuration into `.mcp.json`:

```json
{
  "mcpServers": {
    "stamp": {
      "type": "http",
      "url": "http://127.0.0.1:57183/mcp"
    }
  }
}
```

Claude Code discovers `.mcp.json` when opened in the project and asks once for
approval. Studio serves both the UI and this project-bound MCP endpoint in one
process. Run `/mcp` in Claude Code to inspect the connection.

Clients that prefer to own the server process can still configure the portable
stdio command `stamp mcp serve`. That mode accepts an explicit project `dir` and
does not require Studio.

The server exposes twelve tools: list spaces and projects; create and preview a theme;
create, open, inspect, preview, pull, and push a project; return its Drive link;
and run `doctor`.
Tools return structured results and use the same implementation as the CLI.

A good agent loop is deliberately boring:

1. Call `project_pull` before editing an existing shared project.
2. Edit source or template files directly.
3. Call `project_preview` and inspect the generated artifacts.
4. Call `project_push` with a short human message.
5. If a lease conflict occurs, pull safely or show it to the person. Never
   invent a force lease.

Every new project contains `AGENTS.md` and its Claude Code link, so a
filesystem-capable coding agent gets the workflow and safety boundary simply
by opening the folder.

The MCP server does not contain an agent, prompts, chat history, or a second
document model. It only lets an agent operate the same small tool a person uses.
