# Profile

Runtime sampling and execution metrics.

`jit-internals.md` owns the native runtime contract and compiler architecture.

## Signals

- `WithTick` samples execution and performs runtime coordination.

## Sampling

`interp.Run` samples after every `WithTick` instructions; the default is `128`.

Samples record function, bytecode IP, and opcode. The tick path also polls context, fuel, hooks, and pool state; idle paths skip the extra work.

Lower ticks increase sampling density and cost. Debugger and REPL `.profile` use exact instruction sampling.

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

## Maintenance

The agent `MUST` keep sampling, pool state, and exact-debug sampling independent.

## Related

- `jit-internals.md`
- `benchmarks.md`
- `debugging.md`
