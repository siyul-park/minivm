# Guide: REPL

Interactive assembly REPL for minivm bytecode programs.

## When to Read

Use this guide when working with the command-line REPL, assembly files, bytecode debugging, or saved program text.

For the debugger API, see `docs/debugging.md`. For opcode syntax and semantics, see `docs/instruction-set.md`.

## Running

```bash
./dist/minivm                  # interactive REPL
./dist/minivm run <file>       # execute an assembly file and print the final stack
```

`run` accepts the same text format emitted by `.show` and `.save`: instructions, optional `NNNN:\t` byte-offset prefixes, `.const` function blocks, and type descriptors.

Exit status is `0` on success and `1` on file, parse, verification, or runtime errors. Diagnostics are written to stderr.

## Basic Usage

Enter one assembly instruction per line. The REPL executes the accumulated program and prints the current stack after each step.

```text
> i32.const 42
42
> i32.const 8
42 8
> i32.add
50
```

## Commands

| Command | Description |
|---|---|
| `.const` | Declare a function constant; end the block with a blank line |
| `.type` | Declare type descriptors; end the block with a blank line |
| `.show` | Disassemble the accumulated program |
| `.profile` | Re-execute with profiling and print function, IP, opcode, and metric samples |
| `.load <file>` | Replace REPL state with the parsed file |
| `.save <file>` | Write the current program in `Program.String()` format |
| `.reset` | Clear instructions, constants, types, and breakpoints |
| `.help` | Show help |
| `.quit` / `.exit` | Exit the REPL |

`.load` replaces state rather than merging it. Merging would require renumbering constant and type indexes embedded in instructions.

`.save` refuses programs with host-only constants such as `*interp.HostFunction` or `*interp.HostObject`, because those values have no textual representation.

## Debugging

The REPL integrates `interp.Debugger` for bytecode-level debugging. Breakpoints persist across `.debug` sessions; `.reset` clears them.

### Breakpoints

```text
> .break 5          set breakpoint at func=0, ip=5
> .break 1:10       set breakpoint at func=1, ip=10
> .breaks           list all breakpoints
> .clear 1          remove breakpoint 1
> .enable 1         enable breakpoint 1
> .disable 1        disable breakpoint 1
```

Breakpoint offsets are byte offsets, matching `.show` output.

### Debug Session

`.debug` runs the accumulated program under the debugger. Execution starts in step mode and stops before the first instruction, regardless of breakpoints.

```text
> i32.const 42
42
> i32.const 8
42 8
> .break 5
breakpoint 1 set at func=0 ip=5
> .debug
stopped at func=0 ip=0000 (i32.const)
debug> continue
breakpoint 1 at func=0 ip=0005 (i32.const)
debug> stack
42
debug> continue
42 8
```

### Debug Commands

| Command | Shorthand | Effect |
|---|---|---|
| `step` | `s` | Execute one instruction, entering calls |
| `next` | `n` | Execute one instruction, stepping over calls |
| `finish` | `f` | Run until the current frame returns |
| `continue` | `c` | Run until the next breakpoint or program end |
| `stack` | | Print the operand stack |
| `locals` | | Print current-frame locals |
| `globals` | | Print globals |
| `frames` | | Print the call stack |
| `breaks` | | List breakpoints |
| `break <spec>` | `b` | Add a breakpoint that also persists to the REPL |
| `clear <id>` | | Remove a breakpoint |
| `quit` / `q` | | Exit the debug session |

An empty line reprints the current stopped location.

All stops occur before the displayed instruction executes. The displayed IP is the byte offset of the next instruction.

`frames` marks the innermost frame with `>`.

```text
debug> frames
> frame[0] func=0 ip=0005
  frame[1] func=1 ip=0012
```

## JIT and Precision

`.debug` installs `interp.WithDebugger`, which disables JIT and sets `WithTick(1)`. This preserves exact bytecode instruction boundaries for stepping.

## Branch Syntax

Both relative and absolute branch targets are accepted.

```text
br 10           relative offset from instruction end
br @0x0010      absolute byte offset in accumulated program
```

`.show` prints absolute byte offsets. The REPL normalizes absolute branch input to relative offsets.

## Related Docs

- `docs/debugging.md` — debugger API and precision model
- `docs/instruction-set.md` — opcode semantics and branch-offset rules
- `docs/profile.md` — `.profile` output and sampling behavior
