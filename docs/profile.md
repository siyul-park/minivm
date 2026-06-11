# Profiling

minivm uses low-overhead sampling profiles for observability and JIT guidance. Profiles collected on interpreter tick cadence, not every instruction by default.

## When to Read

Read when exposing execution metrics, changing `prof.Stats` or `interp.WithProfile`, changing `WithTick`, `WithThreshold`, or JIT segment selection, or interpreting REPL `.profile` output.

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
p := prof.New()
vm := interp.New(prog, interp.WithProfile(p))
defer vm.Close()

if err := vm.Run(ctx); err != nil {
    return err
}

snap := p.Snapshot()
```

For pooled execution, prefer `pool.Profile()` over passing one `*prof.Stats`
through `WithProfile` to `NewPool`: `prof.Stats` is not synchronized for
concurrent `Add` calls. `Pool.Profile()` returns the cache aggregate as of the
last `Put`/`Close`, plus shared JIT counters recorded at publication time.

Hot-path allocation-free counters:

| Method | Use |
|---|---|
| `Add(fn, ip, op)` | record one sample |
| `Samples(fn)` | function sample count; used by JIT threshold checks |
| `Range(fn,start,end)` | sample count for byte range `[start,end)` |

Reporting helpers outside hot paths:

| Method | Use |
|---|---|
| `Func(fn)` | copy one function profile |
| `IP(fn,ip)` | copy one instruction profile |
| `Snapshot()` | immutable deep copy of all profile data |
| `Merge(snapshot)` | add a snapshot into this profile on a cold path |
| `Reset()` | clear all profile data after a cold-path flush |

`Snapshot` includes total samples, function/IP/opcode samples, and JIT counters. Function percent relative to total samples, IP percent to its function, opcode percent to total samples.

## JIT Counters

`Snapshot().JIT` records aggregate JIT activity.

| Counter | Meaning |
|---|---|
| `Attempts` | function-level JIT compilation attempts |
| `Emits` | native objects emitted; a merged fallthrough trace counts once |
| `Links` | callable entry IPs installed; one object may install multiple entries |
| `Skips` | cold segments skipped by profile policy |
| `Errors` | buffer, compile, or link errors |
| `Bytes` | total emitted native code bytes |

## Profile-Guided JIT

JIT activates when `Samples(fn)` reaches configured threshold rounded up to tick cadence. Default: `4096 / 128 = 32` samples. `WithThreshold(0)` activates on first sample; negative thresholds disable JIT. Pool members use the same rounded threshold, but the trigger count is aggregated across the shared cache so only one member wins compilation for each function.

At compile time, profile data used to:

1. rank basic blocks by `Range(fn, block.Start, block.End)`; hotter blocks compile first, direct successors of hot blocks included for branch linking
2. emit candidate native segments only when byte range has at least one sample; cold segments inside hot blocks skipped

JIT does not currently recompile or tier-up after first function-level compilation attempt.

## REPL Reporting

`.profile` reruns accumulated code once in fresh VM with exact sampling and prints:

- total samples
- function sample table
- sampled IP table per function
- opcode sample table
- JIT counters, when any JIT activity occurred

`.profile` is side-effect free: does not commit instructions, mutate REPL history, or change constants/types.
