# Profile

Runtime sampling and JIT hotness control.

`jit-internals.md` owns JIT implementation details.

## Signals

- `WithTick` samples execution and performs runtime coordination.
- Hot events request JIT compilation.
- Sampling and tiering are independent.

## Sampling

`interp.Run` samples after every `WithTick` instructions; the default is `128`.

Samples contain function, bytecode IP, and opcode. The tick path also handles context polling, fuel, hooks, and pool coordination; runs without required work skip the extra work.

Lower ticks increase sampling density and cost. Debugger and REPL `.profile` use exact instruction sampling. Compiled loops use safepoint budgets.

## API

```go
p := prof.New()
vm := interp.New(prog, interp.WithProfiler(p))
if err := vm.Run(ctx); err != nil { return err }
if err := vm.Close(); err != nil { return err }
metrics := p.Metrics()
```

`WithProfiler` attaches a profiler to an interpreter or pool. Pool members flush local samples on return/close.

## Metrics

Metrics include compilation attempts, native entries/exits, yields, GC activity, and stable labels such as `func`, `ip`, `kind`, `frontend`, and `reason`. Current names are defined in `prof`.

## Hotness

Hot events are function entries and backward-branch reports. Default threshold: `64`.

| Setting | Effect |
|---|---|
| `WithThreshold(0)` | compile on first event |
| `WithThreshold(n > 0)` | compile after `n` events |
| `WithThreshold(n < 0)` | disable JIT |

`WithThreshold` is independent of `WithTick`. `RESUME` and host callbacks count as entries. Backward `BR`, `BR_IF`, and `BR_TABLE` report loop hotness; forward branches `MUST NOT` report hotness.

Back-edge reports occur every eight iterations with rotating phase. Pool compile admission is shared: one build per function, executed by the claiming worker/member and adopted by other members later.

## Cooling

After all entry/loop roots for a function have been attempted, cooling removes further hotness instrumentation/capture while installed native code remains active. A later peer installation can reactivate the function.

## REPL

`.profile` re-executes with exact sampling and reports hot functions, IPs, opcodes, and JIT metrics.

## Maintenance

The agent `MUST` keep sampling, hotness, pool state, and exact-debug sampling independent.

## Related

- `jit-internals.md`
- `benchmarks.md`
- `debugging.md`
