# Documentation Index

Each topic has one canonical owner. Other documents link to the owner instead of copying its contract.

| Topic | Document |
|---|---|
| architecture | `architecture.md` |
| opcode semantics/status | `instruction-set.md` |
| bytecode verification | `verification.md` |
| value representation | `value-representation.md` |
| memory/ownership | `memory-model.md` |
| JIT | `jit-internals.md` |
| threaded fusion | `fusion.md` |
| profiling/hotness | `profile.md` |
| optimization passes | `pass-system.md` |
| host integration | `host-integration.md` |
| platform support | `compatibility.md` |
| testing/validation | `testing.md` |
| structural review/simplification | `refactoring.md` |
| benchmarks | `benchmarks.md` |
| debugging | `debugging.md` |
| roadmap | `roadmap.md` |
| Go code design | `coding-patterns.md` |
| applied naming vocabulary | `symbol-naming-audit.md` |

## Guides

Guides define procedures over topic contracts:

- `guides/add-opcode.md`
- `guides/add-architecture.md`
- `guides/repl.md`

## Style

- H1 names the document subject; H2 uses unnumbered headings.
- Put one scope sentence directly below H1.
- Use `Ownership` for canonical owner maps.
- End topic and guide documents with `Related`.
- Label every fenced code block with its language.
- Keep one blank line between prose, lists, tables, and code blocks.

## Historical Records

`plans/` and `superpowers/` contain dated plans, audits, and design records. They preserve history and do not own current behavior.

`AGENTS.md` owns repository workflow. Topic docs own current behavior and contracts; roadmap owns priorities.
