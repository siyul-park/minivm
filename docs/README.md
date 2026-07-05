# Documentation Index

This directory uses a single-owner model. Each topic owns one area of behavior; other documents should link to that topic instead of repeating the same explanation.

## Reading Guide

| Need | Read |
|---|---|
| Package boundaries and execution flow | `architecture.md` |
| Opcode semantics and stack effects | `instruction-set.md` |
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

## Document Ownership

- Put detailed opcode behavior in `instruction-set.md`.
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
