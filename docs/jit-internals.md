# JIT Internals

ARM64 JIT contracts at the interpreter boundary.

`architecture.md` owns package/runtime boundaries; `instruction-set.md` owns opcode status; `value-representation.md` owns value representation; `testing.md` owns tests.

## Ownership

| Concern | Owner |
|---|---|
| Threaded execution | `interp/threaded.go` |
| Trace recording | `interp/trace.go` |
| JIT planning | `internal/jit/` |
| SSA frontend | `internal/jit/frontend/` |
| SSA backend | `internal/jit/backend/` |
| ARM64 lowering | `internal/jit/arm64/` |
| Compile coordination | `internal/jit/compile/` |
| Tiering/retirement | `interp/tier.go`, `internal/jit/tier/` |
| Frame journal | `internal/journal/` |
| Callable ABI | `internal/asm/` |
| Hotness | `profile.md` |

## Model

Threaded execution is the correctness baseline. Every native path `MUST` have a threaded fallback.

```text
bytecode
  ↓
threaded root
  ↓
StaticPlan / TracePlan
  ↓
SSA frontend
  ↓
backend.Machine
  ↓
ARM64 code
  ↕
threaded fallback / bridge
```

Compilation consumes immutable input. Recording, snapshot creation, installation, and interpreter state mutation `MUST` remain on the interpreter goroutine; compilation `MAY` run inline or on the shared worker.

## Roots

| Root | Meaning |
|---|---|
| module entry | program start |
| function entry | function start |
| loop header | hot backward-branch target |

Entry roots own and tear down their frame. Loop roots re-enter a live frame and `MUST NOT` unwind it.

## Compilation

`jit.Compiler` tries the SSA backend. If lowering declines or emission/build fails, the agent `MUST` discard native artifacts and retain the existing threaded/plan path. Compilation `MUST NOT` mutate live interpreter state.

`StaticPlan` uses verified bytecode and forward dataflow. `TracePlan` uses immutable recorded execution.

A plan contains blocks, entry state, operations, and explicit edges. Build, layout, metadata, validation, and publication belong to the backend/compiler boundary.

## Static Planning

Static facts `MAY` include:

- stack kinds;
- constants and reference provenance;
- declared aggregate types;
- direct call targets;
- statically known dynamic arities.

Runtime shape/type/bounds/kind checks `MUST` remain guards. The planner `MUST` reject a root when a required fact is unprovable. It `MUST` prune blocks unreachable from the root.

## Trace Planning

Trace recording clones the interpreter and runs threaded handlers until return, loop boundary, branch exit, unsupported operation, trace limit, or abort. It `MUST NOT` mutate the live interpreter.

Recorded observations specialize call targets and heap shapes. Recursive calls from non-entry loop traces are fallback boundaries. Aborted recordings `MUST NOT` be published. Snapshots are immutable; compilation `MUST NOT` access live heap state.

## SSA Backend

`internal/jit/backend` owns:

- block layout;
- value/register bindings;
- block-parameter moves;
- `Deopt` metadata;
- bridge resume points.

`backend.Machine` hooks:

| Hook | Contract |
|---|---|
| `Lowers` | native lowering exists |
| `Traps` | lowering terminates in threaded control |
| `Open` | creates per-compile state |
| `Enter` | emits callable prologue |
| `Lower` | lowers/fuses operations |
| `Term` | lowers block terminator |
| `Leave` | emits deferred cold paths |

The backend `MUST NOT` emit target instructions.

## ARM64 Representation

Native representation is defined in `value-representation.md`; interpreter-visible values remain boxed.

`I64_ADD`, `I64_SUB`, `I64_MUL`, `I64_SHL`, and `I64_SHR_U` can leave the inline boxed range. Each lowering `MUST` guard immediately after computing.

Overflow `MUST` deopt through that operation's pre-op state, which contains the original operands. The interpreter resumes at the operation and re-executes it for heap promotion. No raw out-of-range i64 `MUST` enter a live SSA value.

Division additionally needs a divide-by-zero guard. Float-to-i64 conversion has conversion-specific range/NaN semantics. Remainder cannot overflow the inline boxed range for in-range operands and needs only its division guard.

## Guards and Deoptimization

A guard `MUST` provide:

1. native-path proof;
2. `OpState` sufficient to rebuild interpreter state.

`backend.Deopt` describes the state; ARM64 emits journal stores and the cold stub. Deopt `MUST` materialize VM slots, frames, stack pointer, resume IP, trap state, and required retains before threaded resume.

## Bridge

A bridge executes one unsupported operation in threaded code, then resumes native execution. `IsBridgeable` covers operations whose stack effect remains modelable. The native block resumes after the bridged operation.

## Frame Journal

`internal/journal` is the native/interpreter ABI. Header cells carry stack/global/frame pointers, entry/resume IPs, trap state, exit metadata, and native runtime state. Frame records represent inlined frames.

## Calls, Loops, Suspension

Native calls `MUST` use interpreter-owned native-entry slots and `MUST` fall back when the target is absent. A constant, non-self-recursive, non-captured callee with an all-scalar signature lowers to a direct BLR through its natives slot; the frontend's speculative callee retain is dropped before the BLR, since threaded constant-callee dispatch takes no matching retain. Every other callee stays on the plan pipeline. Loop back-edges `MUST` commit deopt state and use a safepoint budget.

Suspension is terminal fallback; native code `MUST NOT` resume inside a suspended native frame.

`RETURN_CALL` remains a threaded boundary for the SSA backend because it changes frame identity.

## Ownership

Native values borrow VM storage unless the IR owns them. Cold paths `MUST` restore interpreter ownership.

- Slot-loaded refs are borrowed;
- produced refs are owned;
- `OpRetain` / `OpRelease` encode ownership transitions;
- deopt records ownership per stack entry;
- deferred refs `MUST` materialize before a committing loop back-edge.

## Backend Status

Per-opcode status belongs to `instruction-set.md`. Unsupported lowering `MUST` either bridge when stack effects are modelable or remain threaded. A machine decline `MUST NOT` change interpreter semantics.

## Related

- `architecture.md`
- `instruction-set.md`
- `value-representation.md`
- `memory-model.md`
- `testing.md`
- `profile.md`
