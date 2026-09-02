# Go Coding Patterns

Normative coding specification for all Go production and test code in minivm.
It defines function shape, naming, type design, package ownership, public APIs,
file layout, errors, concurrency, tests, generated code, performance work, Git,
and documentation.

Non-compliance in a changed file is a blocking defect, not a style preference.

## Normative Language

This specification uses **MUST**, **MUST NOT**, **REQUIRED**, **SHOULD**,
**SHOULD NOT**, **RECOMMENDED**, **MAY**, and **OPTIONAL** as described by
[RFC 2119](https://www.rfc-editor.org/rfc/rfc2119) and
[RFC 8174](https://www.rfc-editor.org/rfc/rfc8174) when, and only when, they
appear in uppercase.

Changes to this specification MUST update the routing rules in `AGENTS.md` and
`.claude/CLAUDE.md` in the same change.

## 1. Scope and Precedence

### 1.1 Precedence

Apply the first rule that resolves a question:

1. Nearby code that already applies a more specific pattern consistently.
2. This specification.
3. General Go convention.

Nearby code does not override this specification merely because a violation is
repeated. A deliberate exception MUST be documented in the change summary with
its reason and removal condition.

### 1.2 Requirement Levels

| Term | Meaning |
|---|---|
| MUST, MUST NOT | Required. |
| SHOULD, SHOULD NOT | Default; deviation requires a stated reason. |
| MAY | Allowed at the author's discretion. |

### 1.3 Fast Path

Read only the sections touched by a change, but every code or test change MUST
apply §2 and §16.

| Change | Sections |
|---|---|
| Functions, helpers, or names | 2, 3, 4 |
| Types, fields, constructors, or public APIs | 2, 4, 5 |
| Package boundaries or domain ownership | 6, 7 |
| Interpreter, JIT, assembler, or generated handlers | 7, 8, 13, 14 |
| Declaration or file order | 3, 9 |
| Errors or panic/recover | 10 |
| Concurrency or lifecycle | 11 |
| Tests or benchmarks | 12, 14 |
| Commits or documentation | 15 |

## 2. Core Principles

1. Code MUST read as a behavior specification: a reader identifies what
   happens and which object owns it without simulating mechanics.
2. Each function MUST hold one abstraction level.
3. Behavior MUST live in the package and type that own its rule.
4. Related state and behavior MUST stay together.
5. Exported surface MUST be the smallest complete API.
6. Structure MUST be added only when it removes real complexity.
7. Explicit, readable behavior takes precedence over clever mechanics.
8. Established conventions MUST be preserved unless the change intentionally
   replaces them.
9. Interpreter and JIT behavior MUST remain semantically symmetric, while their
   implementations MAY differ where the execution model requires it.
10. Performance claims MUST be supported by reproducible benchmark evidence.

### 2.1 Top-Down Design Review

Every non-trivial change MUST review, from the public entry point downward:

1. package responsibility and dependency direction;
2. public contract and ownership boundary;
3. primary behavior and control flow;
4. state and lifecycle ownership;
5. lower-level mechanics.

A package, type, or abstraction whose responsibility cannot be stated in one
precise sentence SHOULD be split, merged, narrowed, or removed.

### 2.2 Bottom-Up Symbol Review

Every changed file, and every nearby symbol exposed by the change, MUST be
reviewed from leaves upward. Each file, type, interface, field, function,
method, parameter, result, constant, variable, and test helper MUST have a
current reason to exist.

For each symbol, reviewers MUST ask whether it can be:

- removed;
- inlined;
- merged with an existing owner;
- narrowed in scope or made private;
- renamed by role;
- represented by an existing type or operation; or
- replaced by simpler direct code or a simpler algorithm.

A refactor is incomplete while it leaves dead fields, arguments, results,
wrappers, aliases, compatibility shims, or one-call indirections made obsolete
by the change. Future flexibility, superficial symmetry, shorter functions, and
one-call-site convenience are not sufficient reasons for a symbol to exist.

### 2.3 Simplification Loop

Simplification MUST continue until another pass finds no safe improvement. Each
pass checks, in order:

1. removable or mergeable symbols;
2. narrower ownership and visibility;
3. simpler control flow;
4. simpler or more efficient algorithms;
5. tests and docs matching the final contract.

Intentionally rejected simplifications MUST be recorded in the change summary
with the invariant, compatibility constraint, or measured cost that prevented
them.

## 3. Functions

### 3.1 Abstraction Level

A function MUST NOT mix unrelated levels, including:

- CLI parsing with VM state mutation;
- bytecode decoding with optimization policy;
- register-allocation mechanics with JIT orchestration;
- profiling aggregation with execution control.

The main flow MUST remain visible. Mechanics MAY move behind a behavior-level
name when doing so makes that flow easier to read.

### 3.2 Declaration Order

Callers MUST be declared before callees. Reading downward MUST reveal
progressively more detail.

```text
Run
  prepare frame
  execute code
  collect result

execute
  dispatch opcode
  handle fallback
```

A file MUST form a descending staircase of abstraction:

- symbols at the same level MUST be adjacent and in call order;
- a callee MUST be as close to its caller as its other callers allow;
- a shared callee MUST follow the last caller that introduces it;
- a lower level MUST NOT be interleaved between higher-level symbols; and
- error constructors, formatters, and comparable leaves MUST be last.

### 3.3 Helper Extraction

A helper MAY be extracted only when it:

1. removes real duplication;
2. gives reusable behavior a meaningful name;
3. isolates mechanics required to keep one abstraction level; or
4. is required as a function value by composition.

A private helper SHOULD have at least two real callers. A single-use helper MUST
be inlined unless inlining would mix abstraction levels. A surviving single-use
helper MUST name a policy or mechanic, never one sequential step in its caller.

A helper MUST NOT exist only to shorten a function, delegate one call, translate
one obvious error, hide one branch, or label a step.

A helper MUST belong to the package and type that own its behavior. Collection,
validation, encoding, or comparison rules over another package's types MUST NOT
be reimplemented when that package can own the operation.

### 3.4 Methods and Functions

- Use a method when behavior belongs to one receiver.
- Use a package function for constructors, behavior shared by unrelated types,
  or behavior with no natural receiver.
- Receiver-owned behavior MUST NOT remain a package helper.
- A method MUST NOT be added merely to shorten a call.
- Constructors MUST be package functions, never methods.

A private package function used by one type MUST become a method or be inlined,
unless it is a constructor or is materially clearer as a package-level
mechanic.

## 4. Naming

### 4.1 General

- Prefer one-word names.
- Add a word only when package, receiver, or local context cannot distinguish
  the concept.
- Use one canonical term per concept across packages.
- Names MUST NOT repeat the receiver, package, subsystem, phase, or
  representation without a real disambiguation need.
- Abbreviations MUST be established project or domain terms such as `ID`, `IP`,
  `ABI`, `JIT`, `VM`, `CPU`, `CFG`, `GVN`, and `DCE`.
- One-letter names MUST be limited to conventional indexes, receivers, and very
  small scopes.
- Protocol, ISA, opcode, ABI, and standard-library terms MUST retain their exact
  spelling.

Names describe caller-visible roles, not mechanics. `Run`, `Build`, `Lower`,
`Encode`, `Relax`, `Capture`, and `Publish` name behavior; names such as
`appendInstructionAndUpdateLength` expose steps instead of roles.

### 4.2 Predicates and Queries

| Form | Use |
|---|---|
| `Has<Field>` | membership or registered-value existence |
| `Is<State>` | state predicate |
| `Match<Field>` | equality or verifier check |
| `<Field>` | direct boolean value |

Append `At` when a predicate is evaluated at a supplied position or time. Domain
verbs such as `Build`, `Compile`, `Capture`, `Publish`, `Relax`, and `Use` are
reserved for computations and transitions, not plain lookups.

### 4.3 Capabilities and Collections

Injected capabilities take singular role names. Collections, registries, and
stores take plural names. Qualify a name only when another capability in the
same scope would collide.

## 5. Types and APIs

### 5.1 Public Surface

Every exported symbol is a maintenance commitment.

- Accept interfaces when callers provide behavior.
- Return concrete types from constructors.
- Define interfaces in the package that consumes the behavior.
- Keep public structs small and intentional.
- Exported writable aggregate fields SHOULD NOT be added.
- Aliases, wrappers, or pass-through methods MUST have a distinct contract.
- `Request`, `Response`, `Result`, `Data`, `Info`, or `Context` containers MUST
  NOT be added only to group parameters or returns.
- Speculative options, algorithms, extension points, and policy knobs MUST NOT
  be exported.
- A type MUST own an invariant, policy, transition, or stable capability.

Public APIs outside the scope explicitly authorized by the user MUST remain
source compatible. Compatibility MUST NOT preserve an internal symbol that has
no external contract.

### 5.2 Constructors and Options

A constructor MUST require only values that:

1. have no safe default;
2. cannot be created internally or supplied later; and
3. are required for primary behavior.

Optional construction choices MUST use functional options. Required options
MUST NOT replace clearer direct arguments. Defaults MUST be applied before
options.

Constructors validate immediately required dependencies, immutable shape, and
identity. Builders validate the complete value at `Build` or `MustBuild`.
Runtime execution MUST NOT be the first validation boundary for malformed
builder state.

Options MUST pass through the constructor or builder that owns their contract.
When a type already exposes the complete replacement transition, an option MUST
call that transition rather than duplicate it.

Declaration order is:

```text
WithX ...
MustNew or MustBuild
New or Build
```

`MustNew` MUST precede `New`; `MustBuild` MUST immediately precede `Build` when
it delegates to it.

### 5.3 Encapsulation

Every exported struct is an ownership boundary, including within its package.
A method on one exported type MUST NOT access another exported type's unexported
state or construct a literal naming that state. It MUST use the smallest public
constructor, behavior, getter, setter, or conversion preserving the callee's
invariants.

Private constructors, options, encoding hooks, and implementation functions
belonging to a type MAY access its private state. Unexported collaborators that
form one internal implementation MAY access each other; public proxies MUST NOT
be added only to separate them.

Getters MUST return defensive copies of mutable slices, maps, bytes, keys, code,
and buffers unless the API explicitly transfers ownership. Setters MUST own the
complete replacement transition and preserve all invariants. Workflow changes
such as compile, publish, install, reset, retain, release, and close MUST be
behavior methods, not field assignment.

### 5.4 Values and Representations

- Prefer values when they protect meaningful invariants.
- Normalize sets by immutable identity.
- Reject duplicate identities before mutating state.
- Store and return sets in deterministic order.
- Custom text, binary, and JSON input/output SHOULD be symmetric unless a
  current wire contract requires otherwise.
- Mutable builders MUST NOT leak mutable backing storage into built values.
- `Clone` MUST preserve hidden execution and ownership state required by the
  public contract.

## 6. Package and Domain Ownership

A package owns its vocabulary, invariants, policies, and state transitions.
Behavior MUST be placed by its dominant rule, not by how many packages it
references.

| Package role | Owns |
|---|---|
| `instr` | opcode vocabulary, widths, encoding, parsing, metadata |
| `types` | VM values, type invariants, value comparison and tracing |
| `program` | bytecode aggregates, builders, labels, verification boundary |
| `interp` | VM execution state, threaded dispatch, fallback, JIT orchestration |
| `internal/asm` | architecture-neutral assembly IR, allocation, linking, executable buffers |
| `internal/asm/<arch>` | architecture encoding, ABI, registers, relaxation |
| `internal/codegen` | fusion pattern catalog, pattern validation, threaded-handler emission |
| `prof` | samples, counters, profile aggregation |
| `pass` | generic pass lifecycle and analysis cache |
| `analysis` | reusable facts without mutation |
| `transform` | one transformation policy per pass |
| `optimize` | optimization pipeline composition |
| `debug` | bytecode debugging policy |
| `cli` | command parsing, application coordination, display |

Prefer extending an existing cohesive aggregate or value before adding an
object. When a package outgrows one responsibility, split it into a sibling
package, not a nested implementation hierarchy.

A coordinator MAY be introduced only when it coordinates multiple owners,
stable dependencies would otherwise repeat, no one aggregate can honestly own
the operation, and the coordinator remains small. Generic `Service`, `Manager`,
`Util`, and `Helper` names MUST NOT be introduced. Existing standard terms such
as `pass.Manager` MAY remain when they accurately name a registry and cache
owner.

## 7. Runtime and Compiler Boundaries

### 7.1 Program and Verification

`program.Program` is the bytecode hand-off boundary. Builders own label repair,
pool interning, and final construction invariants. `program.Verify` owns
validation of untrusted bytecode and MUST remain independent of interpreter
state and optimizer policy.

A pass that changes bytecode offsets MUST repair every branch, handler, and
position-sensitive structure or leave the function unchanged.

### 7.2 Interpreter

`interp.Interpreter` owns runtime stacks, frames, heap, globals, threaded code,
and per-instance native installation. One interpreter is single-goroutine-owned
during use. Request contexts MUST NOT be stored beyond the active operation.

Threaded dispatch is the semantic reference. Runtime hot-path traps MAY panic
internally only when `Run` owns the single recovery boundary and returns the
stable error with the exact instruction position.

### 7.3 JIT

The JIT is an optimization over threaded semantics.

- Unsupported lowering MUST leave the exact threaded fallback available.
- A failed speculative lowering MUST NOT partially mutate IR, stack facts,
  labels, or ownership state.
- Architecture-neutral planning MUST remain separate from architecture
  encoding.
- JIT policy MUST NOT leak into `asm` instruction mechanics.
- Native installation and publication MUST preserve immutable code and
  interpreter-local dispatch ownership.
- Interpreter/JIT parity MUST be tested through public observable behavior.

### 7.4 Assembler and Executable Memory

`asm` owns virtual registers, allocation, linking, buffers, and executable
memory lifecycle. Architecture packages own concrete instruction encoding and
ABI details.

Executable buffers MUST own their write/execute transition. Each install MUST
write into unpublished memory, seal that mapping executable, and only then
publish it; published mappings MUST remain immutable and executable while later
installs serialize. Relaxation MUST terminate, and each replacement MUST already
be encodable at its final range. Resource cleanup MUST be explicit and
idempotent where the public contract permits repeated cleanup.

### 7.5 Profiling

`prof` owns recording and aggregation only. It MUST NOT decide JIT policy,
mutate interpreter state, or coordinate compilation. Snapshots returned to
callers MUST be deterministic and MUST NOT expose mutable internal storage.

## 8. Execution Flow and Algorithm Design

Primary flows MUST be composed at one level:

```text
parse -> verify -> optimize -> construct -> run -> observe -> close
trace -> plan -> lower -> build -> publish -> install
```

Mechanics MAY be extracted behind policy names, but shallow wrappers MUST NOT
hide these flows. Prefer:

- one direct pass over coordinated passes;
- local state over global maps;
- exact ownership over cleanup protocols;
- data flow matching the runtime model;
- bounded worklists over recursive traversal in runtime ownership paths; and
- immutable publication over shared mutation.

`release` MUST remain iterative. Heap index zero MUST remain `Null`. Frame code
MUST keep function address and callable reference distinct. Detailed runtime and
JIT invariants remain authoritative in `docs/architecture.md`,
`docs/memory-model.md`, and `docs/jit-internals.md`.

## 9. File Layout

### 9.1 Declaration Groups

Declarations in a `.go` file MUST appear in this order:

1. public types;
2. private types;
3. public constants;
4. private constants;
5. public variables;
6. private variables;
7. `init` functions;
8. public options and functions;
9. public constructors;
10. public methods;
11. clone and explicit conversion methods;
12. standard-library, encoding, database, and protocol hooks;
13. private functions and methods.

Within a group, symbols MUST follow meaning and call flow, never alphabetical
order. Related symbols MUST remain adjacent. Options MAY sit immediately above
the constructor they configure.

A struct and all its methods SHOULD remain in one file. A large type MAY be
split by a real concern, but methods MUST NOT be placed in another type's file
for superficial locality. JIT-neutral orchestration belongs in
`internal/jit`; architecture lowering belongs in `internal/jit/<arch>/`.

### 9.2 Method Order

For a stateless capability, coordinator, adapter, compiler, or encoder:

1. primary behavior;
2. supporting behavior in top-down call order;
3. lower-level private mechanics.

For a stateful entity or value:

1. multi-field behavior near the earliest field it depends on;
2. field groups in struct declaration order;
3. within each group: mutator, predicate or matcher, getter;
4. workflow transitions in natural order;
5. `Clone` and explicit conversions;
6. `String`, marshal/unmarshal, and other interface hooks.

### 9.3 Struct Fields

Fields MUST be ordered by reader understanding:

1. lifecycle and policy objects;
2. infrastructure and shared capabilities;
3. program or domain data;
4. runtime state;
5. mutable counters;
6. read-only configuration;
7. synchronization primitives.

Groups MUST be separated by a blank line. `sync.Mutex` MUST be last. Struct
literals MUST follow declaration order.

## 10. Errors

- Errors MUST NOT be ignored.
- Package-created semantic errors MUST be stable `ErrXxx` sentinels.
- Dependency errors MUST remain unchanged when identity is contractual.
- Only outcomes understood at the current boundary MAY be translated.
- Cancellation, timeout, conflict, corruption, resource exhaustion, and other
  operational failures MUST be preserved.
- `fmt.Errorf` MUST NOT create a new semantic category.
- Errors MUST NOT be wrapped only to repeat the failed operation.
- `%w` MUST be used when added context preserves a dependency or sentinel.
- `errors.Is` and `errors.As` MUST be used only when identity controls behavior.
- Error messages MUST NOT expose host values, private code, pointers, or other
  sensitive process state.

| Form | Meaning |
|---|---|
| `ErrNo*` | required capability is not configured |
| `*Required` | required value is absent |
| `Invalid*`, `*Invalid` | supplied value violates its contract |
| `*Incomplete` | construction or wiring is incomplete |

Sentinel names and messages MUST align. Panic is permitted only for impossible
programmer errors, `Must*` APIs, and documented runtime hot-path invariants with
a single recovery boundary. Other runtime failures MUST return errors.

## 11. Concurrency and Lifecycle

- `context.Context` MUST be the first parameter of operations that may block,
  perform I/O, or cross a process boundary.
- Contexts MUST NOT be stored in long-lived structs.
- Cancellation and timeout ownership MUST be preserved.
- Shared mutable state MUST have one clear owner and synchronization strategy.
- Goroutine lifetimes MUST be tied to an operation or process lifecycle.
- Long-lived goroutines MUST shut down gracefully.
- Immutable data SHOULD be published atomically instead of mutated under
  readers.
- Cleanup methods MUST release native memory and owned references exactly once.
- Race tests MUST cover changes to pools, caches, profilers, publication, or
  shared compilation.

## 12. Tests

### 12.1 Placement and Ownership

- Unit tests live beside production code as `*_test.go`.
- Every test package MUST use the production package name plus the `_test`
  suffix and exercise the package as an importing client.
- Each test file MUST match the production file owning the symbol.
- Catch-all concept files and `test_helpers_test.go` MUST NOT be created.
- Black-box, conformance, and external fixtures belong under `test/` or the
  dedicated `benchmarks/` module.
- Test order MUST match source declaration order.

Write one top-level test per exported constructor, function, or method with an
independent contract. Cases MUST be subtests; sibling top-level tests per case
MUST NOT be created. Setter and getter behavior MUST be tested together under
the setter owner.

### 12.2 Public Contract Only

Tests are executable specifications. Public-contract tests MUST construct
exported types through exported constructors, builders, or options and verify
behavior through exported functions and methods or an observable boundary.

Tests MUST NOT:

- construct literals naming unexported fields;
- read or write unexported state;
- call unexported functions or methods;
- assert private representation;
- add production proxies solely for testing; or
- proxy a real implementation merely to inject failure.

If behavior cannot be reached publicly, it is either a missing legitimate
contract or unobservable implementation state. Improve the production API only
for a real caller contract; otherwise delete the test and record the lost claim.
Internal invariants MUST be tested through public observable behavior, generated
output, executable artifacts, or another real package boundary. A `main`
package MUST use a separate external test package and invoke its executable or
generation boundary; being non-importable does not permit private access.

A test-local implementation of an exported extension interface is a public API
client and MAY be used when it does not depend on private state.

### 12.3 Arrange Isolation

Each `t.Run` MUST own its complete arrange. Mutable objects MUST NOT be shared
between sibling subtests, and a parent test MUST NOT hold the object graph its
children exercise. Repeated setup is expected. Test helpers MUST NOT hide the
behavior under test or add a second abstraction layer.

Use real in-memory implementations by default. Reproduce failures with real
states such as cancellation, invalid input, missing values, exhaustion, or
concurrent conflict. If no public real mechanism can produce a condition, the
case MUST NOT be kept alive by a proxy double.

### 12.4 Assertions and Cleanup

- Use `require`, not `assert`.
- Defer cleanup immediately after successful allocation.
- Keep setup, behavior, and expectation visible in one flow.
- Aim for at most one `t.Run` level.
- Table tests SHOULD be used when cases share one shape.

### 12.5 Runtime Parity

Opcode examples belong to the public `Interpreter.Run` specification. A change
touching threaded, fused, optimized, or JIT paths MUST test every applicable
mode or state why a mode is not applicable. Tests MUST assert returned values,
errors, encoded output, profiling snapshots, or another public observable
boundary, never dispatch-table or lowering internals.

### 12.6 Fuzzing

Fuzz tests SHOULD target trust boundaries and semantic parity: instruction and
program parsing, verification, type parsing, and optimizer equivalence. Inputs
MUST be bounded so CI work remains finite.

## 13. Generated and Architecture-Specific Code

Generated files MUST be changed through their generator. Generator output MUST
be deterministic, formatted, and verified with `make check-generated`.

Architecture-specific code MUST use matching build-tagged implementations,
stubs, and tests. Portable behavior belongs in default files; architecture
files contain only the narrow behavior that differs.

Adding or changing an architecture path MUST update `docs/compatibility.md` and
the relevant implementation guide or JIT contract.

## 14. Performance and Benchmarks

Correctness and simplicity are prerequisites for optimization. Performance work
MUST:

1. define the public workload and compared modes;
2. capture a reproducible baseline;
3. profile or otherwise identify the actual hot path;
4. change the smallest owning layer;
5. rerun correctness, race, and parity tests; and
6. report before/after medians with `ns/op`, `B/op`, and `allocs/op`.

Benchmarks MUST validate fixtures before timing and results after timing. Setup,
warmup, reset, cleanup, and expected-result computation stay outside the timer
unless they are the named operation. Inputs MUST be deterministic. Warm JIT
benchmarks MUST prove native installation before timing and MUST NOT report
threaded fallback as JIT throughput.

Direct interpreter costs belong in `interp/*_test.go`; runtime-neutral kernels
belong in `benchmarks/`; external comparisons require the `compare` build tag.
Benchmark claims MUST name the hardware, OS, Go version, command, count, and
statistical summary. A regression in an unoptimized path MUST NOT be hidden by
an aggregate score.

The canonical commands are:

```bash
make benchmark-pr
make benchmark-core
make benchmark-nightly
make benchmark-compare
```

## 15. Git and Documentation

### 15.1 Commits

Commits MUST have one reason to exist and use
`<type>(scope): <imperative summary>` with a summary no longer than 72
characters.

| Change | Type |
|---|---|
| behavior | `feat` |
| defect | `fix` |
| performance | `perf` |
| internal design | `refactor` |
| tests only | `test` |
| documentation | `docs` |

Breaking changes MUST include `!` and a `BREAKING CHANGE:` body. Performance
commits MUST record baseline, result, and conclusion in the commit body or
linked change summary.

### 15.2 Documentation Ownership

Documentation is part of the contract. Each topic has one owner; other docs
summarize and link rather than duplicate it.

| Change | Update |
|---|---|
| coding rules, names, structure | this specification |
| architecture, ownership, invariants | `docs/architecture.md` |
| opcode semantics and JIT status | `docs/instruction-set.md` |
| JIT contracts and assembler APIs | `docs/jit-internals.md` |
| benchmark results and methodology | `docs/benchmarks.md` |
| workflow or convention routing | `AGENTS.md`, `.claude/CLAUDE.md` |
| document index or shape | `docs/README.md` |

Documentation MUST describe current behavior only, use canonical minivm terms,
and remain concise, factual, and reproducible.

## 16. Completion Checklist

Before completing a change, verify:

- [ ] package and type ownership are clear (§6-§8);
- [ ] top-down design and bottom-up symbol reviews are complete (§2.1-§2.2);
- [ ] every touched symbol has a current reason to exist (§2.2);
- [ ] another simplification pass found no safe improvement (§2.3);
- [ ] names use canonical terms (§4);
- [ ] functions hold one abstraction level (§3.1);
- [ ] helpers remove real complexity (§3.3);
- [ ] public APIs are minimal and compatibility constraints are respected (§5);
- [ ] mutable values are defensively copied (§5.3);
- [ ] declarations and tests follow source order (§3.2, §9, §12);
- [ ] runtime, ownership, and fallback invariants remain intact (§7-§8);
- [ ] operational errors preserve identity (§10);
- [ ] concurrency and lifecycle ownership are explicit (§11);
- [ ] generated and architecture paths are synchronized (§13);
- [ ] performance claims have reproducible evidence (§14);
- [ ] relevant tests, race checks, static analysis, and benchmarks pass; and
- [ ] unrelated user changes are absent from every commit.

## Maintenance Notes

Changes to this specification SHOULD remove ambiguity rather than add process.
New rules MUST prevent a demonstrated class of defect or inconsistency. Examples
MUST use current APIs. Historic decisions belong in focused audit or
architecture docs, not in this standard.

## Related Documents

- `AGENTS.md` — repository workflow and task router
- `.claude/CLAUDE.md` — Claude-specific execution overlay
- `docs/architecture.md` — package boundaries and runtime ownership
- `docs/testing.md` — public API test ownership inventory
- `docs/benchmarks.md` — measured results and methodology
- `docs/jit-internals.md` — JIT and assembler contracts
- `docs/README.md` — documentation ownership and format
