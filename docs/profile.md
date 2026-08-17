# Profiling

Low-overhead execution profiling and JIT hotness control.

## When to Read

Use this document when changing profiler sampling, `WithTick`, `WithThreshold`, pool aggregation, or REPL `.profile` output.

For JIT implementation details, see `docs/jit-internals.md`.

## Model

minivm has two independent signals:

- **Samples** are for profiling and observability.
- **Hot events** decide JIT compilation.

Sampling does not determine JIT tiering.

## Sampling

`interp.Run` records a sample every `WithTick` executed instructions. The default is `128`.

Each sample contains the function, bytecode IP, and opcode.

The tick path also handles context polling, fuel, hooks, and pool coordination. A run with none of these features skips that work.

Lower `WithTick` values provide denser samples at higher runtime cost. `WithDebugger` and REPL `.profile` use exact instruction sampling.

Compiled loops use safepoint budgets rather than interpreter ticks for their runtime coordination.

## API

```go
p := prof.New()
vm := interp.New(prog, interp.WithProfiler(p))
if err := vm.Run(ctx); err != nil {
    return err
}
if err := vm.Close(); err != nil {
    return err
}
metrics := p.Metrics()
```

`WithProfiler` attaches a profiler to an interpreter or pool. Pool members flush their local samples when returned or closed.

## Metrics

The profiler exposes aggregate JIT and runtime metrics, including compilation attempts, emitted entries, native entries, exits, yields, and GC activity.

Detailed metrics use stable labels such as `func`, `ip`, `kind`, `frontend`, and `reason`. See the metric names in `prof` for the current set.

## Hotness

JIT compilation is driven by two hot events:

- a function entry
- a report from a backward branch

A function becomes hot after the configured number of events. The default threshold is `64`.

| Setting | Effect |
|---|---|
| `WithThreshold(0)` | compile on the first hot event |
| `WithThreshold(n > 0)` | compile after `n` hot events |
| `WithThreshold(n < 0)` | disable compilation |

The threshold is independent of `WithTick`.

`RESUME` and host-callback trampolines count as normal entries. Backward `BR`, `BR_IF`, and `BR_TABLE` report loop hotness. Forward branches have no hotness counter.

Back-edge reports occur every eight iterations with a rotating phase. This avoids systematically observing only the last iteration of short loops.

Pool members use the same threshold. A shared cache aggregates hot events so only one member compiles a root at a time.

## Cooling

Once all entry and loop roots for a function have been attempted, the function is cooled. Cooling removes further hotness instrumentation and capture overhead while leaving any installed native code active.

A later shared-module installation can reactivate a cold function.

## REPL

`.profile` runs the accumulated REPL program with exact sampling and reports hot functions, hot instruction pointers, hot opcodes, and JIT metrics.

## Maintenance

- Keep sampling and JIT hotness independent.
- Keep normal execution free of sampling overhead unless `WithTick` work is required.
- Keep profile aggregation deterministic.
- Keep pool-local and shared state separate.
- Preserve exact-sampling behavior for debugging and REPL reporting.

## Related Docs

- `docs/jit-internals.md` — trace recording, lowering, loops, and native fallback
- `docs/debugging.md` — debugger execution model
- `docs/guides/repl.md` — `.profile`
- `docs/benchmarks.md` — benchmark results and methodology
