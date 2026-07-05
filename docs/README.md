# Documentation Index

This directory uses a single-owner model. Each topic owns one area of behavior; other documents should link to that topic instead of repeating the same explanation.

## Reading Guide

| Need | Read |
|---|---|
| Package boundaries and execution flow | `architecture.md` |
| Opcode semantics, stack effects, and per-backend JIT status | `instruction-set.md` |
| Static bytecode validation | `verification.md` |
| Value layout, kinds, boxed values, dynamic `ref` | `value-representation.md` |
| Heap ownership, reference counting, GC | `memory-model.md` |
| Trace JIT internals | `jit-internals.md` |
| Profiling, ticks, JIT counters | `profile.md` |
| Pass manager, analyses, optimizer levels | `pass-system.md` |
| Host functions and marshaling | `host-integration.md` |
| Platform and backend support | `compatibility.md` |
| Benchmark results and methodology | `benchmarks.md` |
| Debugger API | `debugging.md` |
| Current priorities | `roadmap.md` |
| Code style | `coding-patterns.md` |
| Adding an opcode | `guides/add-opcode.md` |
| Adding a JIT backend | `guides/add-architecture.md` |
| REPL usage | `guides/repl.md` |

## README Cross-Reference

Top-level READMEs stay as summaries. Each README section should point to the full document that owns the details.

| README section | Owning documents |
|---|---|
| Overview and install requirements | `architecture.md`, `compatibility.md` |
| Why minivm / use cases | `architecture.md`, `host-integration.md`, `memory-model.md`, `jit-internals.md` |
| Performance | `benchmarks.md`, `jit-internals.md`, `value-representation.md` |
| Usage: execute bytecode | `instruction-set.md`, `verification.md` |
| Usage: host calls | `host-integration.md`, `memory-model.md`, `value-representation.md` |
| Usage: functions and calls | `instruction-set.md`, `architecture.md` |
| Usage: verification | `verification.md` |
| Usage: optimization | `pass-system.md` |
| JIT overview | `jit-internals.md`, `profile.md`, `compatibility.md` |
| Instruction set | `instruction-set.md` |
| Options, profiling, debugging | `memory-model.md`, `profile.md`, `debugging.md` |
| Status and roadmap | `compatibility.md`, `roadmap.md` |

If a README paragraph needs more than a summary, move the detail into the owning document and link back to it.

## Document Ownership

- Put detailed opcode behavior and per-backend JIT status in `instruction-set.md`.
- Put heap ownership and RC rules in `memory-model.md`.
- Put boxed value layout and kind rules in `value-representation.md`.
- Put JIT implementation contracts in `jit-internals.md`.
- Put benchmark numbers in `benchmarks.md`.
- Put host conversion details in `host-integration.md`.
- Put platform support in `compatibility.md`.

Other documents should provide a short summary and link to the owning document.

## Standard Shape

Long-lived topic docs should generally use:

1. title and one-line purpose
2. `When to Read`
3. `Source of Truth` when code paths matter
4. topic-specific reference content
5. `Maintenance Notes`
6. `Related Docs`

Guides may use task-oriented steps instead.

## Style Rules

- Use standard technical terms over project-specific slang.
- Keep wording direct and general.
- Prefer short paragraphs and tables for reference material.
- Avoid repeating the same explanation across documents.
- Keep examples current with code.
- Use `minivm` consistently for the project name.
