# Guide: Interactive Assembly REPL

The `minivm` binary provides an interactive REPL for writing and executing
assembly instructions one at a time.

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
| `.show` | Format (disassemble) the accumulated instruction history (includes constants) |
| `.reset` | Clear accumulated instructions, constants, and stack state |
| `.const` | Declare a function constant (multi-line block, end with blank line) |
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
| Branches (within one step) | `br`, `br_if`, `br_table` — but see limitation below |
| Globals | `global.get`, `global.set`, `global.tee` |
| Locals | `local.get`, `local.set`, `local.tee` — index relative to bottom of pre-pushed stack |
| Constants | `const.get N` — after declaring constant N with `.const` |
| Functions | call a declared function with `const.get N` + `call` |

## Declaring Function Constants

Use `.const` to add a function to the constant pool. The format is identical
to what `Program.String()` / `.show` produces for constants — paste formatted
output directly.

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
... 0000:	local.get 0x00
... 0003:	local.get 0x00
... 0006:	i32.add
... 000b:	return
...
constant 0 added.
```

The block prompt `... ` appears for each line. End the block with a blank line.

## What Does Not Work

### Heap-producing instructions (`array.new`, `struct.new`, string ops)

These instructions allocate objects on the interpreter's heap and push a
`KindRef` (heap index) onto the stack. Each REPL step uses a fresh
interpreter with an empty heap, so a ref saved from step N points into the
old interpreter's heap and is invalid in step N+1.

Affected opcodes: `array.new`, `array.new_default`, `array.get`, `array.set`,
`struct.new`, `struct.new_default`, `struct.get`, `struct.set`,
`string.new_utf32`, `string.concat`, `ref.null`, `ref.cast`, `ref.test`, and
all other string/array/struct operations.

### Branches that span multiple steps

A `br 0x0005` instruction is compiled as part of a one-instruction program.
The branch offset is interpreted relative to the current instruction, so a
branch within a single step works, but you cannot branch to an instruction
typed in a previous REPL step.

## Execution Model

Each instruction executes immediately on top of the current stack state.
Only the new instruction runs — previous instructions are not re-executed.

Internally the REPL maintains `stack []types.Value`. For each new instruction:

1. A single-instruction `program.Program` is created.
2. A fresh `interp.Interpreter` is initialized with saved stack values
   pre-pushed via `Push()`.
3. The one instruction executes and the resulting stack is saved back.
4. On error the stack is not updated and the instruction is not added to
   history (session remains consistent).

This gives O(1) execution cost per instruction regardless of session length.

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
