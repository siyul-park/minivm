# Profiling

minivm uses low-overhead sampling profiles for observability and JIT guidance.
Profiles are collected on the interpreter tick cadence rather than on every
instruction by default.

## When to Read This

Read this when you:

- expose execution metrics to embedders or CLI users
- change `prof.Stats` or `interp.WithProfile`
- change `WithTick`, `WithThreshold`, or JIT segment selection
- interpret `.profile` output from the REPL

## Sampling Model

`interp.Run` records one sample every `WithTick` executed instructions. The
default tick is 128. Each sample records:

| Field | Meaning |
|---|---|
| function index | `0` for top-level code; function constants are stored at `constant index + 1` |
| instruction pointer | byte offset in that function's bytecode |
| opcode | raw opcode byte at the sampled IP |

The same tick cadence also drives context polling, fuel accounting, and hook
callbacks. Lower ticks give denser profile data but add more polling and
sampling overhead. The REPL `.profile` command intentionally uses `WithTick(1)`
so small examples show exact per-instruction samples.

## Library API

Create a profile and pass it to the interpreter:

```go
p := prof.New()
vm := interp.New(prog, interp.WithProfile(p))
defer vm.Close()

if err := vm.Run(ctx); err != nil {
    return err
}

snap := p.Snapshot()
```

Use allocation-free counters on hot paths:

| Method | Use |
|---|---|
| `Add(fn, ip, op)` | record one sample |
| `Samples(fn)` | function sample count; used by JIT threshold checks |
| `Range(fn, start, end)` | sample count for byte range `[start, end)` |

Use reporting helpers outside hot paths:

| Method | Use |
|---|---|
| `Func(fn)` | copy one function profile |
| `IP(fn, ip)` | copy one instruction profile |
| `Snapshot()` | immutable deep copy of all profile data |

`Snapshot` contains total samples, function/IP samples, opcode samples, and JIT
counters. Percent fields are derived from the relevant aggregate: function
percent is relative to total samples, IP percent is relative to its function,
and opcode percent is relative to total samples.

## JIT Counters

`Snapshot().JIT` records aggregate JIT activity:

| Counter | Meaning |
|---|---|
| `Attempts` | function-level JIT compilation attempts |
| `Emits` | native segments emitted into executable memory |
| `Links` | emitted segments successfully linked and installed |
| `Skips` | cold segments skipped by profile-guided selection |
| `Aborts` | candidate segments discarded because they are too short or otherwise ineligible |
| `Errors` | buffer, compile, or link errors |
| `Bytes` | total emitted native code bytes |
| `Time` | total time spent compiling/linking JIT code |

`Skips` are profile policy decisions; `Aborts` are compilation eligibility
decisions. Treat them separately when tuning thresholds.

## Profile-Guided JIT

The JIT activates for a function when `Samples(fn) == WithThreshold / WithTick`.
The default is `4096 / 128 = 32` samples.

At compilation time, the JIT uses profile data in two places:

1. Basic blocks are ranked by heat with `Range(fn, block.Start, block.End)`.
   Blocks with zero samples are skipped, and hotter blocks compile first.
2. Candidate native segments are emitted only if their own byte range has at
   least one sample. Cold segments inside an otherwise hot block are skipped.

The JIT does not currently recompile or tier-up code after the first
function-level compilation attempt.

## REPL Reporting

The REPL exposes profiling with `.profile`. It reruns the accumulated program
once in a fresh VM with exact sampling and prints:

- total sample count
- function sample table
- sampled IP table per function
- opcode sample table
- JIT counters when any JIT activity occurred

`.profile` is side-effect free: it does not commit instructions, mutate REPL
history, or change constants/types.
