# Profile

Runtime sampling and execution metrics.

`jit-internals.md` owns the native runtime contract and compiler architecture.

## Signals

`WithTick` is the sampling and runtime-coordination interval.

## Sampling

`Run` samples after every `WithTick` instructions (`128` by default). Each sample records function, bytecode IP, and opcode; the tick also polls context, fuel, hooks, and pool state.

Lower ticks increase both density and cost. Debugger and REPL `.profile` use exact instruction sampling.

## API

```go
p := prof.New()
vm := interp.New(prog, interp.WithProfiler(p))
if err := vm.Run(ctx); err != nil { return err }
if err := vm.Close(); err != nil { return err }
metrics := p.Metrics()
```

`WithProfiler` attaches a profiler to an interpreter or pool. `Flush` publishes samples without closing; pool members flush on return/close.

## Metrics

Metrics include execution samples, opcode/function/IP sample counts, custom metrics, and GC activity. Current names are defined in `prof`.

An interpreter built with `interp.WithThreshold` and `WithProfiler` also reports native execution: `vm_jit_compiles_total{tier,outcome=ok|unsupported|failed}`, `vm_jit_entries_total{tier}`, `vm_jit_exits_total{kind}`. See `jit-internals.md` Exits for exit kinds.

## REPL

`.profile` re-executes with exact sampling and reports hot functions, IPs, and opcodes.

## Separation

Sampling, pool state, and exact-debug sampling are independent concerns.

## Related

- `jit-internals.md`
- `benchmarks.md`
- `debugging.md`
