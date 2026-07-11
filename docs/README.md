# Documentation Index

This directory uses a single-owner model. Each topic owns one area of behavior; other documents should link here when readers need more detail instead of repeating the same explanation.

## Reading Guide

| Need | Read |
|---|---|
| Package boundaries and execution flow | `architecture.md` |
| Opcode semantics, stack effects, and JIT status | `instruction-set.md` |
| Static bytecode validation | `verification.md` |
| Value layout and boxed values | `value-representation.md` |
| Heap ownership, reference counting, GC | `memory-model.md` |
| Trace JIT internals | `jit-internals.md` |
| Threaded and ARM64 opcode fusion | `fusion.md` |
| Profiling and JIT counters | `profile.md` |
| Pass manager and optimizer levels | `pass-system.md` |
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

Keep detailed explanations in the document that owns the topic. Other documents should summarize briefly and link only when the reader is likely to need the full version.

- Put opcode behavior and per-backend JIT status in `instruction-set.md`.
- Put heap ownership and RC rules in `memory-model.md`.
- Put boxed value layout and kind rules in `value-representation.md`.
- Put JIT implementation contracts in `jit-internals.md`.
- Put generated opcode-fusion rules and backend coverage in `fusion.md`.
- Put benchmark numbers in `benchmarks.md`.
- Put host conversion details in `host-integration.md`.
- Put platform support in `compatibility.md`.

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
- Link only where it improves navigation.
- Keep examples current with code.
- Use `minivm` consistently for the project name.
