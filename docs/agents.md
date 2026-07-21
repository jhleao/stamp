# Agents

Agents can edit a Stamp workspace as ordinary files or use the bundled MCP
server for the complete collaboration loop.

Build and note the absolute binary path:

```sh
make build
pwd
```

Configure an MCP client to run:

```json
{
  "mcpServers": {
    "stamp": {
      "command": "/absolute/path/to/stamp/bin/stamp",
      "args": ["mcp", "serve"]
    }
  }
}
```

The server exposes ten tools: list spaces and projects; create, open, inspect,
preview, pull, and push a project; return its Drive link; and run `doctor`.
Tools return structured results and use the same implementation as the CLI.

A good agent loop is deliberately boring:

1. Call `project_pull` before editing an existing shared project.
2. Edit source or template files directly.
3. Call `project_preview` and inspect the generated artifacts.
4. Call `project_push` with a short human message.
5. If a lease conflict occurs, pull safely or show it to the person. Never
   invent a force lease.

The MCP server does not contain an agent, prompts, chat history, or a second
document model. It only lets an agent operate the same small tool a person uses.
