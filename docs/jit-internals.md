# JIT Internals

Current status and planned ownership for the JIT rebuild.

`architecture.md` owns package boundaries; `coding-patterns.md` owns code design; `testing.md` owns test contracts; `jit-lessons.md` owns historical evidence.

## Status

The previous ARM64 JIT was removed (2026-09). Threaded execution and AOT optimization are the current implementation; a native tier is being rebuilt as one compiler pipeline.

## Current owners

| Concern | Owner |
|---|---|
| Threaded execution | `interp/` |
| Bytecode to SSA | `transform/` |
| SSA IR | `internal/ssa/` |
| SSA passes | `internal/ssa/transform/` |
| Machine encoding and executable memory | `internal/asm/` |
| ARM64 encoding | `internal/asm/arm64/` |
| Profiling | `prof/` |

## Planned rebuild

The rebuild targets one compiler pipeline:

```text
bytecode
  ↓
transform.Translate
  ↓
internal/ssa
  ↓
SSA passes
  ↓
machine lowering
  ↓
internal/asm
  ↓
ARM64 native code
```

The target design adds a runtime contract in `internal/asm`, a target-neutral compiler under a JIT implementation package, and target lowering under a native architecture package. These packages are planned, not current.

The rebuild MUST preserve threaded behavior as the semantic baseline. Native execution MUST resume through explicit runtime state rather than duplicate interpreter ownership.

## Evidence

`jit-lessons.md` records the previous implementation's evidence and the design decisions derived from it.

## Related

- `architecture.md`
- `value-representation.md`
- `testing.md`
- `jit-lessons.md`
