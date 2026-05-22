# Debugging

Bytecode-level debugging for embedders and test harnesses.

## When to Read

Read when building a debugger, tracer, or test harness around `interp.Run`.

Use it for:

- breakpoints
- step / next / finish control
- current bytecode location
- frame and operand stack inspection

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

`WithDebugger` installs the debugger hook, sets `WithTick(1)`, disables JIT with `WithThreshold(-1)`, and preserves exact threaded bytecode instruction boundaries.

## Controls

| Method | Effect |
|---|---|
| `Continue()` | Run until breakpoint, runtime error, context cancellation, fuel exhaustion, or exit |
| `Step()` | Execute one bytecode instruction, entering calls |
| `Next()` | Execute one bytecode instruction, stepping over calls |
| `Finish()` | Run until current frame returns |

Stops occur before the current instruction executes. `Run` returns `ErrStopped`. `Stop()` returns function index, instruction pointer, and breakpoint ID; stepping stops use breakpoint ID `0`.

## Breakpoints

Breakpoints use function index plus bytecode offset:

```go
id := dbg.Break(0, 10)
dbg.Enable(id, false)
dbg.Enable(id, true)
dbg.Clear(id)
```

Conditional breakpoint:

```go
dbg.BreakIf(0, 10, func(vm *interp.Interpreter) bool {
    return vm.Len() > 0
})
```

`Breakpoints()` returns sorted snapshot by breakpoint ID. Each breakpoint tracks `Hits`.

## Inspection

Inspect a stopped interpreter directly.

| Method | Use |
|---|---|
| `Func()` | current function slot; `0` is top-level |
| `IP()` | current bytecode offset |
| `Opcode()` | opcode at current bytecode offset |
| `FrameDepth()` | active frame count |
| `Frame(n)` | frame snapshot; `0` current, `1` caller |
| `Len()` / `Peek(n)` | operand stack inspection |
| `Local(n)` / `Global(n)` / `Const(n)` | value inspection |
| `Load(addr)` | resolve heap references |

`Frame(n)` returns `(fn, ip, bp, err)` without exposing mutable internal frame state.

## JIT and Precision

Debugging is bytecode-level. Exact stepping needs instruction-boundary execution, so `WithDebugger` disables JIT and uses `WithTick(1)`. Threaded compilation then preserves observable bytecode offsets.

Normal non-debug execution keeps threaded fusion optimizations such as NOP run collapsing and `const.get` + `call` fusion.
