# Task 2 report: Interpreter JIT profiling lifecycle

Status: DONE_WITH_CONCERNS

## Files

- `interp/interp.go`: preserves explicit profiler intent, records typed compile/emission events, installs interpreter-local counter handles, and counts native entries, fallbacks, and yields without changing aggregate counters.
- `interp/trace.go`: returns typed capture results and records bounded rejection/publication reasons while suppressing cache-hit events.
- `interp/jit.go`: returns typed compiler results, preserves frontend ordering, classifies lowering/build/link outcomes, stores per-entry frontend/bytes, and owns immutable exit descriptors plus `journalExitID`.
- `interp/jit_arm64.go`: assigns stable exit IDs at fallback creation sites, writes ID+1 before fallback traps, and classifies guards, cold branches, trace cuts, terminal operations, and loop exits.
- `interp/cache.go`: preserves winner/peer accounting and carries hot versus side-exit trigger identity across cache rearming.
- `interp/interp_test.go`, `interp/trace_test.go`, `interp/jit_arm64_test.go`: cover explicit profiling, typed capture/compiler results, native entry/terminal fallback metrics, and existing lifecycle/native behavior.

## TDD evidence

### RED: typed capture and explicit profiling

Command:

```text
go test ./interp -run '^TestTracer_Capture$/reports_one_published_attempt_only_when_profiling_is_explicit$' -count=1
```

Expected failure observed: build failed because `capture` still returned `*trace` and exposed no typed `trace`/`outcome` result fields.

Representative output:

```text
interp/trace_test.go:49:28: result.trace undefined
interp/trace_test.go:50:57: result.outcome undefined
FAIL github.com/siyul-park/minivm/interp [build failed]
```

GREEN:

```text
go test ./interp -run '^TestTracer_Capture$/reports_one_published_attempt_only_when_profiling_is_explicit$' -count=1
```

Result: passed.

### RED: typed compiler result

Command:

```text
go test ./interp -run '^TestCompiler_Compile$/reports_missing_input$' -count=1
```

Expected failure observed:

```text
interp/jit_arm64_test.go:243:13: assignment mismatch: 1 variable but (&compiler{}).Compile returns 2 values
FAIL github.com/siyul-park/minivm/interp [build failed]
```

GREEN:

```text
go test ./interp -run '^TestCompiler_Compile$/reports_missing_input$' -count=1
```

Result: passed.

### Runtime lifecycle checks

Commands:

```text
go test ./interp -run '^TestWithProfiler$/records_compilation_and_native_entry$' -count=1
go test ./interp -run '^TestWithProfiler$/records_terminal_native_fallback$' -count=1
go test ./interp -run '^TestTracer_Capture$' -count=1
```

Results: passed after correcting the native-entry expectation to include both native invocations made by the test.

## Verification

```text
go test ./prof ./interp -count=1
go test ./interp -count=1
go test -race ./interp -count=1
go test -race ./interp -run '^(TestWithProfiler|TestTracer_Capture|TestPool|TestCache)' -count=1
GOARCH=amd64 go test ./interp -run '^(TestWithProfiler|TestTracer_Capture)$' -count=1
go vet ./interp
git diff --check
```

All commands passed. The final post-edit `go test ./interp -count=1` and focused race command also passed.

## Self-review

- Aggregate `vm_jit_attempts_total`, `vm_jit_emits_total`, `vm_jit_errors_total`, and `vm_jit_bytes_total` paths remain present and are updated at the same winner/solo ownership points.
- Detailed capture/compile/emission updates require `WithProfiler`; `WithLocal` alone does not enable them.
- Shared-cache peers only install local runtime handles; the cache winner alone records compile/emission.
- Native runtime increments use pre-registered `prof.Counter` handles and do no label construction.
- `trapYield` increments yield only; `trapOverflow` increments neither exit nor yield; fallback traps resolve an explicit immutable descriptor.
- A simplification pass removed an unused loop-root parameter and kept mapping helpers local to interpreter ownership.
- No Task 3 CLI or documentation files were changed.

## Concerns

- The identity machinery and representative native entry/terminal fallback paths are directly tested, and the full existing ARM64/race suites pass. New dedicated assertions were not added for every requested guard category, loop exit, yield, and overflow metric row; those paths share the tested descriptor/handle plumbing but retain some coverage reliance on the existing native behavior suite.
- CodeGraph was not initialized in this worktree. Its MCP response explicitly warned that the available index belonged to the main worktree, so structural exploration used focused local reads after recording that limitation.
