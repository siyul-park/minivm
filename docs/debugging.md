# Debugging

Bytecode-level debugger for `interp.Run`.

`guides/repl.md` owns REPL commands.

## API

| Concern | Owner |
|---|---|
| debugger | `debug.NewDebugger` |
| runtime integration | `interp.WithHook(dbg.Hook)`, `interp.WithTick(1)` |
| stop result | `debug.ErrStopped` |
| REPL commands | `guides/repl.md` |

The debugger exposes breakpoints, stepping, bytecode location, frames, stack, locals, globals, constants, and heap inspection. `WithTick(1)` runs the hook at every bytecode boundary, so stops occur there.

## Setup

```go
dbg := debug.NewDebugger()
dbg.Break(0, 5)
vm, err := interp.New(prog, interp.WithHook(dbg.Hook), interp.WithTick(1))
if err != nil { return err }
defer vm.Close()

for {
    err := vm.Run(ctx)
    if errors.Is(err, debug.ErrStopped) {
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

## Source Positions

A front-end maps bytecode back to its source through two optional fields beside `Handlers`: `types.Function.Positions` and, for module code, `program.Program.Positions`. Each `instr.Position` maps the instructions from its `IP` up to the next entry's to an opaque front-end source ID, ID `0` being none; IPs strictly ascend and lie on instruction boundaries; `instr.Find` returns the ID covering an ip. `types.Function.Name` is the function's source name. The VM never interprets either and execution never reads them, so a program without them pays nothing; optimization keeps them (`pass-system.md`, Bytecode). Resolving IDs to files, lines, and names stays in the front-end, as a source map's `sources` and `names` do. Text form: `name "<quoted>"` and `position ip=N source=M` lines in a function, and a `.positions` section in a program.

A `types.Frame` (`RuntimeError.Frames`, `types.Error.Frames`) resolves to a source in either tier: `Func` `0` reads the program's `Positions`, any other `Func` is the heap address `Interpreter.Load` returns the `*types.Function` for. The innermost frame's `IP` names the faulting instruction; an outer frame's `IP` is its return address, so its call is found at `IP-1`.

## Precision

On optimized code (bytecode `optimize.O3` or native), locals are not program semantics (`pass-system.md`): `Local` and `SetLocal` from a hook do not observe or affect the program's computation, as they do on threaded code. Debug the unoptimized program.

`WithTick(1)` runs no fused or native code, which hide bytecode boundaries: debugger execution is exact bytecode execution. A larger tick runs native code (`jit-internals.md` Ticks).

## Invariants

Stop state is explicit, bytecode locations remain stable, mutable interpreter state stays unexposed, and debugger semantics do not depend on optimization details.

## Related

- `guides/repl.md`
- `profile.md`
- `jit-internals.md`
