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
0000:   i32.const 0x0000002A
0005:   i32.const 0x00000008
000a:   i32.add
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
| `.show` | Disassemble the accumulated instruction history |
| `.reset` | Clear accumulated instructions and stack state |
| `.quit` / `.exit` | Exit the REPL |

## Instruction Syntax

The REPL accepts the same text format that `instr.Disassemble` produces, so
you can paste disassembly output directly.

**Plain form** (for interactive use):
```
i32.const 42
i32.const -1
f32.const 1.0
br_table 0x02 0x0000 0x0005 0x0000
```

**Offset-prefixed form** (disassembly output, also accepted):
```
0000:   i32.const 0x0000002A
0005:   i32.add
```

### Operand formats

| Format | Example | Accepted for |
|---|---|---|
| Hex | `0x2a`, `0x3F800000` | all operands |
| Unsigned decimal | `42`, `5` | integer operands |
| Signed decimal | `-1` | integer operands |
| Float literal | `1.0`, `-3.14` | `f32.const`, `f64.const` |

Float literals are encoded as IEEE 754 bits using the operand width (4 bytes
for f32, 8 bytes for f64).

## Execution Model

Each instruction executes immediately on top of the current stack state.
Only the new instruction runs — previous instructions are not re-executed.

Internally the REPL maintains `stack []types.Value`. For each new instruction:

1. A single-instruction `program.Program` is created.
2. A fresh `interp.Interpreter` is initialized with saved stack values
   pre-pushed.
3. The one instruction executes and the resulting stack is saved.
4. On error the stack is not updated and the instruction is not added to
   history.

This gives O(1) execution cost per instruction regardless of session length.

> **Limitation**: `KindRef` values (heap pointers) are bound to a specific
> interpreter's heap. Transferring them across instruction steps is not
> supported; instructions that produce refs (e.g. `array.new`, `struct.new`)
> will not behave correctly in the REPL.

## Parsing API

The text parser lives in the `instr` package and is available for other uses:

```go
// Parse one line — accepts plain or offset-prefixed format.
inst, err := instr.Parse("i32.const 42")

// Parse a reader (file, strings.NewReader, os.Stdin, …).
instrs, err := instr.ParseAll(strings.NewReader("i32.const 1\ni32.add"))
instrs, err  = instr.ParseAll(os.Stdin)
```

`ParseAll` skips blank lines and reports the first error with its line number.
The output of `instr.Disassemble` round-trips cleanly through `ParseAll`.

## Extending the CLI

`cmd/minivm/main.go` uses [cobra](https://github.com/spf13/cobra). To add a
new subcommand (e.g. `minivm run <file>`):

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
