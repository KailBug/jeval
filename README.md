<p align="center"><img src="apps/desktop/resources/jeval.svg" width="72" height="72" alt="jeval logo"></p>

# jeval

[简体中文](README.zh-CN.md)

A local desktop workbench for inspecting agent runs. Explore timelines, tool output, and source evidence in one place.

**Early development preview.** Scan a Codex directory, browse the discovered records, and choose which to import—or select all. Scanning alone never adds records. Single-file classic/paginated JSONL import is also available. Confirmed snapshots and directory settings persist locally in SQLite; browse saved previews offline, update a registered record manually, and page through tasks and events without an API key. Export a saved Codex snapshot as native JSON or Markdown, and import JSON into another library while preserving evidence. Content remains an 8 KiB preview per event plus any truncation notice; exports include source paths and are not automatically redacted. Full content, automatic sync, and Jev analysis remain planned. Checks use synthetic samples; one Codex 0.160.0 recording was previously validated.

![jeval desktop preview](docs/design/m0-desktop.png)

## Run locally

Requires Node.js 22.12+ and Go 1.26+. Built with Electron, React, TypeScript, and Go; currently tested on Windows x64.

```sh
npm ci
npm run dev
```

`npm run check` runs type checks, engine/integration tests, and a production build. `npm run pack` creates a desktop directory package.

[Development guide (Chinese)](docs/development/getting-started.md) · [Roadmap (Chinese)](docs/jeval-development-plan.md) · [Documentation (Chinese)](docs/README.md)
