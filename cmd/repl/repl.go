package repl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strconv"
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
  global.set 0        pop and store into global 0
  global.get 0        push global 0
  br 0x0005           branch (relative offset from instruction end)
  br @0x0010          branch (absolute byte offset in accumulated program)

Commands:
  .const              declare a function constant (multi-line, end with blank line)
  .type               declare one or more types (multi-line, end with blank line)
                        e.g.  struct {i32; f64}
                              []i32
  .show               show disassembly of accumulated program
  .reset              clear all accumulated instructions, stack, constants, and types
  .help               show this help
  .quit  /  .exit     exit the REPL
`

// REPL holds accumulated instructions, constants, and types.
type REPL struct {
	in        io.Reader
	out       io.Writer
	instrs    []instr.Instruction
	codeLen   int // byte length of instr.Marshal(instrs); updated incrementally
	constants []types.Value
	typs      []types.Type
}

// New returns a new REPL that reads from in and writes to out.
func New(in io.Reader, out io.Writer) *REPL {
	return &REPL{in: in, out: out}
}

// Run starts the read-eval-print loop. It returns nil on clean exit.
func (r *REPL) Run(ctx context.Context) error {
	fmt.Fprintf(r.out, "MiniVM Assembly REPL — type '.help' for commands, '.quit' to exit\n")

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

		rewritten, rewErr := rewriteBranchAbsolute(line, r.codeLen)
		if rewErr != nil {
			fmt.Fprintf(r.out, "error: %v\n", rewErr)
			continue
		}

		inst, err := instr.Parse(rewritten)
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
		r.codeLen += len(inst)
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
		r.codeLen = 0
		r.constants = nil
		r.typs = nil
		fmt.Fprintln(r.out, "reset.")
	case lower == ".show":
		if len(r.instrs) == 0 && len(r.constants) == 0 && len(r.typs) == 0 {
			fmt.Fprintln(r.out, "(empty)")
		} else {
			prog := program.New(r.instrs, program.WithConstants(r.constants...), program.WithTypes(r.typs...))
			fmt.Fprint(r.out, prog.String())
		}
	case lower == ".help":
		fmt.Fprint(r.out, helpText)
	case lower == ".const":
		if err := r.readConstant(scanner); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
		}
	case lower == ".type":
		if err := r.readTypes(scanner); err != nil {
			fmt.Fprintf(r.out, "error: %v\n", err)
		}
	default:
		fmt.Fprintf(r.out, "unknown command: %s (type '.help' for help)\n", line)
	}
	return false, nil
}

func (r *REPL) readBlock(scanner *bufio.Scanner) []string {
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
	return lines
}

func (r *REPL) readConstant(scanner *bufio.Scanner) error {
	lines := r.readBlock(scanner)
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

// readTypes accepts the program.String() format: optional "N:\t" index prefix is stripped.
func (r *REPL) readTypes(scanner *bufio.Scanner) error {
	lines := r.readBlock(scanner)
	if len(lines) == 0 {
		return fmt.Errorf("empty type definition")
	}
	for _, l := range lines {
		if idx := strings.Index(l, ":\t"); idx >= 0 {
			l = l[idx+2:]
		}
		if err := r.addType(l); err != nil {
			return err
		}
	}
	return nil
}

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

// execute reruns the full accumulated history plus inst; the caller appends inst on success.
func (r *REPL) execute(ctx context.Context, inst instr.Instruction) error {
	all := make([]instr.Instruction, len(r.instrs)+1)
	copy(all, r.instrs)
	all[len(r.instrs)] = inst

	prog := program.New(
		all,
		program.WithConstants(r.constants...),
		program.WithTypes(r.typs...),
	)
	vm := interp.New(prog)
	defer vm.Close()

	if err := vm.Run(ctx); err != nil {
		return err
	}

	printStack(r.out, vm)
	return nil
}

func printStack(out io.Writer, vm *interp.Interpreter) {
	n := vm.Len()
	if n == 0 {
		return
	}
	parts := make([]string, n)
	for k := 0; k < n; k++ {
		v, _ := vm.Peek(k)
		parts[n-1-k] = formatBoxed(v, vm)
	}
	fmt.Fprintln(out, strings.Join(parts, " "))
}

// formatBoxed returns a human-readable string for a stack value.
// KindRef values are resolved through the interpreter heap so the
// actual object (array, struct, string, …) is displayed rather than
// a raw heap index. Multi-line values (functions) are truncated to
// their first line. i64/f32/f64 carry a type suffix to disambiguate
// from the more common i32.
func formatBoxed(v types.Boxed, vm *interp.Interpreter) string {
	switch v.Kind() {
	case types.KindI32:
		return fmt.Sprintf("%d", v.I32())
	case types.KindI64:
		return fmt.Sprintf("%d (i64)", v.I64())
	case types.KindF32:
		return fmt.Sprintf("%g (f32)", v.F32())
	case types.KindF64:
		return fmt.Sprintf("%g (f64)", v.F64())
	case types.KindRef:
		val, err := vm.Load(v.Ref())
		if err != nil || val == nil {
			return "null"
		}
		s := val.String()
		if idx := strings.IndexByte(s, '\n'); idx >= 0 {
			s = s[:idx]
		}
		return s
	default:
		return "<invalid>"
	}
}

// rewriteBranchAbsolute replaces "@N" absolute byte targets in branch
// instructions with the equivalent relative offset, given that the
// instruction will be encoded starting at byte ip.
// Lines with no "@" tokens are returned unchanged.
func rewriteBranchAbsolute(line string, ip int) (string, error) {
	if idx := strings.Index(line, ":\t"); idx >= 0 {
		line = line[idx+2:]
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return line, nil
	}
	opLower := strings.ToLower(fields[0])
	switch opLower {
	case "br", "br_if":
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "@") {
			return line, nil
		}
		abs, err := parseIntLiteral(fields[1][1:])
		if err != nil {
			return "", fmt.Errorf("invalid absolute branch target %s: %w", fields[1], err)
		}
		const instrWidth = 3
		rel := abs - (ip + instrWidth)
		if rel < 0 || rel > 0xFFFF {
			return "", fmt.Errorf("branch target %s out of range from offset %d", fields[1], ip)
		}
		return fmt.Sprintf("%s %d", fields[0], rel), nil

	case "br_table":
		if len(fields) < 2 {
			return line, nil
		}
		hasAt := false
		for _, f := range fields[2:] {
			if strings.HasPrefix(f, "@") {
				hasAt = true
				break
			}
		}
		if !hasAt {
			return line, nil
		}
		count, err := parseIntLiteral(fields[1])
		if err != nil {
			return "", fmt.Errorf("invalid br_table count %s: %w", fields[1], err)
		}
		instrWidth := 4 + count*2
		parts := make([]string, len(fields))
		copy(parts, fields)
		for i, f := range fields[2:] {
			if !strings.HasPrefix(f, "@") {
				continue
			}
			abs, err := parseIntLiteral(f[1:])
			if err != nil {
				return "", fmt.Errorf("invalid absolute branch target %s: %w", f, err)
			}
			rel := abs - (ip + instrWidth)
			if rel < 0 || rel > 0xFFFF {
				return "", fmt.Errorf("branch target %s out of range from offset %d", f, ip)
			}
			parts[2+i] = fmt.Sprintf("%d", rel)
		}
		return strings.Join(parts, " "), nil
	}
	return line, nil
}

func parseIntLiteral(s string) (int, error) {
	v, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("expected integer, got %q", s)
	}
	return int(v), nil
}
