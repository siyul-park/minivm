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
const blockPrompt = "... "

const helpText = `MiniVM Assembly REPL

Enter assembly instructions one per line. Each instruction executes
immediately and the current stack is shown after each step.

Instructions (examples):
  i32.const 42        push i32 constant
  i32.const 8         push another i32
  i32.add             pop two i32s, push their sum
  br 0x0005           branch to byte offset 5

Commands:
  .const              declare a function constant (multi-line, end with blank line)
  .type <type>        declare a type (e.g. .type struct {i32; f64})
  .show               show disassembly of accumulated program
  .reset              clear all accumulated instructions, stack, constants, and types
  .help               show this help
  .quit  /  .exit     exit the REPL
`

// REPL holds accumulated instructions, persistent stack state, constants, and types.
type REPL struct {
	in        io.Reader
	out       io.Writer
	instrs    []instr.Instruction // for .show and .reset
	stack     []types.Boxed       // raw NaN-boxed stack values carried across steps
	constants []types.Value       // constant pool passed to each step
	typs      []types.Type        // type pool passed to each step
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
			done, err := r.handleMeta(scanner, line)
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

func (r *REPL) handleMeta(scanner *bufio.Scanner, line string) (done bool, err error) {
	lower := strings.ToLower(line)
	switch {
	case lower == ".quit" || lower == ".exit":
		fmt.Fprintln(r.out, "bye")
		return true, nil
	case lower == ".reset":
		r.instrs = nil
		r.stack = nil
		r.constants = nil
		r.typs = nil
		fmt.Fprintln(r.out, "reset.")
	case lower == ".show":
		prog := program.New(r.instrs, program.WithConstants(r.constants...), program.WithTypes(r.typs...))
		if len(r.instrs) == 0 && len(r.constants) == 0 && len(r.typs) == 0 {
			fmt.Fprintln(r.out, "(empty)")
		} else {
			fmt.Fprint(r.out, prog.String())
		}
	case lower == ".help":
		fmt.Fprint(r.out, helpText)
	case lower == ".const":
		if err := r.readConstant(scanner); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
		}
	case strings.HasPrefix(lower, ".type"):
		typeStr := strings.TrimSpace(line[5:])
		if err := r.addType(typeStr); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
		}
	default:
		fmt.Fprintf(r.out, "unknown command: %s (type '.help' for help)\n", line)
	}
	return false, nil
}

// readConstant reads a multi-line constant definition (until blank line) and
// appends the parsed constant to r.constants.
func (r *REPL) readConstant(scanner *bufio.Scanner) error {
	var lines []string
	for {
		fmt.Fprint(r.out, blockPrompt)
		if !scanner.Scan() {
			break
		}
		l := scanner.Text()
		if l == "" {
			break
		}
		lines = append(lines, l)
	}

	if len(lines) == 0 {
		return fmt.Errorf("empty constant definition")
	}

	fn, err := types.ParseFunction(lines)
	if err != nil {
		return err
	}

	r.constants = append(r.constants, fn)
	fmt.Fprintf(r.out, "constant %d added.\n", len(r.constants)-1)
	return nil
}

// addType parses a type string and appends it to r.typs.
func (r *REPL) addType(s string) error {
	if s == "" {
		return fmt.Errorf("missing type: usage: .type <type>")
	}
	t, err := types.Parse(s)
	if err != nil {
		return err
	}
	r.typs = append(r.typs, t)
	fmt.Fprintf(r.out, "type %d added.\n", len(r.typs)-1)
	return nil
}

// execute runs a single instruction on top of the saved stack state.
// On success it updates r.stack and prints the result; on error it leaves
// r.stack unchanged.
func (r *REPL) execute(ctx context.Context, inst instr.Instruction) error {
	prog := program.New(
		[]instr.Instruction{inst},
		program.WithConstants(r.constants...),
		program.WithTypes(r.typs...),
	)
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

	// Save resulting stack via Peek (bottom-to-top order).
	// Peek returns raw Boxed values without unboxing, so KindRef values (e.g.
	// function refs from const.get) stay valid when pushed into the next
	// interpreter, which has the same constant pool heap layout.
	n := vm.Len()
	newStack := make([]types.Boxed, n)
	for k := 0; k < n; k++ {
		v, _ := vm.Peek(n - 1 - k)
		newStack[k] = v
	}
	r.stack = newStack

	printStack(r.out, r.stack)
	return nil
}

func printStack(out io.Writer, stack []types.Boxed) {
	if len(stack) == 0 {
		return
	}
	parts := make([]string, len(stack))
	for i, v := range stack {
		parts[i] = types.Unbox(v).String()
	}
	fmt.Fprintln(out, strings.Join(parts, " "))
}
