# Guide: REPL

Interactive assembly REPL for MiniVM bytecode interpreter.

## Running

```bash
./dist/minivm                  # interactive REPL
./dist/minivm run <file>       # execute an assembly file and print the final stack
```

`run` accepts files written in the same format `.show` (and `.save`) emit — code lines optionally prefixed with `NNNN:\t`, followed by `.const`-style function blocks, followed by single-line type descriptors. The exit status is 0 on success and 1 on open/parse/runtime errors (diagnostics go to stderr).

## Basic Usage

Enter assembly instructions one per line. Each instruction executes immediately and current stack is printed after each step.

```
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
| `.const` | Declare a function constant (multi-line, end with blank line) |
| `.type` | Declare type descriptors (multi-line, end with blank line) |
| `.show` | Disassemble the accumulated program |
| `.profile` | Re-execute with profiling and print function/IP/opcode samples |
| `.load <file>` | Replace REPL state with the program parsed from `<file>` |
| `.save <file>` | Write the current program to `<file>` in `Program.String()` format |
| `.reset` | Clear all instructions, constants, types, and breakpoints |
| `.help` | Show help |
| `.quit` / `.exit` | Exit the REPL |

`.save` followed by `.load` round-trips the session through a file. `.load` replaces state rather than merging — merging would require renumbering instruction-embedded constant and type indices, which is out of scope. `.save` refuses programs that contain host-typed constants (`*interp.HostFunction`, `*interp.HostObject`) because those values have no textual representation.

## Debugging

REPL integrates `interp.Debugger` for GDB-style bytecode-level debugging. Breakpoints persist across `.debug` sessions; `.reset` clears them.

### Setting Breakpoints

```
> .break 5          set breakpoint at func=0, ip=5
> .break 1:10       set breakpoint at func=1, ip=10
> .breaks           list all breakpoints
> .clear 1          remove breakpoint 1
> .enable 1         enable breakpoint 1
> .disable 1        disable breakpoint 1
```

Breakpoint offsets match byte offsets shown by `.show`.

### Starting a Debug Session

`.debug` runs accumulated program under debugger. Execution always stops at first instruction (step mode), regardless of breakpoints.

```
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

### Debug Sub-loop Commands

| Command | Shorthand | Effect |
|---|---|---|
| `step` | `s` | Execute one instruction, entering calls |
| `next` | `n` | Execute one instruction, stepping over calls |
| `finish` | `f` | Run until current frame returns |
| `continue` | `c` | Run until next breakpoint or program end |
| `stack` | | Print the operand stack |
| `locals` | | Print local variables of the current frame |
| `globals` | | Print all global variables |
| `frames` | | Print the call stack |
| `breaks` | | List all breakpoints |
| `break <spec>` | `b` | Add a breakpoint (also persists to REPL) |
| `clear <id>` | | Remove a breakpoint |
| `quit` / `q` | | Exit the debug session |

Empty line re-prints current stopped location.

### Inspection

All stops occur **before** indicated instruction executes. Displayed ip is bytecode offset of next instruction to run.

`locals` and `globals` iterate until out-of-range index reached (no explicit count needed).

`frames` prints full call stack with `>` marking innermost frame:

```
debug> frames
> frame[0] func=0 ip=0005
  frame[1] func=1 ip=0012
```

### JIT and Precision

`.debug` automatically disables JIT and sets tick=1 (via `interp.WithDebugger`), preserving exact bytecode instruction boundaries for step-level control.

## Branch Syntax

Both relative and absolute branch targets are accepted:

```
br 10           relative offset from instruction end
br @0x0010      absolute byte offset in accumulated program
```

`.show` output uses absolute offsets; REPL normalizes them to relative on input.
