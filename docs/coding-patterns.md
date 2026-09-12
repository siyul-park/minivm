# Go Coding Patterns

Normative specification for all production and test code in `minivm`.
A changed file that violates this document is incomplete even when tests pass.

## 1. How to Apply This Document

Read the rules for the affected layer before editing. `AGENTS.md` defines repository workflow; this document defines code and test design. More-specific path instructions override this document only when they explicitly define a narrower rule. Direct user/system/developer instructions take precedence over repository files.

When a rule is unclear, inspect the owning package and its current contract before adding a new pattern. Repetition in nearby code is not evidence that an exception is valid.

Every non-trivial change must complete three reviews before it is done:

1. **Top-down:** package responsibility → public contract → control flow → state/lifecycle → mechanics.
2. **Bottom-up:** every changed or nearby affected symbol → ownership → visibility → naming → necessity.
3. **Simplification:** remove or merge symbols, narrow ownership, simplify control flow, then re-check tests and docs.

A rejected simplification needs a concrete reason: invariant, compatibility requirement, or measured cost.

## 2. Core Design

Code is the specification. A reader should see what happens, who owns it, and where control moves without reconstructing the design from mechanics.

- One function, one abstraction level.
- One behavior, one implementation.
- Put each rule in the narrowest package and type that owns it.
- Keep related state and behavior together.
- Keep the exported API to the smallest complete contract.
- Prefer direct, conventional code over clever or speculative structure.
- Preserve interpreter/JIT semantic parity even when implementations differ.
- Do not add structure merely for symmetry, future flexibility, or shorter functions.
- Performance changes require reproducible evidence.

Do not duplicate one rule across bytecode metadata, SSA, generator tables, and backend code when an existing owner can express it. Separate representations may encode a rule differently, but they must not define it independently.

## 3. Functions

A function should read as a short behavior description. It must not mix unrelated levels such as bytecode decoding with optimization policy or register allocation with JIT orchestration.

Extract a helper only when it:

- removes real duplication;
- gives reusable behavior a meaningful name;
- isolates mechanics needed to keep one abstraction level; or
- is required as a function value.

A private helper should normally have two real callers. A single-use helper is inlined unless it names a real policy or mechanic whose extraction clarifies the design.

Use methods when behavior belongs to a receiver; use package functions for constructors and behavior with no natural receiver. Do not keep receiver-owned behavior in package helpers.

Declare callers before callees. Read downward from public behavior to lower-level mechanics. Keep related declarations adjacent and in call order rather than alphabetically.

## 4. Naming

Names describe roles and contracts, not implementation steps.

- Prefer one word.
- Add words only when context cannot distinguish the concept.
- Use one canonical term for one concept across packages.
- Do not repeat package, receiver, phase, or representation without a real need.
- Keep standard terms and established abbreviations such as `ID`, `IP`, `ABI`, `JIT`, `VM`, `SSA`, `CFG`, `GVN`, and `DCE`.
- One-letter names are for conventional receivers, indexes, and tiny scopes.

Predicates follow this vocabulary:

| Form | Meaning |
|---|---|
| `HasX` | contains or is registered with X |
| `IsX` | state predicate |
| `MatchX` | comparison or validation against X |
| `X` | direct boolean value |

Use `At` when the predicate is evaluated at a supplied position or time. Reserve verbs such as `Build`, `Compile`, `Publish`, `Capture`, and `Use` for actions or transitions, not simple lookups.

Injected capabilities use singular role names. Collections, registries, and stores use plural names.

## 5. Types and APIs

Every exported symbol is a maintenance commitment.

- Accept interfaces only when callers supply behavior.
- Return concrete types from constructors.
- Define interfaces where behavior is consumed.
- Keep exported structs small and intentional.
- Prefer immutable values and defensive copies at ownership boundaries.
- Do not export writable aggregate fields without an explicit ownership contract.
- Do not add `Request`, `Response`, `Result`, `Data`, `Info`, or `Context` containers only to group parameters.
- Do not expose speculative options, algorithms, extension points, or policy knobs.
- Do not add aliases or pass-through wrappers without a distinct contract.

Constructors require only values with no safe default that are necessary for primary behavior. Optional choices use functional options when that is clearer than direct arguments. Validate required shape and dependencies at construction time; validate complete builders at `Build`.

A type owns its invariants and transitions. Workflow operations such as compile, publish, install, reset, retain, release, and close are methods rather than field assignments.

An exported type's private state remains behind its public boundary. Another type must not construct its private fields directly. Use its public constructor or behavior.

## 6. Package Ownership

| Package | Owns |
|---|---|
| `instr` | opcode vocabulary, widths, encoding, metadata |
| `types` | VM values, kinds, value invariants |
| `program` | bytecode, builders, verification boundary |
| `interp` | runtime state, threaded execution, host calls, JIT installation |
| `internal/codegen` | generated threaded handlers and fusion selection |
| `internal/ssa` | SSA IR and verification |
| `internal/ssa/transform` | individual SSA transformations |
| `internal/asm` | machine IR, allocation, linking, executable memory |
| `internal/asm/<arch>` | ISA encoding and ABI details |
| `internal/jit` | architecture-neutral JIT plans, frontend/backend seam, runtime layout |
| `internal/jit/frontend` | bytecode/trace → SSA |
| `internal/jit/backend` | SSA → machine orchestration and deopt metadata |
| `internal/jit/<arch>` | native lowering |
| `internal/journal` | native/interpreter frame-journal ABI |
| `internal/jit/compile` | compilation queue and published code store |
| `internal/jit/tier` | JIT tiering and retirement policy |
| `prof` | profiling and aggregation |
| `analysis` | reusable read-only facts |
| `transform` | transformation policies |
| `optimize` | optimization composition |
| `pass` | pass lifecycle and analysis cache |
| `debug` | debugging policy |
| `cli` | command parsing and presentation |

Place behavior by its dominant rule, not by convenience of imports. Prefer extending an existing owner over adding a coordinator. Generic `Service`, `Manager`, `Util`, and `Helper` abstractions are forbidden unless an existing standard term accurately names the owner.

## 7. Runtime and JIT Boundaries

`program.Verify` is the untrusted bytecode boundary. It must not depend on interpreter state or optimization policy.

`interp.Interpreter` owns execution state and is single-goroutine-owned during active execution. Threaded execution is the semantic baseline.

The JIT is an optimization over that baseline:

- architecture-neutral policy stays out of architecture packages;
- unsupported lowering returns `false` without partial state mutation;
- speculative guards deopt before behavior the native path cannot own;
- native publication preserves immutable code and interpreter-local dispatch ownership;
- fallback reconstructs the exact interpreter state required at the resume point;
- observable parity is tested through public behavior.

`internal/asm` owns allocation, linking, and executable memory. Architecture packages own concrete encoding and ABI mechanics. Executable memory is written privately, sealed executable, then published; published code stays immutable.

## 8. Values, Ownership, and Errors

Prefer values when they carry meaningful invariants. Mutable backing storage must not escape an ownership boundary.

Reference ownership must be explicit. Borrowed values must not cross a boundary that requires ownership. Retain/release transitions belong to the owner of the transition.

Errors are part of the contract:

- semantic package errors use stable `ErrXxx` sentinels;
- dependency errors remain identifiable when callers depend on them;
- only the current boundary may translate an error category;
- `%w` is used when added context preserves identity;
- do not create semantic categories with `fmt.Errorf` alone;
- do not leak private state or sensitive process values through messages.

Panic is reserved for impossible programmer errors, `Must*` APIs, and documented hot-path invariants with a single recovery boundary. Normal runtime failures return errors.

## 9. Concurrency and Lifecycle

Contexts are first parameters of operations that may block, perform I/O, or cross process boundaries. Never store a request context in a long-lived object.

Shared mutable state has one clear owner and synchronization strategy. Long-lived goroutines have an explicit shutdown path. Native memory, heap references, executable buffers, and queue claims are released exactly once according to their owner contract.

Race tests are required when changing pools, caches, profilers, compilation publication, or shared lifecycle state.

## 10. File and Declaration Order

Within a Go file:

1. public types;
2. private types;
3. public constants;
4. private constants;
5. variables;
6. `init`;
7. public options/functions;
8. public constructors;
9. public methods;
10. clone/conversion and interface hooks;
11. private functions and methods.

Keep a type and its methods together. Split a large type only along a real concern. JIT-neutral code belongs in `internal/jit`; architecture lowering belongs in `internal/jit/<arch>`.

Struct fields should read from ownership and policy to runtime state. Keep synchronization fields last.

## 11. Comments

Comments are exceptions, not a second specification.

Keep only facts the code cannot express:

- an invariant and what breaks without it;
- an external or cross-package constraint;
- a rejected alternative with measured cost or a concrete defect;
- an external contract or specification reference.

Do not narrate statements, restate symbol names, label `arrange/act/assert`, or explain obvious control flow. Improve names, types, structure, or ownership instead.

Exported Go symbols still need normal doc comments stating their contract.

## 12. Tests

Tests are executable specifications and use the production package plus `_test`.

- Tests live beside the production owner.
- Each test file belongs to the production file owning the symbol; do not create catch-all concept files.
- One top-level test owns one independent exported contract; cases are subtests.
- Test names state behavior, not implementation steps.
- Arrange, execute, and assert remain visible.
- Sibling subtests own independent mutable fixtures.
- Use real in-memory behavior by default.
- Use `require`, not `assert`, and no assertion message strings.
- Do not access private symbols or representation.
- Do not add production APIs or proxies solely for tests.

### Test-first

Write the test before implementation and observe the expected failure. For tests added after existing code, deliberately mutate a load-bearing line and confirm the test fails. Restore the mutation before final validation.

### JIT and backend tests

Frontend tests prove plan/SSA contracts and `ssa.Verify`. Backend tests prove layout, register bindings, moves, metadata, and bridge/deopt points. ARM64 tests specify exact emitted instruction streams with goldens. Interpreter tests prove threaded/JIT parity through public results, errors, ownership effects, and native execution.

A backend golden is written before the backend is made to match it. Do not derive expected output from the current emitter.

## 13. Generated and Architecture-Specific Code

Generated files are changed only through their generator. Run `make generate` and `make check-generated`.

Portable behavior stays in shared files. Architecture-specific behavior stays behind matching build tags and has a corresponding native test path.

A native backend is specified by the instruction stream it emits, not only by end-to-end behavior.

## 14. Performance

Performance work follows this order:

1. define workload and comparison;
2. capture baseline;
3. identify the real hot path;
4. change the narrowest owning layer;
5. rerun correctness, race, and parity checks;
6. report reproducible before/after results.

Benchmarks keep setup, reset, cleanup, and expected-result computation outside the timer unless they are the measured operation. Warm JIT benchmarks must prove native installation and must not time threaded fallback as native throughput.

## 15. Documentation

Documentation is part of the contract. Each topic has one owner; other documents summarize and link.

Long-lived documents describe the final supported state. They must not contain migration narratives, implementation chronology, obsolete alternatives, progress logs, or superseded terminology. Put history only in explicitly historical audits or dated plans.

Update the owning document when behavior changes, then remove stale duplicates.

| Topic | Owner |
|---|---|
| package boundaries and invariants | `docs/architecture.md` |
| opcode semantics and JIT status | `docs/instruction-set.md` |
| value representation | `docs/value-representation.md` |
| memory ownership | `docs/memory-model.md` |
| JIT and assembler contracts | `docs/jit-internals.md` |
| test contracts | `docs/testing.md` |
| benchmarks | `docs/benchmarks.md` |
| platform support | `docs/compatibility.md` |

Keep docs short, factual, current, and directly useful to the next engineer.

## 16. Completion

A change is complete only when:

- ownership, boundaries, and names are clear;
- top-down and bottom-up reviews are complete;
- another simplification pass found no safe improvement;
- no behavior has a second implementation;
- tests constrain the intended contract and mutation evidence exists when required;
- comments carry only non-obvious facts;
- generated and architecture-specific output is synchronized;
- docs describe the final state without duplicates or history;
- relevant tests and static checks pass;
- performance claims have evidence;
- unrelated changes remain untouched.
