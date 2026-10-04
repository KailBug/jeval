<p align="center"><img src="apps/desktop/resources/jeval.svg" width="72" height="72" alt="jeval logo"></p>

# jeval

[简体中文](README.zh-CN.md)

A local desktop workbench for inspecting agent runs. Explore timelines, tool output, and source evidence in one place.

**Early development preview.** Scan a Codex directory, browse the discovered records, and choose which to import—or select all. Scanning alone never adds records. Single-file classic/paginated JSONL import is also available. Search record content, filter event types, and inspect source evidence locally without an API key. Imports are held in memory; persistence, automatic sync, and Jev analysis are still planned. Directory selection and search are tested with synthetic samples; one Codex 0.160.0 recording was previously validated.

![jeval desktop preview](docs/design/m0-desktop.png)

## Run locally

Requires Node.js 22.12+ and Go 1.24+. Built with Electron, React, TypeScript, and Go; currently tested on Windows x64.

```sh
npm ci
npm run dev
```

`npm run check` runs type checks, engine/integration tests, and a production build. `npm run pack` creates a desktop directory package.

[Development guide (Chinese)](docs/development/getting-started.md) · [Roadmap (Chinese)](docs/jeval-development-plan.md) · [Documentation (Chinese)](docs/README.md)
