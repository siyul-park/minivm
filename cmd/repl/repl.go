package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

const prompt = "> "

const helpText = `MiniVM Assembly REPL

Enter assembly instructions one per line. Each instruction is executed
immediately and the current stack is shown after each step.

Instructions (examples):
  i32.const 42        push i32 constant
  i32.const 8         push another i32
  i32.add             pop two i32s, push their sum
  br 0x0005           branch to byte offset 5

Commands:
  .show               show disassembly of accumulated program
  .reset              clear all accumulated instructions
  .help               show this help
  .quit  /  .exit     exit the REPL
`

// REPL holds accumulated instructions and runs them interactively.
type REPL struct {
	in     io.Reader
	out    io.Writer
	instrs []instr.Instruction
}

// New returns a new REPL that reads from in and writes to out.
func New(in io.Reader, out io.Writer) *REPL {
	return &REPL{in: in, out: out}
}

// Run starts the read-eval-print loop. It returns nil on clean exit.
func (r *REPL) Run(ctx context.Context) error {
	fmt.Fprintf(r.out, "MiniVM REPL — type '.help' for commands, '.quit' to exit\n")

	scanner := bufio.NewScanner(r.in)
	for {
		fmt.Fprint(r.out, prompt)
		if !scanner.Scan() {
			fmt.Fprintln(r.out)
			return scanner.Err()
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, ".") {
			done, err := r.handleMeta(line)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
			continue
		}

		inst, err := instr.Parse(line)
		if err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			continue
		}
		if inst == nil {
			continue
		}

		r.instrs = append(r.instrs, inst)

		if err := r.execute(ctx); err != nil {
			r.instrs = r.instrs[:len(r.instrs)-1]
			fmt.Fprintf(r.out, "error: %v\n", err)
		}
	}
}

func (r *REPL) handleMeta(line string) (done bool, err error) {
	switch strings.ToLower(line) {
	case ".quit", ".exit":
		fmt.Fprintln(r.out, "bye")
		return true, nil
	case ".reset":
		r.instrs = r.instrs[:0]
		fmt.Fprintln(r.out, "reset.")
	case ".show":
		if len(r.instrs) == 0 {
			fmt.Fprintln(r.out, "(empty)")
		} else {
			prog := program.New(r.instrs)
			fmt.Fprint(r.out, prog.String())
		}
	case ".help":
		fmt.Fprint(r.out, helpText)
	default:
		fmt.Fprintf(r.out, "unknown command: %s (type '.help' for help)\n", line)
	}
	return false, nil
}

func (r *REPL) execute(ctx context.Context) error {
	prog := program.New(r.instrs)
	vm := interp.New(prog)
	defer vm.Close()

	if err := vm.Run(ctx); err != nil {
		return err
	}

	printStack(r.out, vm)
	return nil
}

func printStack(out io.Writer, vm *interp.Interpreter) {
	var vals []types.Value
	for {
		v, err := vm.Pop()
		if err != nil {
			break
		}
		vals = append(vals, v)
	}

	if len(vals) == 0 {
		return
	}

	// vals[0] is TOS; display bottom-to-top
	parts := make([]string, len(vals))
	for i, v := range vals {
		parts[len(vals)-1-i] = v.String()
	}
	fmt.Fprintln(out, strings.Join(parts, " "))
}
