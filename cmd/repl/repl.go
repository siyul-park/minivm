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

Enter assembly instructions one per line. Each instruction executes
immediately and the current stack is shown after each step.

Instructions (examples):
  i32.const 42        push i32 constant
  i32.const 8         push another i32
  i32.add             pop two i32s, push their sum
  br 0x0005           branch to byte offset 5

Commands:
  .show               show disassembly of accumulated program
  .reset              clear all accumulated instructions and stack
  .help               show this help
  .quit  /  .exit     exit the REPL
`

// REPL holds accumulated instructions and persistent stack state.
type REPL struct {
	in     io.Reader
	out    io.Writer
	instrs []instr.Instruction // for .show and .reset
	stack  []types.Value       // stack state carried across instructions
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

		if err := r.execute(ctx, inst); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
			continue
		}
		r.instrs = append(r.instrs, inst)
	}
}

func (r *REPL) handleMeta(line string) (done bool, err error) {
	switch strings.ToLower(line) {
	case ".quit", ".exit":
		fmt.Fprintln(r.out, "bye")
		return true, nil
	case ".reset":
		r.instrs = r.instrs[:0]
		r.stack = r.stack[:0]
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

// execute runs a single instruction on top of the saved stack state.
// On success it updates r.stack and prints the result; on error it leaves
// r.stack unchanged.
func (r *REPL) execute(ctx context.Context, inst instr.Instruction) error {
	prog := program.New([]instr.Instruction{inst})
	vm := interp.New(prog)
	defer vm.Close()

	// Restore saved stack into the new interpreter before running.
	for _, v := range r.stack {
		if err := vm.Push(v); err != nil {
			return err
		}
	}

	if err := vm.Run(ctx); err != nil {
		return err
	}

	// Collect resulting stack (TOS first) and update saved state.
	var newStack []types.Value
	for {
		v, err := vm.Pop()
		if err != nil {
			break
		}
		newStack = append(newStack, v)
	}
	// Reverse to bottom-to-top order.
	for l, r := 0, len(newStack)-1; l < r; l, r = l+1, r-1 {
		newStack[l], newStack[r] = newStack[r], newStack[l]
	}
	r.stack = newStack

	printStack(r.out, r.stack)
	return nil
}

func printStack(out io.Writer, stack []types.Value) {
	if len(stack) == 0 {
		return
	}
	parts := make([]string, len(stack))
	for i, v := range stack {
		parts[i] = v.String()
	}
	fmt.Fprintln(out, strings.Join(parts, " "))
}
