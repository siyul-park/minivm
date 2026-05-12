# Debugging

Bytecode-level debugging for embedders.

## When to Read This

Read this when you:

- build a debugger, tracer, or test harness around `interp.Run`
- need breakpoint, step, next, or finish control
- inspect the current bytecode location, call frames, or operand stack

## Setup

Use `NewDebugger` with `WithDebugger`:

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

`WithDebugger` installs the debugger hook, sets `WithTick(1)`, disables JIT with
`WithThreshold(-1)`, and asks the threaded compiler to preserve exact bytecode
instruction boundaries.

## Controls

| Method | Effect |
|---|---|
| `Continue()` | Run until a breakpoint, runtime error, context cancellation, fuel exhaustion, or normal exit |
| `Step()` | Execute one bytecode instruction, entering calls |
| `Next()` | Execute one bytecode instruction, stepping over calls |
| `Finish()` | Run until the current frame returns |

All stops happen before the current instruction executes. `Run` returns
`ErrStopped`, and `Stop()` returns the function index, instruction pointer, and
breakpoint ID (`0` when the stop was caused by stepping).

## Breakpoints

Breakpoints are identified by function index and bytecode offset:

```go
id := dbg.Break(0, 10)
dbg.Enable(id, false)
dbg.Enable(id, true)
dbg.Clear(id)
```

Use `BreakIf` for conditional breakpoints:

```go
dbg.BreakIf(0, 10, func(vm *interp.Interpreter) bool {
    return vm.Len() > 0
})
```

`Breakpoints()` returns a sorted snapshot by breakpoint ID. Each breakpoint
tracks `Hits`.

## Inspection

Use the stopped interpreter directly:

| Method | Use |
|---|---|
| `Func()` | current function slot (`0` for top-level code) |
| `IP()` | current bytecode offset |
| `Opcode()` | opcode at the current bytecode offset |
| `FrameDepth()` | active frame count |
| `Frame(n)` | frame snapshot; `0` is current frame, `1` is caller |
| `Len()` / `Peek(n)` | operand stack inspection |
| `Local(n)` / `Global(n)` / `Const(n)` | value inspection |
| `Load(addr)` | resolve heap references |

`Frame(n)` returns `(fn, ip, bp, err)` and does not expose mutable internal frame
state.

## JIT and Precision

Debugging is bytecode-level. Exact stepping requires instruction-boundary
execution, so `WithDebugger` disables JIT and uses `WithTick(1)`, which makes
threaded compilation preserve observable bytecode offsets. Normal non-debug
execution keeps threaded fusion optimizations such as NOP run collapsing and
`const.get` + `call` fusion.
