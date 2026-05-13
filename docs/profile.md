# Profiling

minivm uses low-overhead sampling profiles for observability and JIT guidance. Profiles are collected on interpreter tick cadence, not every instruction by default.

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

The same tick drives context polling, fuel accounting, and hooks. Lower ticks give denser data but more overhead. `WithDebugger` uses instruction-accurate hooks. REPL `.profile` also uses `WithTick(1)` so small examples show exact per-instruction samples.

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

`Snapshot` includes total samples, function/IP/opcode samples, and JIT counters. Function percent is relative to total samples, IP percent to its function, and opcode percent to total samples.

## JIT Counters

`Snapshot().JIT` records aggregate JIT activity.

| Counter | Meaning |
|---|---|
| `Attempts` | function-level JIT compilation attempts |
| `Emits` | native segments emitted |
| `Links` | emitted segments linked and installed |
| `Skips` | cold segments skipped by profile policy |
| `Aborts` | candidates discarded as too short or ineligible |
| `Errors` | buffer, compile, or link errors |
| `Bytes` | total emitted native code bytes |
| `Time` | total JIT compile/link time |

`Skips` are profile policy decisions; `Aborts` are compilation eligibility decisions. Tune them separately.

## Profile-Guided JIT

JIT activates when `Samples(fn)` reaches the configured threshold rounded up to tick cadence. Default: `4096 / 128 = 32` samples. `WithThreshold(0)` activates on first sample; negative thresholds disable JIT.

At compile time, profile data is used to:

1. rank basic blocks by `Range(fn, block.Start, block.End)`; zero-sample blocks are skipped, hotter blocks compile first
2. emit candidate native segments only when their byte range has at least one sample; cold segments inside hot blocks are skipped

JIT does not currently recompile or tier-up after the first function-level compilation attempt.

## REPL Reporting

`.profile` reruns accumulated code once in a fresh VM with exact sampling and prints:

- total samples
- function sample table
- sampled IP table per function
- opcode sample table
- JIT counters, when any JIT activity occurred

`.profile` is side-effect free: it does not commit instructions, mutate REPL history, or change constants/types.
