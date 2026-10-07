# Public, sanitized fork corpus

These two files are unchanged copies of the `archived-fork-33ce-3869` corpus from [steipete/CodexBar](https://github.com/steipete/CodexBar/tree/eda352c17d41d8bbb55b6b4b1c5cf2a5588bc536/Tests/CodexBarTests/Fixtures/CostUsage/Issue2037/archived-fork-33ce-3869/codex-home/archived_sessions), pinned to commit `eda352c17d41d8bbb55b6b4b1c5cf2a5588bc536`, retrieved on 2026-10-07 through the GitHub contents API.

The upstream [corpus findings](https://github.com/steipete/CodexBar/blob/eda352c17d41d8bbb55b6b4b1c5cf2a5588bc536/docs/issue-2037-p0-local-corpus-findings.md) identify this pair as sanitized derivatives of a real local parent/child fork, retaining usage and metadata only. They are **not complete original trajectories**: identifiers and timestamps are aliases, the 2030 dates are synthetic, and CLI version/history mode are absent. The adapter defaults missing history mode to classic; that does not establish the original runtime's mode or version. These files support a narrow lineage-field and missing-content check, not message replay, token accounting, or full fork compatibility.

| File         | Bytes | SHA-256                                                          |
| ------------ | ----: | ---------------------------------------------------------------- |
| parent.jsonl | 52918 | 98b4bd22629d05211b284cdedb7e5e58c4b6ad615c12723dcc9ecfe5da54bbcf |
| child.jsonl  | 62030 | 56aed95f5a5a25734da03f2068164e652b0749d9a7ce6ac5d998ca93975ea5e9 |

The source repository uses MIT; its unchanged [LICENSE](LICENSE) is included (Copyright 2026 Peter Steinberger). Its SHA-256 is `14293556b79940745123d0160c71d27ed0e9fe9b8a848093f3ed78f4853caafe`. Git attributes preserve the original bytes. No private jeval-user data is included.

Expected results are zero display events and one no-display-content warning per file; the child's `forked_from_id` must match the parent's source session ID. This expected absence is a compatibility limitation, not a full-trajectory success claim. Verification and the broader local corpus are documented in the [source matrix](../../../../../docs/adapters/codex-source-matrix.md).
