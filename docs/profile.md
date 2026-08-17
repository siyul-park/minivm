# Profiling

Low-overhead execution profiling for observability and compiler guidance.

## When to Read

Use this document when changing profiler samples, profile snapshots, tick cadence, hotness thresholds, pool aggregation, or REPL `.profile` output.

For trace compiler internals, see `docs/jit-internals.md`.

## Source of Truth

| Concern | File or API |
|---|---|
| profiler implementation | `prof` package |
| runtime sampling | `interp.Run` |
| tick option | `interp.WithTick` |
| hotness threshold option | `interp.WithThreshold` |
| REPL profile command | `cli` REPL implementation |

## Summary

minivm profiles execution by sampling on interpreter ticks. It does not record every instruction by default.

Profiles are used for runtime observability, REPL output, and runtime counters. They no longer decide hotness: compilation is triggered by counters compiled into the call and back-edge handlers, so a program with no profiler attached pays nothing for tiering up. See Hotness Thresholds.

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

The same tick also drives context polling, fuel accounting, hooks, profiling, and a pool's shared-module handshake. A run with none of those attached skips the tick loop entirely.

Lower tick values produce denser samples but add more overhead.

`WithDebugger` uses instruction-accurate hooks. REPL `.profile` also uses `WithTick(1)` so small programs show exact per-instruction samples.

## Loop Safepoints

Compiled loops do not pass through the normal interpreter tick on every bytecode instruction.

Instead, loops use a fixed back-edge budget before returning to the interpreter coordination path for context checks, fuel checks, hook calls, and profile samples.

For compiled loops, cadence is counted in back-edges, not bytecode instructions. This keeps cancellation and fuel bounded, but approximate.

## Library API

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

For pooled execution:

```go
p := prof.New()
pool := interp.NewPool(prog, 4, interp.WithProfiler(p))

vm, err := pool.Get(ctx)
if err != nil {
    return err
}
err = vm.Run(ctx)
pool.Put(vm)
if err != nil {
    return err
}
if err := pool.Close(); err != nil {
    return err
}
metrics := p.Metrics()
```

`WithProfiler` attaches a shared profiler. `Interpreter.Close`, `Pool.Put`, and `Pool.Close` flush member-local samples into it.

A pooled interpreter's local `Collector` flushes on every `Put`, so its `reset` after merging keeps the backing arrays it grew to and only zeroes recorded counts, instead of discarding them. This preserves the geometric growth in `Collector`'s internal `grow`; a `Pool` under steady load reaches a stable local capacity and stops allocating on later flush cycles.

## Reporting API

| API | Use |
|---|---|
| `interp.WithProfiler(p)` | attach a profiler to one interpreter or pool |
| `prof.Profiler.Metrics()` | read flushed aggregate samples and counters |
| `prof.Collector.Metrics()` | read a collector directly, mainly for tests and internal plumbing |

Reported data includes total samples, function samples, instruction pointer samples, opcode samples, and named metrics.

Percentages are interpreted as:

| Report | Percentage base |
|---|---|
| function percent | total samples |
| IP percent | samples in that function |
| opcode percent | total samples |

## Metrics

Runtime compiler activity is exported as named metrics.

Aggregate metrics remain available for existing consumers:

| Metric | Meaning |
|---|---|
| `vm_jit_attempts_total` | compilation attempts |
| `vm_jit_emits_total` | emitted trace objects |
| `vm_jit_errors_total` | compile or link errors |
| `vm_jit_bytes_total` | generated code bytes |
| `vm_gc_cycles_total` | collections run |
| `vm_gc_slots_total` | heap slots a collection walked, summed over cycles |

Every collection pass walks the whole heap, so `vm_gc_slots_total` is the
collector's total work; divided by the allocations a run performs it is the
price paced allocation pays per object. A rising `vm_gc_cycles_total` on a
program with a steady live set means garbage that reference counting is not
reclaiming.

Detailed lifecycle metrics retain bounded typed dimensions until `Metrics()`
converts them to ordered string labels:

| Metric | Labels, in order | Meaning |
|---|---|---|
| `vm_jit_trace_captures_total` | `func,ip,outcome,reason` | actual trace capture results; cache hits are not attempts |
| `vm_jit_compiles_total` | `func,ip,trigger,frontend,outcome,reason` | compile results owned by the solo compiler or shared-cache winner |
| `vm_jit_entry_emits_total` | `func,ip,kind,frontend` | emitted native entries |
| `vm_jit_entry_bytes_total` | `func,ip,kind,frontend` | generated bytes owned by each entry |
| `vm_jit_native_entries_total` | `func,ip,kind,frontend` | native entry invocations |
| `vm_jit_native_exits_total` | `func,ip,kind,frontend,reason,opcode` | descriptor-attributed fallback exits |
| `vm_jit_native_yields_total` | `func,ip,kind,frontend` | native safepoint yields, separate from exits |

`none` denotes a reason or opcode that cannot be attributed. Attributable
fallbacks report their concrete source opcode; synthetic boundaries such as a
`trace-cut` report opcode `none`. Registered native entry, exit, and yield
counters keep stable storage across collector resets, so the runtime increment
is constant-time and allocation-free. Merging collectors adds each exact labeled
row once and retains destination counter objects.

## Hotness Thresholds

Compilation is driven by hot events, not by profile samples. One hot event is
one call into a function, or one report from one of its back edges. Both are
counted per function by handlers the threader compiles in, so hotness is
observed at exact anchors in exact runtime state rather than at whichever
instruction a countdown happened to stop on.

A function becomes hot when:

```text
HotEvents(fn) >= threshold
```

Default threshold:

```text
64 hot events
```

| Setting | Effect |
|---|---|
| `WithThreshold(0)` | compile a function's entry on its first hot event, and its loop roots as soon as a back edge reports |
| `WithThreshold(n > 0)` | compile after `n` hot events |
| `WithThreshold(n < 0)` | disable compilation |

The threshold is used as given. It is not divided by the tick, because nothing
about tiering up runs on the tick loop.

Each call site counts its callee, so a function accumulates one event per entry
however it was reached and from however many sites. `RESUME` counts a resumed
coroutine and the `invoke` trampoline counts a callee reached from a host
callback, so no entry path is exempt.

Back edges keep a counter per branch site. The site reports every eight
iterations, and each report is one hot event, so a module body entered once
still becomes hot by looping. Each report restarts the count at a rotating
offset: a fixed interval would report at the same iteration of every trip, and a
loop whose trip count divides the interval would only ever be observed on the
iteration that exits it. A loop root is only compiled once its function has
crossed the threshold, so the whole-function plan is always attempted first.

Backward `BR`, `BR_IF`, and `BR_TABLE` all report. Direction is settled when the
site is threaded, so a forward branch pays one predicted test on the arm it
takes and nothing on the arm it does not.

Pool members use the same threshold. With a shared cache, hot events are
aggregated across members so only one member compiles at a time, while a
per-function queue retains distinct exact loop roots and prioritizes newer
side-exit work.

## Cooling

Trace capture and back-edge reporting are only worth their cost while
compilation can still succeed. A function whose entry root and every loop header
have all been attempted is cooled: it is marked cold and reverts to the
zero-overhead branch handlers. A cold function stops capturing and stops
triggering compiles, while an attached profiler still receives its per-tick
samples.

Whether a root installed anything does not matter here. Every root has been
tried either way, so nothing further can be compiled and the instrumentation
has no remaining purpose; installed entries stay installed and keep running.
Withdrawing the instrumentation is worth several percent on kernels whose hot
functions do install, so this is a deliberate rule, not an oversight.

Two unproductive observations cool a function. Capture rejections count against
the same per-anchor attempt limit as every other rejection, including a
mis-anchored entry, so a repeatedly unusable anchor stops being retried instead
of costing one rejected capture per tick forever.

A cold function resumes if native code later lands on it, which is how a pooled
member adopts a module a peer published. `docs/jit-internals.md` covers
retirement, the runtime counterpart that removes a native entry that is already
installed but not paying for itself.

minivm does not currently tier beyond the ARM64 trace backend.

## REPL Reporting

`.profile` reruns the accumulated REPL program once in a fresh VM with exact sampling.

It normalizes the flat metric stream before rendering these ranked sections:

| Section | Columns | Default rank and limit |
|---|---|---|
| `hot functions` | `func,samples,total%,native-entries,native-exits,exit%` | samples, top 10 |
| `hot ips for func F` | `ip,samples,func%,native-kind,emits,entries,exits` | samples within each displayed function, top 10 per function |
| `hot opcodes` | `opcode,samples,total%` | samples, top 10 |
| `jit summary` | `attempts,emits,errors,bytes,native-entries,native-exits,native-yields` | aggregate totals |
| `jit entries` | `func,ip,kind,frontend,emits,bytes,entries,exits,exit%` | native entries, then emits, top 10 |
| `jit exit reasons` | `func,ip,reason,opcode,count,entry%` | exit count, top 10 |
| `jit misses` | `func,ip,phase,reason,count` | capture or compile miss count, top 10 |

Full row keys break ties deterministically. The report unions sampled anchors,
compile results, emission rows, and runtime rows. A sampled anchor without a
compile row is rendered as `kind=none,frontend=interpreted`; a compile miss is
rendered as a zero-count entry beside its miss row; and an emitted entry remains
visible even when it was never entered. Sample-only anchors never synthesize a
miss reason. Only rejected captures are misses; a published partial capture at
the operation limit remains a successful capture outcome and is not listed.

Function and opcode percentages use total samples. IP percentages use the
matching function's samples. Function exit percentages use all native entries
for that function; entry and exit-reason percentages use native entries for the
same anchor. A zero denominator prints `-`. Native yields appear in the JIT
summary and are excluded from exit percentages and miss ranking.

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
