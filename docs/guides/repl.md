# REPL

Interactive assembly REPL for bytecode programs. Debugger API: `debugging.md`; opcode syntax: `instruction-set.md`.

## Running

```bash
./dist/minivm
./dist/minivm run <file>
```

`run` accepts the `.show`/`.save` text format: instructions, optional `NNNN:\t` byte offsets, `.const` function blocks, and type descriptors. Exit status: `0` success, `1` file/parse/verification/runtime error. Diagnostics use stderr.

## Input

One instruction per line; the accumulated program runs and prints the current stack.

```text
> i32.const 42
42
> i32.const 8
42 8
> i32.add
50
```

## Commands

| Command | Effect |
|---|---|
| `.const` | function-constant block; blank line ends it |
| `.type` | type-descriptor block; blank line ends it |
| `.show` | disassemble accumulated program |
| `.profile` | re-execute with profiling |
| `.load <file>` | replace REPL state from file |
| `.save <file>` | write `Program.String()` format |
| `.reset` | clear instructions, constants, types, breakpoints |
| `.help` | help |
| `.quit` / `.exit` | exit |

`.load` replaces rather than merges state because merge would require renumbering embedded constant/type indexes.

`.save` rejects host-value constants such as `*interp.HostFunction` and live views such as `*interp.HostStruct` because they have no textual representation.

## Debugging

`.debug` installs `interp.Debugger`; breakpoints persist across debug sessions and `.reset` clears them.

### Breakpoints

```text
> .break 5
> .break 1:10
> .breaks
> .clear 1
> .enable 1
> .disable 1
```

Offsets are byte offsets. Function `0` is top-level.

### Debug Session

`.debug` starts in step mode and stops before the first instruction, regardless of breakpoints.

```text
> .break 5
> .debug
stopped at func=0 ip=0000 (i32.const)
debug> continue
breakpoint 1 at func=0 ip=0005 (i32.const)
debug> stack
42
debug> continue
42 8
```

| Command | Shorthand | Effect |
|---|---|---|
| `step` | `s` | one instruction, enter calls |
| `next` | `n` | one instruction, step over calls |
| `finish` | `f` | run to current-frame return |
| `continue` | `c` | run to next breakpoint or program end |
| `stack` | | operand stack |
| `locals` | | current locals |
| `globals` | | globals |
| `frames` | | call stack |
| `breaks` | | breakpoints |
| `break <spec>` | `b` | add breakpoint |
| `clear <id>` | | remove breakpoint |
| `quit` / `q` | | exit debug session |

Stops occur before the displayed instruction. The displayed IP is the next byte offset. `frames` marks the innermost frame with `>`.

## Precision

`.debug` disables JIT and uses `WithTick(1)` through `interp.WithDebugger`, preserving bytecode boundaries.

## Branches

Interactive input accepts relative or absolute targets:

```text
br 10
br @0x0010
```

`.show` prints absolute offsets; the REPL normalizes `@` targets to relative encoding.

Whole-program text (`.load`, `run`, `.code`) also accepts labels:

```text
loop:
nop
br_if done
br loop
done:
return
```

`br_table` labels follow `count, cases, default`. Numeric and symbolic targets may be mixed.

`.show`/`.save` emit `L%04d:` labels for instruction-boundary targets. The result round-trips through `.load`. Interactive `>` input has no labels because forward references require the complete input.

## Related

- `debugging.md`
- `instruction-set.md`
- `profile.md`
