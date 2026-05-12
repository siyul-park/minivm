# Guide: Interactive Assembly REPL

The `minivm` binary provides an interactive REPL for writing and executing
assembly instructions one at a time.

## Agent Checklist

Read this before editing `cmd/repl/`, `cmd/minivm/`, or text parsing behavior in `instr/`.

- REPL execution reruns the full accumulated program each step.
- `.const` parses function constants; `.type` parses type descriptors.
- Absolute branch syntax (`@N`) is REPL sugar and normalizes to relative byte offsets before `instr.Parse`.
- Keep formatted output pasteable back into the parser.
- Verify with `go test ./cmd/repl ./cmd/minivm ./instr`.

## Usage

```bash
make build
./dist/minivm
```

The REPL reads from stdin and writes to stdout, so it is also scriptable:

```bash
echo -e "i32.const 21\ni32.const 21\ni32.add\n.quit" | ./dist/minivm
```

## Session Example

```
MiniVM REPL — type '.help' for commands, '.quit' to exit
> i32.const 42
42
> i32.const 8
42 8
> i32.add
50
> .show
0000:	i32.const 0x0000002A
0005:	i32.const 0x00000008
0010:	i32.add
> .profile
profile samples: 3
functions:
func	samples	%
0	3	100.0%
func 0 ips:
ip	samples	%
0000	1	33.3%
0005	1	33.3%
0010	1	33.3%
opcodes:
opcode	samples	%
i32.const	2	66.7%
i32.add	1	33.3%
> .reset
reset.
> .quit
bye
```

After each instruction the current stack is printed bottom-to-top. An empty
stack produces no output.

## Commands

| Command | Effect |
|---|---|
| `.help` | Print command reference |
| `.show` | Format (disassemble) the accumulated instruction history (includes constants and types) |
| `.profile` | Run the accumulated program once with exact sampling and print a profile report |
| `.reset` | Clear accumulated instructions, constants, types, and stack state |
| `.const` | Declare a function constant (multi-line block, end with blank line) |
| `.type` | Declare one or more types (multi-line block, end with blank line) |
| `.quit` / `.exit` | Exit the REPL |

## Instruction Syntax

The REPL accepts the same text format that `instr.Format` produces, so
you can paste formatted output directly.

**Plain form** (for interactive use):
```
i32.const 42
i32.const -1
f32.const 1.0
br_table 0x02 0x0000 0x0005 0x0000
```

**Offset-prefixed form** (formatted output, also accepted):
```
0000:	i32.const 0x0000002A
0005:	i32.add
```

### Operand formats

| Format | Example | Accepted for |
|---|---|---|
| Hex | `0x2a`, `0x3F800000` | all operands |
| Unsigned decimal | `42`, `5` | integer operands |
| Signed decimal | `-1` | integer operands |
| Float literal | `1.0`, `-3.14` | `f32.const`, `f64.const` (encoded as IEEE 754 bits) |
| Absolute address | `@8`, `@0x0010` | `br`, `br_if`, `br_table` targets only |

### Branch target formats

Branch instructions accept two forms for their target operands:

- **Relative** (existing): `br 0x0005` — jump 5 bytes past the end of this instruction (raw operand stored in the bytecode)
- **Absolute** (`@`-prefixed): `br @0x0010` — jump to byte offset 16 in the accumulated program; the REPL converts this to the correct relative offset before encoding

The `@` prefix works with both hex and decimal:

```
> i32.const 1
1
> br @13
1
> i32.const 99
1 99
> i32.const 2
1 99 2
```

In step-by-step interactive use, absolute targets must refer to instructions already in the accumulated history (or the end of the current program). Forward references to not-yet-typed instructions are not supported.

## What Works in the REPL

The REPL is designed for stack-based arithmetic and value manipulation.
Everything that operates purely on value-typed stack entries works correctly.

**Fully supported:**

| Category | Examples |
|---|---|
| Integer arithmetic | `i32.add`, `i32.mul`, `i32.div_s`, `i64.sub`, … |
| Float arithmetic | `f32.add`, `f64.mul`, `f64.div`, … |
| Comparisons | `i32.eq`, `i32.lt_s`, `f64.ge`, … |
| Type conversions | `i32.to_i64_s`, `f32.to_f64`, `i64.to_i32`, … |
| Bitwise ops | `i32.and`, `i32.xor`, `i64.shl`, … |
| Stack manipulation | `drop`, `dup`, `swap`, `nop` |
| Branches | `br`, `br_if`, `br_table` — offsets span the full accumulated history |
| Globals | `global.get`, `global.set`, `global.tee` |
| Locals | `local.get`, `local.set`, `local.tee` |
| Constants | `const.get N` — after declaring constant N with `.const` |
| Functions | call a declared function with `const.get N` + `call` |
| Arrays | `array.new`, `array.new_default`, `array.get`, `array.set`, `array.fill` |
| Structs | `struct.new`, `struct.new_default`, `struct.get`, `struct.set` |
| Strings | `string.new_utf32`, `string.concat`, `string.len`, string comparisons |
| References | `ref.null`, `ref.is_null`, `ref.eq`, `ref.test`, `ref.cast` |

## Declaring Function Constants

Use `.const` to add a function to the constant pool. The format is identical
to what `Program.String()` / `.show` produces for constants — paste formatted
output directly.

Instructions can be written with or without the offset prefix:

```
> .const
... func() i32
... i32.const 42
... return
...
constant 0 added.
```

Offset-prefixed form (from `.show` output) is also accepted:

```
> .const
... func() i32
... 0000:	i32.const 0x0000002A
... 0005:	return
...
constant 0 added.
> const.get 0
> call
42
> .show
0000:	const.get 0x0000
0003:	call

0000:	func() i32
	0000:	i32.const 0x0000002A
	0005:	return
```

Functions with parameters and locals:

```
> .const
... func(i32) i32
... i32
... local.get 0
... local.get 0
... i32.add
... return
...
constant 0 added.
```

The block prompt `... ` appears for each line. End the block with a blank line.

## Declaring Types

Use `.type` to add type definitions to the type pool. Types are needed by
instructions like `array.new`, `struct.new`, `ref.test`, and `ref.cast`.

The format matches the types section of `Program.String()` / `.show` output —
paste it directly. Each line is one type definition; an optional `N:\t` index
prefix is stripped automatically.

```
> .type
... struct {i32; f64}
...
type 0 added.

> .type
... []i32
... struct {i32; f64}
...
type 0 added.
type 1 added.
```

Pasting `.show` types section directly also works:

```
> .type
... 0:	struct {i32; f64}
... 1:	[]i32
...
type 0 added.
type 1 added.
```

## JIT Limitations

Function calls, globals, references, arrays, structs, strings, and other heap-object operations always run in the threaded interpreter. ARM64 JIT covers straight-line numeric work plus selected stack operations, locals, constants, `select`, and branch instructions when the current stack shape can be represented by the native segment signature.

## Execution Model

Each step reruns the **full accumulated instruction history** plus the new
instruction from scratch in a fresh interpreter. This ensures heap-allocated
objects (`KindRef` values from `array.new`, `struct.new`, etc.) are always
valid across steps.

For each new instruction:

1. A `program.Program` is built from all previously accepted instructions plus
   the new one.
2. A fresh `interp.Interpreter` runs the whole program.
3. The resulting stack is printed.
4. On error the new instruction is **not** added to history (session stays
   consistent).

Heap, globals, and branches work naturally because the full program is
recompiled and re-executed each step. The execution cost is O(N) per step
(where N is the number of accumulated instructions), which is negligible for
interactive sessions.

## Parsing API

The text parser lives in the `instr` package and is usable independently:

```go
// Parse one line — accepts plain or offset-prefixed format.
inst, err := instr.Parse("i32.const 42")

// Parse a reader (file, strings.NewReader, os.Stdin, …).
instrs, err := instr.ParseAll(strings.NewReader("i32.const 1\ni32.add"))
instrs, err  = instr.ParseAll(os.Stdin)
```

`ParseAll` skips blank lines and reports the first error with its line number.
The output of `instr.Format` round-trips cleanly through `ParseAll`:

```go
original := []instr.Instruction{instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD)}
text     := instr.Format(instr.Marshal(original))
parsed, _ := instr.ParseAll(strings.NewReader(text))
// parsed == original
```

## Extending the CLI

`cmd/minivm/main.go` uses [cobra](https://github.com/spf13/cobra). To add a
new subcommand (e.g. `minivm run <file>`) that executes an assembly file:

```go
runCmd := &cobra.Command{
    Use:   "run <file>",
    Short: "Execute an assembly file",
    Args:  cobra.ExactArgs(1),
    RunE: func(cmd *cobra.Command, args []string) error {
        f, err := os.Open(args[0])
        if err != nil {
            return err
        }
        defer f.Close()
        instrs, err := instr.ParseAll(f)
        if err != nil {
            return err
        }
        vm := interp.New(program.New(instrs))
        defer vm.Close()
        return vm.Run(cmd.Context())
    },
}
root.AddCommand(runCmd)
```
