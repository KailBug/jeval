<p align="center"><img src="apps/desktop/resources/jeval.svg" width="72" height="72" alt="jeval logo"></p>

# jeval

[简体中文](README.zh-CN.md)

A local desktop workbench for inspecting agent runs. Explore timelines, tool output, and source evidence in one place.

**Early development preview.** Browse demo runs or explicitly import a classic Codex rollout JSONL file. Inspect messages, tool results, and source lines locally, without an API key. Imports are held in memory; automatic sync, persistence, and Jev analysis are still planned. Codex release compatibility is not yet verified.

![jeval desktop preview](docs/design/m0-desktop.png)

## Run locally

Requires Node.js 22.12+ and Go 1.24+. Built with Electron, React, TypeScript, and Go; currently tested on Windows x64.

```sh
npm ci
npm run dev
```

`npm run check` runs type checks, engine/integration tests, and a production build. `npm run pack` creates a desktop directory package.

[Development guide (Chinese)](docs/development/getting-started.md) · [Roadmap (Chinese)](docs/jeval-development-plan.md) · [Documentation (Chinese)](docs/README.md)
