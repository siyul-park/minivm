# Documentation Index

Each topic has one canonical owner. This document owns the topic-to-document map.

## Terminology

Requirements use RFC 2119 keywords (`MUST`, `MUST NOT`, `SHOULD`, `SHOULD NOT`, `MAY`). Bare imperatives are not requirements; move normative wording into the owner document.

| Topic | Document |
|---|---|
| architecture | `architecture.md` |
| opcode semantics/status | `instruction-set.md` |
| bytecode verification | `verification.md` |
| value representation | `value-representation.md` |
| memory/ownership | `memory-model.md` |
| JIT | `jit-internals.md` |
| JIT rebuild history | `jit-lessons.md` |
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

- `guides/add-opcode.md` — opcode change procedure
- `guides/add-architecture.md` — target-backend procedure
- `guides/repl.md` — REPL usage

Guides `MUST` own procedure, not topic contracts; contract text stays with its canonical owner.

## Style

Every topic and guide document `MUST`:

- use H1 for the document subject and unnumbered H2 headings;
- place one scope sentence directly below H1;
- use an `Ownership` section for canonical owner maps where ownership applies;
- end with a `Related` section;
- label every fenced code block with its language;
- keep one blank line between prose, lists, tables, and code blocks.

## Document Roles

- Topic docs: current behavior and contracts; no removed behavior or duplicate ownership.
- Guides: procedures over topic contracts.
- `roadmap.md`: priorities only.
- `plans/` and `superpowers/`: dated plans, audits, design records; never current authority.
- `AGENTS.md`: repository workflow.

## Related

- `AGENTS.md` — terminology, precedence, workflow
