# Stamp

Stamp is a small macOS utility for making document packs with people and agents.
It keeps a project in one revisioned Google Drive file, works on ordinary local
files, renders PDFs and XLSX files with established tools, and exposes the same
small workflow through a CLI, a local Studio, and MCP.

The whole product loop is:

```text
pull -> edit -> preview -> push
```

The previous TypeScript implementation is preserved next to this repository as
`../stamp-old`.

Start with [the whole workflow](docs/getting-started.md). See
[DESIGN.md](DESIGN.md) for the scope and the decisions we are holding ourselves
to, or [docs/agents.md](docs/agents.md) to connect an agent over MCP.

## Build

```sh
go build ./cmd/stamp
go test ./...
make smoke
```

Chrome, LibreOffice, and Pandoc provide the rendering wheels. Google Drive
setup is documented in [docs/drive.md](docs/drive.md).

Create a project and open the local workbench:

```sh
./stamp project create board-pack --name 'Board Pack'
cd board-pack
../stamp studio
```

See [docs/studio.md](docs/studio.md) for preview behavior and boundaries.
