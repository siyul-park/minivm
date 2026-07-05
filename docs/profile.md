# Profiling

Low-overhead execution profiling for observability and compiler guidance.

## When to Read

Use this document when changing profiler samples, profile snapshots, tick cadence, hotness thresholds, pool aggregation, or REPL `.profile` output.

For trace compiler internals, see `docs/jit-internals.md`.

## Source of Truth

| Concern | File or API |
|---|---|
| profiler implementation | `interp.Tracer` |
| runtime sampling | `interp.Run` |
| tick option | `interp.WithTick` |
| hotness threshold option | `interp.WithThreshold` |
| REPL profile command | `cli` REPL implementation |

## Summary

minivm profiles execution by sampling on interpreter ticks. It does not record every instruction by default.

Profiles are used for runtime observability, REPL output, hotness decisions, and runtime counters.

## Sampling Model

`interp.Run` records one sample every `WithTick` executed instructions.

Default tick:

```text
128
```

Each sample records:

| Field | Meaning |
|---|---|
| function index | `0` for top-level code; functions use their heap ref |
| instruction pointer | byte offset in that function's bytecode |
| opcode | raw opcode byte at the sampled IP |

The same tick also drives context polling, fuel accounting, hooks, profiling, and threshold checks.

Lower tick values produce denser samples but add more overhead.

`WithDebugger` uses instruction-accurate hooks. REPL `.profile` also uses `WithTick(1)` so small programs show exact per-instruction samples.

## Loop Safepoints

Compiled loops do not pass through the normal interpreter tick on every bytecode instruction.

Instead, loops use a safepoint at back-edges. Every `tick` back-edges, the loop returns to the interpreter coordination path for context checks, fuel checks, hook calls, and profile samples.

For compiled loops, cadence is counted in back-edges, not bytecode instructions. This keeps cancellation and fuel bounded, but approximate.

## Library API

```go
tracer := interp.NewTracer()

vm := interp.New(prog, interp.WithTracer(tracer))
defer vm.Close()

if err := vm.Run(ctx); err != nil {
    return err
}

snap := vm.Profile()
shared := tracer.Profile()
```

For pooled execution:

```go
profile := pool.Profile()
```

`NewPool` creates one shared `Tracer` for all pool members. `Put` and `Close` flush member-local samples into the shared tracer.

## Reporting API

| Method | Use |
|---|---|
| `Interpreter.Profile()` | shared aggregate plus this interpreter's unflushed local samples |
| `Pool.Profile()` | shared aggregate for flushed pool members |
| `Tracer.Profile()` | shared aggregate for a manually shared tracer |

Reported data includes total samples, function samples, instruction pointer samples, opcode samples, and named metrics.

Percentages are interpreted as:

| Report | Percentage base |
|---|---|
| function percent | total samples |
| IP percent | samples in that function |
| opcode percent | total samples |

## Metrics

Runtime compiler activity is exported as named metrics.

| Metric | Meaning |
|---|---|
| `vm_jit_attempts_total` | compilation attempts |
| `vm_jit_emits_total` | emitted trace objects |
| `vm_jit_links_total` | installed callable entries |
| `vm_jit_skips_total` | reserved; currently zero |
| `vm_jit_errors_total` | compile or link errors |
| `vm_jit_bytes_total` | generated code bytes |

Metrics use the same reporting path as other profile counters.

## Hotness Thresholds

Compilation is driven by profile samples.

A function becomes hot when:

```text
Samples(fn) >= threshold rounded to tick cadence
```

Default threshold:

```text
4096 executed instructions
```

With the default tick of `128`, this is about `32` samples.

| Setting | Effect |
|---|---|
| `WithThreshold(0)` | compile on the first sample |
| `WithThreshold(n > 0)` | compile after the rounded sample threshold |
| `WithThreshold(n < 0)` | disable compilation |

Pool members use the same rounded threshold. With a shared cache, trigger counts are aggregated so only one member compiles each module or function slot.

minivm does not currently tier beyond the ARM64 trace backend.

## REPL Reporting

`.profile` reruns the accumulated REPL program once in a fresh VM with exact sampling.

It prints total sample count, function samples, sampled IPs per function, opcode samples, and runtime counters when present.

`.profile` is side-effect free. It does not commit instructions, mutate REPL history, change constants, or change types.

## Maintenance Notes

When changing profiling code:

- keep sampling low overhead
- do not add per-instruction work to normal execution unless tick requires it
- keep profile aggregation deterministic
- keep pool-local and shared samples clearly separated
- flush local samples at pool `Put` and `Close`
- keep hotness based on samples, not wall-clock time
- keep named metrics counter-like and easy to aggregate
- avoid exposing internal trace state through profile APIs
- preserve debugger and REPL exact-sampling behavior

## Related Docs

- `docs/jit-internals.md` — trace recording and loop safepoints
- `docs/debugging.md` — exact stepping and debugger tick behavior
- `docs/guides/repl.md` — `.profile` command
- `docs/benchmarks.md` — benchmark methodology and runtime measurements
