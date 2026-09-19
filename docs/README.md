# Documentation Index

Each topic has one canonical owner. This document owns the topic-to-document map.

## Terminology

Every doc in this directory `MUST` use RFC 2119 keywords (`MUST`, `MUST NOT`, `SHOULD`, `SHOULD NOT`, `MAY`) for agent requirements and `MUST NOT` use bare imperatives (`keep`, `do not`, `prefer`, `skip`) where a keyword applies. Sentences without a keyword are informative and `MUST NOT` be treated as requirements.

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

A guide `MUST` describe change order and validation only. The contract it operates on `MUST` stay in its owner topic doc; a guide `MUST NOT` duplicate that contract and `MUST` link to the owner instead.

## Style

Every topic and guide document `MUST`:

- use H1 for the document subject and unnumbered H2 headings;
- place one scope sentence directly below H1;
- use an `Ownership` section for canonical owner maps where ownership applies;
- end with a `Related` section;
- label every fenced code block with its language;
- keep one blank line between prose, lists, tables, and code blocks.

## Document Roles

- A canonical topic doc `MUST` describe supported/current behavior. It `MUST NOT` describe removed behavior as current and `MUST NOT` duplicate a contract owned elsewhere.
- `roadmap.md` `MUST` own priorities only; it `MUST NOT` override topic contracts.
- `plans/` and `superpowers/` contain dated plans, audits, and design records. They `MAY` preserve history and future work, but they `MUST NOT` own current behavior.
- `AGENTS.md` owns repository workflow. Topic docs own current behavior and contracts; roadmap owns priorities.

## Related

- `AGENTS.md` — terminology, precedence, workflow
