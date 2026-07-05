# Debugging

Bytecode-level debugging for embedders, tests, and tooling built around `interp.Run`.

## When to Read

Use this document when changing or using `interp.NewDebugger`, `interp.WithDebugger`, breakpoints, stepping, test harnesses around `interp.Run`, bytecode-location inspection, or frame and stack inspection APIs.

For REPL command usage, see `docs/guides/repl.md`.

## Source of Truth

| Concern | File or API |
|---|---|
| debugger API | `interp.NewDebugger` |
| runtime integration | `interp.WithDebugger` |
| stop signal | `interp.ErrStopped` |
| REPL debugger commands | `docs/guides/repl.md` |

## Summary

The debugger provides precise bytecode-level control over interpreter execution.

Use it for:

- breakpoints
- step, next, and finish control
- current function and bytecode location
- frame inspection
- operand stack inspection
- local, global, constant, and heap value inspection

Debugging favors precision over speed. When a debugger is installed, JIT is disabled and execution runs at exact instruction boundaries.

## Setup

Create a debugger and install it with `interp.WithDebugger`.

```go
dbg := interp.NewDebugger()
dbg.Break(0, 5)

vm := interp.New(prog, interp.WithDebugger(dbg))
defer vm.Close()

for {
    err := vm.Run(ctx)
    if errors.Is(err, interp.ErrStopped) {
        stop := dbg.Stop()
        _ = stop

        dbg.Continue()
        continue
    }
    if err != nil {
        return err
    }
    break
}
```

`WithDebugger` applies the runtime settings required for precise debugging:

- installs the debugger hook
- sets `WithTick(1)`
- disables JIT with `WithThreshold(-1)`
- preserves exact threaded bytecode instruction boundaries

## Controls

| Method | Effect |
|---|---|
| `Continue()` | Run until breakpoint, runtime error, context cancellation, fuel exhaustion, or program exit |
| `Step()` | Execute one bytecode instruction, entering calls |
| `Next()` | Execute one bytecode instruction, stepping over calls |
| `Finish()` | Run until the current frame returns |

Stops happen before the current instruction executes.

When execution stops:

- `Run` returns `ErrStopped`
- `Stop()` returns the current function index, bytecode offset, and breakpoint ID
- stepping stops use breakpoint ID `0`

## Breakpoints

Breakpoints are identified by function index and bytecode offset. Function index `0` is the top-level program.

```go
id := dbg.Break(0, 10)

dbg.Enable(id, false)
dbg.Enable(id, true)

dbg.Clear(id)
```

Use `BreakIf` for conditional breakpoints.

```go
dbg.BreakIf(0, 10, func(vm *interp.Interpreter) bool {
    return vm.Len() > 0
})
```

`Breakpoints()` returns a sorted snapshot by breakpoint ID. Each breakpoint records its hit count in `Hits`.

## Inspection

Inspect state directly from a stopped interpreter.

| Method | Use |
|---|---|
| `Func()` | current function slot; `0` is top-level |
| `IP()` | current bytecode offset |
| `Opcode()` | opcode at the current bytecode offset |
| `FP()` | active frame count |
| `Frame(n)` | frame snapshot; `0` is current, `1` is caller |
| `Len()` | operand stack length |
| `Peek(n)` | operand stack value |
| `Local(n)` | local slot value |
| `Global(n)` | global slot value |
| `Const(n)` | constant value |
| `Load(addr)` | heap reference lookup |

`Frame(n)` returns a stable snapshot without exposing mutable internal frame state.

## Precision and JIT

Debugging is bytecode-level.

Exact stepping requires execution to stop at instruction boundaries, so `WithDebugger` disables JIT and sets tick frequency to one instruction.

Normal non-debug execution may still use threaded fusion and JIT optimizations, including NOP run collapsing, call fusion, hot trace compilation, and native loop execution.

These optimizations are disabled or bypassed during debugging when they would hide bytecode boundaries.

## Maintenance Notes

When changing debugger behavior:

- keep bytecode precision first
- keep stop state explicit
- avoid exposing mutable interpreter internals
- keep function and IP semantics consistent
- do not add JIT-specific behavior to the debugger API unless the bytecode-level contract remains clear

## Related Docs

- `docs/guides/repl.md` — `.debug`, breakpoints, and REPL inspection commands
- `docs/profile.md` — tick behavior and exact sampling
- `docs/jit-internals.md` — optimized execution paths disabled by debugging
