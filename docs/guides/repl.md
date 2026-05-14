# Guide: REPL

Interactive assembly REPL for the MiniVM bytecode interpreter.

## Running

```bash
./dist/minivm
```

## Basic Usage

Enter assembly instructions one per line. Each instruction executes immediately and the current stack is printed after each step.

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
| `.reset` | Clear all instructions, constants, types, and breakpoints |
| `.help` | Show help |
| `.quit` / `.exit` | Exit the REPL |

## Debugging

The REPL integrates `interp.Debugger` for GDB-style bytecode-level debugging. Breakpoints persist across `.debug` sessions; `.reset` clears them.

### Setting Breakpoints

```
> .break 5          set breakpoint at func=0, ip=5
> .break 1:10       set breakpoint at func=1, ip=10
> .breaks           list all breakpoints
> .clear 1          remove breakpoint 1
> .enable 1         enable breakpoint 1
> .disable 1        disable breakpoint 1
```

Breakpoint offsets match the byte offsets shown by `.show`.

### Starting a Debug Session

`.debug` runs the accumulated program under the debugger. Execution always stops at the first instruction (step mode), regardless of breakpoints.

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

An empty line re-prints the current stopped location.

### Inspection

All stops occur **before** the indicated instruction executes. The displayed ip is the bytecode offset of the next instruction to run.

`locals` and `globals` iterate until an out-of-range index is reached (no explicit count needed).

`frames` prints the full call stack with `>` marking the innermost frame:

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

`.show` output uses absolute offsets; the REPL normalizes them to relative on input.
