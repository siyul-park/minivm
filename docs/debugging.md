# Debugging

Bytecode-level debugger for `interp.Run`.

`guides/repl.md` owns REPL commands.

## API

| Concern | Owner |
|---|---|
| debugger | `interp.NewDebugger` |
| runtime integration | `interp.WithDebugger` |
| stop result | `interp.ErrStopped` |
| REPL commands | `guides/repl.md` |

Debugger provides breakpoints, stepping, function/IP inspection, frames, operand stack, locals, globals, constants, and heap lookup.

`WithDebugger` sets `WithTick(1)` and `WithThreshold(-1)`: JIT and fusion are disabled and execution stops at bytecode boundaries.

## Setup

```go
dbg := interp.NewDebugger()
dbg.Break(0, 5)
vm := interp.New(prog, interp.WithDebugger(dbg))
defer vm.Close()

for {
    err := vm.Run(ctx)
    if errors.Is(err, interp.ErrStopped) {
        _ = dbg.Stop()
        dbg.Continue()
        continue
    }
    if err != nil { return err }
    break
}
```

## Controls

| Method | Effect |
|---|---|
| `Continue()` | run to breakpoint, error, cancellation, fuel exhaustion, or exit |
| `Step()` | execute one instruction, entering calls |
| `Next()` | execute one instruction, stepping over calls |
| `Finish()` | run to current-frame return |

Stops occur before the next instruction executes.

## Breakpoints

Breakpoints use function index and bytecode offset; function `0` is top-level.

```go
id := dbg.Break(0, 10)
dbg.Enable(id, false)
dbg.Enable(id, true)
dbg.Clear(id)
dbg.BreakIf(0, 10, func(vm *interp.Interpreter) bool { return vm.Len() > 0 })
```

`Breakpoints()` returns a breakpoint-ID-sorted snapshot with hit counts. `Stop()` returns function, IP, and breakpoint ID; step stops use ID `0`.

## Inspection

| Method | Result |
|---|---|
| `Func()` | current function; `0` = top-level |
| `IP()` | next bytecode offset |
| `Opcode()` | opcode at `IP()` |
| `FP()` | active frame count |
| `Frame(n)` | stable frame snapshot; `0` current, `1` caller |
| `Len()` | operand stack length |
| `Peek(n)` | operand stack value |
| `Local(n)` | local slot |
| `Global(n)` | global slot |
| `Const(n)` | constant |
| `Load(addr)` | heap value |

## Precision

Debugger execution is exact bytecode execution. Optimization paths that hide instruction boundaries are disabled by `WithDebugger`.

## Maintenance

The agent `MUST` keep stop state explicit, bytecode locations stable, mutable interpreter state unexposed, and debugger semantics independent of optimization details.

## Related

- `guides/repl.md`
- `profile.md`
- `jit-internals.md`
