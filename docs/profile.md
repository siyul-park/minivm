# Profiling

Low-overhead execution profiling for observability and JIT guidance.

## Summary

minivm profiles execution by sampling on interpreter ticks, not by recording every instruction by default.

Profiles are used for:

* runtime observability
* REPL `.profile` output
* JIT hotness detection
* JIT activity metrics

Default rule:

* keep profiling cheap
* keep sampling tied to tick cadence
* keep JIT counters as ordinary metrics
* prefer simple counters over complex event streams
* use short, standard names such as `sample`, `metric`, `tick`, `profile`, and `trace`

## When to Read

Read this before changing:

* `interp.Tracer`
* profile snapshots
* `WithTick`
* `WithThreshold`
* JIT trace triggering
* pool profile aggregation
* REPL `.profile` output

## Sampling Model

`interp.Run` records one sample every `WithTick` executed instructions.

Default tick:

```text id="88d0ez"
128
```

Each sample records:

| Field               | Meaning                                              |
| ------------------- | ---------------------------------------------------- |
| function index      | `0` for top-level code; functions use their heap ref |
| instruction pointer | byte offset in that function’s bytecode              |
| opcode              | raw opcode byte at the sampled IP                    |

The same tick also drives:

* context polling
* fuel accounting
* hooks
* profiling
* JIT threshold checks

Lower tick values produce denser samples but add more overhead.

`WithDebugger` uses instruction-accurate hooks. REPL `.profile` also uses `WithTick(1)` so small programs show exact per-instruction samples.

## Native Loop Safepoints

A fully JIT-compiled loop runs in native code and does not pass through the normal interpreter tick on every bytecode instruction.

Instead, native loops use a safepoint at loop back-edges.

Every `tick` back-edges, the loop yields to the interpreter coordination path:

* context check
* fuel check
* hook call
* profile sample

For native loops, the cadence is counted in back-edges, not bytecode instructions. This keeps cancellation and fuel bounded, but approximate.

See `jit-internals.md` for loop lowering details.

## Library API

Basic usage:

```go id="o0rypa"
tracer := interp.NewTracer()

vm := interp.New(prog, interp.WithTracer(tracer))
defer vm.Close()

if err := vm.Run(ctx); err != nil {
    return err
}

snap := vm.Profile()
shared := tracer.Profile()
```

For pooled execution, use:

```go id="m50b4h"
profile := pool.Profile()
```

`NewPool` creates one shared `Tracer` for all pool members. `Put` and `Close` flush member-local samples into the shared tracer.

Shared JIT counters are recorded when compilation is published.

## Internal Hot-Path API

| Method                              | Use                                                              |
| ----------------------------------- | ---------------------------------------------------------------- |
| `Add(fn, ip, op)`                   | record one execution sample                                      |
| `Samples(fn)`                       | return sample count for a function; used by JIT threshold checks |
| `AddMetric(name, value, labels...)` | record a named counter-style metric                              |

Keep these APIs small. They are used on hot paths.

## Public Reporting API

| Method                  | Use                                                              |
| ----------------------- | ---------------------------------------------------------------- |
| `Interpreter.Profile()` | shared aggregate plus this interpreter’s unflushed local samples |
| `Pool.Profile()`        | shared aggregate for flushed pool members                        |
| `Tracer.Profile()`      | shared aggregate for a manually shared tracer                    |

Reported data includes:

* total samples
* function samples
* instruction pointer samples
* opcode samples
* named metrics

Percentages are interpreted as:

| Report           | Percentage base          |
| ---------------- | ------------------------ |
| function percent | total samples            |
| IP percent       | samples in that function |
| opcode percent   | total samples            |

`Interpreter.Profile()` merges the current interpreter’s unflushed local samples with its shared tracer.

`Tracer.Profile()` reports only the shared aggregate.

## JIT Metrics

JIT activity is exported as named metrics.

| Metric                  | Meaning                                                |
| ----------------------- | ------------------------------------------------------ |
| `vm_jit_attempts_total` | trace/native compilation attempts                      |
| `vm_jit_emits_total`    | native trace objects emitted                           |
| `vm_jit_links_total`    | callable entries installed                             |
| `vm_jit_skips_total`    | reserved; trace-only JIT currently leaves this at zero |
| `vm_jit_errors_total`   | buffer, compile, or link errors                        |
| `vm_jit_bytes_total`    | emitted native code bytes                              |

JIT metrics use the same reporting path as other profile counters.

## Profile-Guided JIT

JIT activation is driven by profile samples.

A function becomes hot when:

```text id="bmgku8"
Samples(fn) >= threshold rounded to tick cadence
```

Eligible function indexes:

* `0` for top-level module code
* function heap refs

Default threshold:

```text id="7rb1fv"
4096 executed instructions
```

With the default tick of `128`, this is about:

```text id="bjzo9o"
32 samples
```

Threshold behavior:

| Setting                | Effect                                 |
| ---------------------- | -------------------------------------- |
| `WithThreshold(0)`     | JIT on the first sample                |
| `WithThreshold(n > 0)` | JIT after the rounded sample threshold |
| `WithThreshold(n < 0)` | disable JIT                            |

Each hot attempt records a runtime trace before native compilation.

Pool members use the same rounded threshold. With a shared `Cache`, trigger counts are aggregated so only one member wins compilation for each module or function slot.

Profile samples decide when a slot is due for compilation. Recorded branch exits can later request recompilation after their own exit counter reaches threshold.

minivm does not currently tier beyond the ARM64 trace backend.

## REPL Reporting

`.profile` reruns the accumulated REPL program once in a fresh VM with exact sampling.

It prints:

* total sample count
* function sample table
* sampled IP table per function
* opcode sample table
* JIT counters, if any JIT activity occurred

`.profile` is side-effect free.

It does not:

* commit instructions
* mutate REPL history
* change constants
* change types

## Agent Notes

When changing profiling code:

* keep sampling low overhead
* do not add per-instruction work to normal execution unless tick requires it
* keep profile aggregation deterministic
* keep pool-local and shared samples clearly separated
* flush local samples at pool `Put` and `Close`
* keep JIT hotness based on samples, not wall-clock time
* keep named metrics counter-like and easy to aggregate
* avoid exposing internal trace state through profile APIs
* preserve debugger and REPL exact-sampling behavior

The best profiling change is cheap on the hot path, clear in aggregation, and useful for both humans and JIT decisions.
