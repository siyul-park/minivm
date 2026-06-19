# Profiling

minivm uses low-overhead sampling profiles for observability and JIT guidance. Profiles collected on interpreter tick cadence, not every instruction by default.

## When to Read

Read when exposing execution metrics, changing `interp.Tracer` or profile snapshots, changing `WithTick`, `WithThreshold`, or JIT trace triggering, or interpreting REPL `.profile` output.

## Sampling Model

`interp.Run` records one sample every `WithTick` executed instructions; default tick is `128`.

Each sample records:

| Field | Meaning |
|---|---|
| function index | `0` for top-level; function constants use `constant index + 1` |
| instruction pointer | byte offset in that function bytecode |
| opcode | raw opcode byte at sampled IP |

Same tick drives context polling, fuel accounting, and hooks. Lower ticks give denser data but more overhead. `WithDebugger` uses instruction-accurate hooks. REPL `.profile` also uses `WithTick(1)` so small examples show exact per-instruction samples.

A fully JIT-compiled loop runs in native code and bypasses this tick, so it carries its own safepoint at each back-edge: every `tick` back-edges it yields back to the interpreter and runs the same coordination (context, fuel, hook, profiling). The cadence is counted in back-edges, not instructions, so fuel/cancellation in a native loop is bounded but approximate. See `docs/jit-internals.md` (Loop Safepoints).

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

For pooled execution, use `pool.Profile()`. `NewPool` creates one shared
`Tracer` for all members; `Put`/`Close` flush member-local samples into it, and
shared JIT counters are recorded when compilation is published.

Internal hot-path methods:

| Method | Use |
|---|---|
| `Add(fn, ip, op)` | record one sample |
| `Samples(fn)` | function sample count; used by JIT threshold checks |
| `AddMetric(name, value, labels...)` | record a named counter-style metric |

Public reporting APIs:

| Method | Use |
|---|---|
| `Interpreter.Profile()` | aggregate plus the interpreter's unflushed local samples |
| `Pool.Profile()` | shared aggregate for all flushed pool members |
| `Tracer.Profile()` | shared aggregate for a manually shared tracer |

Metrics include total samples, function/IP/opcode samples, and named JIT counters. Function percent relative to total samples, IP percent to its function, opcode percent to total samples. `Interpreter.Profile()` merges the current interpreter's unflushed local samples with its shared `Tracer`; `Tracer.Profile()` reports only the shared aggregate.

## JIT Metrics

JIT activity is exported as ordinary named metrics.

| Metric | Meaning |
|---|---|
| `vm_jit_attempts_total` | function-level tracing/native compilation attempts |
| `vm_jit_emits_total` | native trace objects emitted |
| `vm_jit_links_total` | callable function entries installed |
| `vm_jit_skips_total` | reserved; trace-only JIT leaves this at zero |
| `vm_jit_errors_total` | buffer, compile, or link errors |
| `vm_jit_bytes_total` | total emitted native code bytes |

## Profile-Guided JIT

JIT activates when `Samples(fn)` reaches configured threshold rounded up to tick
cadence. Default: `4096 / 128 = 32` samples. `WithThreshold(0)` activates on
first sample; negative thresholds disable JIT. Each hot attempt records a runtime
trace before native compilation. Pool members use the same rounded threshold;
when a shared `Cache` is supplied, the trigger count is aggregated there so only
one member wins native compilation for each function. Trace trees themselves are
currently interpreter-local.

At compile time, the `Tracer` supplies the entry trace tree for the hot function.
Profile samples still drive when a function becomes due, while recorded branch
exits can request a later recompile after the exit counter reaches its threshold.

JIT does not currently tier-up beyond the ARM64 trace backend.

## REPL Reporting

`.profile` reruns accumulated code once in fresh VM with exact sampling and prints:

- total samples
- function sample table
- sampled IP table per function
- opcode sample table
- JIT counters, when any JIT activity occurred

`.profile` is side-effect free: does not commit instructions, mutate REPL history, or change constants/types.
