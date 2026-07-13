# Task 3 report: Ranked profile report and documentation

Status: DONE

## Files

- `cli/profile.go`: normalizes flat profiler metrics into a ranked report model and renders deterministic sample, JIT entry, exit, and miss sections.
- `cli/profile_test.go`: covers repeated labeled rows, deterministic ties, percentages, top-10 limits, interpreted/not-attempted, compile-empty, emitted-unused, and yield exclusion.
- `cli/repl.go`: removes the old direct flat metric renderer; `.profile` execution and side-effect behavior are unchanged.
- `cli/repl_test.go`: updates empty/simple report expectations and retains the history/constants/types side-effect assertions.
- `docs/profile.md`: documents aggregate and detailed metric schemas, ranked report sections, percentages, and lifecycle statuses.
- `docs/jit-internals.md`: documents `journalExitID`, immutable exit descriptors, stable runtime counters, and cache-winner accounting.

## TDD evidence

### RED 1: normalized ranked report

Command:

```text
go test ./cli -run '^TestPrintProfile$/normalizes_and_ranks_lifecycle_rows$' -count=1
```

Expected failure observed: the old renderer printed `functions`, per-function IPs, and `opcodes` directly, retained opcode input order, and omitted every detailed JIT section. The literal expected report differed at `hot functions`, `hot ips`, opcode tie order, `jit summary`, `jit entries`, `jit exit reasons`, and `jit misses`.

GREEN:

```text
ok github.com/siyul-park/minivm/cli 0.456s
```

### RED 2: compile-empty union

Command:

```text
go test ./cli -run '^TestPrintProfile$/keeps_compile_empty_and_zero_denominator_exits_visible$' -count=1
```

Expected failure observed: the compile miss existed, but the normalized entry model omitted the required `compile-empty` row:

```text
does not contain "4\t0007\tnone\tstatic\tcompile-empty\t1\t0\t0\t0"
```

GREEN:

```text
ok github.com/siyul-park/minivm/cli 0.446s
```

The same slice also verifies that an exit with no matching native entries prints `-` and that a yield count of `99` is absent from the report.

## Verification

Focused deterministic report run:

```text
go test ./cli -run '^TestPrintProfile$' -count=20
ok github.com/siyul-park/minivm/cli 0.231s
```

Required CLI and command packages:

```text
go test ./cli/... ./cmd/minivm -count=1
ok github.com/siyul-park/minivm/cli 0.419s
?  github.com/siyul-park/minivm/cmd/minivm [no test files]
```

Final package set:

```text
go test ./asm/... ./prof ./interp ./cli/... ./cmd/minivm -count=1
```

All packages passed.

Race verification:

```text
go test -race ./prof ./interp ./cli/... ./cmd/minivm -count=1
```

All packages passed.

Additional checks:

```text
go vet ./cli/... ./cmd/minivm
git diff --check
```

Both passed.

## Benchmark comparison

`make benchmark` was rerun through the comparable baseline cases. The remaining long benchmark run was stopped after the task coordinator confirmed it did not need to complete.

| Benchmark | Baseline | Current | Difference |
|---|---:|---:|---:|
| `Fib20AllocJITWarmup` | 237472 ns/op, 390931 B/op, 3769 allocs/op | 239933 ns/op, 392165 B/op, 3787 allocs/op | +1.0% time, +0.3% bytes, +0.5% allocs |
| `Fib35AllocJITSteady` | 47659488 ns/op, 0 B/op, 0 allocs/op | 47020945 ns/op, 0 B/op, 0 allocs/op | -1.3% time, allocation-neutral |
| `JITIssue101/interp` | 1477 ns/op, 0 B/op, 0 allocs/op | 1463 ns/op, 0 B/op, 0 allocs/op | -0.9% time, allocation-neutral |

The differences are within normal benchmark noise and the reporting change is outside runtime execution loops.

## Completion-gate self-review

- Re-read every touched code, test, and documentation file against `docs/coding-patterns.md` sections 0, 1, 2, 6, 7, and 8.
- The report model retains exact metric keys, aggregates repeated exact rows, and sorts every ranked collection by count plus its full deterministic key.
- Functions, each displayed function's IPs, opcodes, entries, exits, and misses are independently capped at 10.
- Function/opcode, IP, and matching-entry exit denominators are explicit; zero denominators render `-`.
- Samples without lifecycle rows become `interpreted/not-attempted`; only recorded capture or compile failures become misses.
- The old renderer and now-unused helpers were removed rather than retained beside the new model.
- Declaration order is private types, private constants, private methods, then caller-before-callee private functions.
- A second simplification pass found no removable symbol or simpler control flow. Splitting normalization further would add coordination without removing complexity, so the cohesive report model remains in one file.
- Broad local review found no Critical or Important spec or standards issues.

## Concerns

None.

## Review follow-up

The final review fixes preserve distinct compile lifecycle rows even when an
entry with the same function, IP, and frontend was emitted earlier. A
non-successful compile now always contributes its `compile-empty` or
`compile-rejected` placeholder and miss row; a successful `emitted/none`
compile does not add a redundant `compile-emitted` placeholder.

The focused regression was verified against both defects:

- restoring the old anchor/frontend suppression removed the `compile-empty`
  row while leaving the emitted entry, and the regression failed
- adding placeholders for every compile outcome exposed a spurious
  `compile-emitted` row, and the regression failed
- the final non-success-only condition passes the same regression

Profile rendering is owned by `profileReport.print`, and its output coverage
lives under `TestREPL_Run` with output assertions rather than report-field
assertions. The `.profile` side-effect case now runs a program with non-empty
history, constants, and types, executes more bytecode after profiling, and uses
`.show` plus stack output to verify all three remain usable.

The lifecycle documentation now states that attributable fallbacks use their
concrete opcode while synthetic boundaries such as an `opLimit` trace cut use
`none`.

Review verification:

```text
go test ./cli -run '^TestREPL_Run$' -count=20
go test ./cli/... ./cmd/minivm -count=1
go test ./asm/... ./prof ./interp ./cli/... ./cmd/minivm -count=1
go test -race ./prof ./interp ./cli/... ./cmd/minivm -count=1
go vet ./...
git diff --check
```

All commands passed. A completion-gate reread found no remaining removable
symbols or safe control-flow simplification in the touched code.

### Final style follow-up

`newProfileReport` now appears in the private-constructor section before the
private `profileReport` and `jitProfile` methods. The remaining private
functions follow those methods, preserving `docs/coding-patterns.md` section
2.4 and caller-before-callee order without changing behavior.

Verification:

```text
go test ./cli -run '^TestREPL_Run$' -count=1
git diff --check
```

Both commands passed.

## Final whole-branch fix pass

The final pass aligned the REPL report with issue #130's required columns:

- hot functions include native entries, exits, and exit percentage
- hot IPs include native kind, emits, entries, and exits
- the JIT summary uses aggregate attempts, emits, errors, bytes, native
  entries, native exits, and native yields
- JIT entries and exit reasons expose their documented denominators
- misses are normalized to `func,ip,phase,reason,count`
- yields remain visible in the summary and never contribute to exit percentage

TDD started with `go test ./cli -run TestREPL_Run`, which failed on the missing
`hot functions (top 10)` contract. The same public REPL test passed after the
minimal report change. Ranking and top-10 behavior now run through real REPL
programs; arbitrary lifecycle aggregation remains covered through public
`Collector`, `Profiler`, `Interpreter`, and `Pool` behavior.

The standards pass removed direct private-renderer tests, branch-added private
cache/tracer/compiler-result assertions, and the stale `TestCache_Due` owner.
The lost-rearm scenario now runs under public Pool contention and is asserted
through profiler lifecycle metrics. ARM64 exit descriptor assertions remain in
`TestCompiler_Compile` because descriptor ID, reason, and opcode are the
protected generated-code/journal ABI contract.

Declaration order now keeps private types before constants, constructors before
methods, methods before functions, and callers before callees. Entry-kind
conversion and compile-result preference are methods on their owning types.
A second simplification pass removed unused helpers, redundant yield maps,
repeated native aggregation, and unnecessary report scans.

Final verification:

```text
go test ./...
go test -race ./prof ./interp ./cli/... ./cmd/minivm
go vet ./...
make build
git diff --check
```

All commands passed.
