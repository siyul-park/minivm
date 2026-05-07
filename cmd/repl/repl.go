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

type REPL struct {
	in        io.Reader
	out       io.Writer
	instrs    []instr.Instruction
	codeLen   int // byte length of instr.Marshal(instrs); updated incrementally
	constants []types.Value
	types     []types.Type
}

// New returns a new REPL that reads from in and writes to out.
func New(in io.Reader, out io.Writer) *REPL {
	return &REPL{in: in, out: out}
}

// Run reads and executes assembly instructions until EOF or .quit.
func (r *REPL) Run(ctx context.Context) error {
	fmt.Fprintln(r.out, "MiniVM Assembly REPL — type '.help' for commands, '.quit' to exit")

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
			done, err := r.command(scanner, line)
			if err != nil {
				return err
			}
			if done {
				return nil
			}
			continue
		}

		inst, err := r.parse(line)
		if err != nil {
			r.printErr(err)
			continue
		}
		if inst == nil {
			continue
		}

		if err := r.exec(ctx, inst); err != nil {
			r.printErr(err)
			continue
		}
		r.commit(inst)
	}
}

func (r *REPL) command(scanner *bufio.Scanner, line string) (bool, error) {
	switch strings.ToLower(line) {
	case ".quit", ".exit":
		fmt.Fprintln(r.out, "bye")
		return true, nil
	case ".reset":
		r.reset()
	case ".show":
		r.show()
	case ".help":
		fmt.Fprint(r.out, helpText)
	case ".const":
		if err := r.readConst(scanner); err != nil {
			r.printErr(err)
		}
	case ".type":
		if err := r.readType(scanner); err != nil {
			r.printErr(err)
		}
	default:
		fmt.Fprintf(r.out, "unknown command: %s (type '.help' for help)\n", line)
	}
	return false, nil
}

func (r *REPL) exec(ctx context.Context, inst instr.Instruction) error {
	vm := interp.New(r.build(inst))
	defer vm.Close()
	if err := vm.Run(ctx); err != nil {
		return err
	}
	printStack(r.out, vm)
	return nil
}

func (r *REPL) readConst(scanner *bufio.Scanner) error {
	lines := r.block(scanner)
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

// readType accepts the program.String() format: optional "N:\t" index prefix is stripped.
func (r *REPL) readType(scanner *bufio.Scanner) error {
	lines := r.block(scanner)
	if len(lines) == 0 {
		return fmt.Errorf("empty type definition")
	}
	for _, line := range lines {
		if _, after, ok := strings.Cut(line, ":\t"); ok {
			line = after
		}
		if err := r.addType(line); err != nil {
			return err
		}
	}
	return nil
}

func (r *REPL) block(scanner *bufio.Scanner) []string {
	var lines []string
	for {
		fmt.Fprint(r.out, blockPrompt)
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if line == "" {
			break
		}
		lines = append(lines, line)
	}
	return lines
}

func (r *REPL) addType(s string) error {
	if s == "" {
		return fmt.Errorf("missing type: usage: .type <type>")
	}
	t, err := types.Parse(s)
	if err != nil {
		return err
	}
	r.types = append(r.types, t)
	fmt.Fprintf(r.out, "type %d added.\n", len(r.types)-1)
	return nil
}

func (r *REPL) reset() {
	r.instrs = nil
	r.codeLen = 0
	r.constants = nil
	r.types = nil
	fmt.Fprintln(r.out, "reset.")
}

func (r *REPL) show() {
	if len(r.instrs) == 0 && len(r.constants) == 0 && len(r.types) == 0 {
		fmt.Fprintln(r.out, "(empty)")
		return
	}
	fmt.Fprint(r.out, r.build().String())
}

func (r *REPL) build(extra ...instr.Instruction) *program.Program {
	return program.New(
		append(r.instrs, extra...),
		program.WithConstants(r.constants...),
		program.WithTypes(r.types...),
	)
}

func (r *REPL) commit(inst instr.Instruction) {
	r.instrs = append(r.instrs, inst)
	r.codeLen += len(inst)
}

func (r *REPL) parse(line string) (instr.Instruction, error) {
	normalized, err := normalize(line, r.codeLen)
	if err != nil {
		return nil, err
	}
	return instr.Parse(normalized)
}

func (r *REPL) printErr(err error) {
	fmt.Fprintf(r.out, "error: %v\n", err)
}

func printStack(out io.Writer, vm *interp.Interpreter) {
	n := vm.Len()
	if n == 0 {
		return
	}
	parts := make([]string, n)
	for i := 0; i < n; i++ {
		v, _ := vm.Peek(i)
		parts[n-1-i] = format(v, vm)
	}
	fmt.Fprintln(out, strings.Join(parts, " "))
}

// format resolves KindRef through the heap (shows actual object, not raw index),
// truncates multi-line values to the first line, and adds type suffixes to i64/f32/f64.
func format(v types.Boxed, vm *interp.Interpreter) string {
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
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[:i]
		}
		return s
	default:
		return "<invalid>"
	}
}

// normalize converts "@N" absolute byte targets in branch instructions to relative
// offsets from ip, and strips any "NNNN:\t" offset prefix. Returns the line unchanged
// if no "@" tokens are present.
func normalize(line string, ip int) (string, error) {
	if _, after, ok := strings.Cut(line, ":\t"); ok {
		line = after
	}
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return line, nil
	}
	op := strings.ToLower(fields[0])
	switch op {
	case "br", "br_if":
		if len(fields) < 2 || !strings.HasPrefix(fields[1], "@") {
			return line, nil
		}
		abs, err := parseInt(fields[1][1:])
		if err != nil {
			return "", fmt.Errorf("invalid absolute branch target %s: %w", fields[1], err)
		}
		const width = 3
		rel := abs - (ip + width)
		if rel < 0 || rel > 0xFFFF {
			return "", fmt.Errorf("branch target %s out of range from offset %d", fields[1], ip)
		}
		return fmt.Sprintf("%s %d", fields[0], rel), nil

	case "br_table":
		if len(fields) < 2 {
			return line, nil
		}
		count, err := parseInt(fields[1])
		if err != nil {
			return "", fmt.Errorf("invalid br_table count %s: %w", fields[1], err)
		}
		width := 4 + count*2
		tokens := make([]string, len(fields))
		copy(tokens, fields)
		changed := false
		for i, f := range fields[2:] {
			if !strings.HasPrefix(f, "@") {
				continue
			}
			abs, err := parseInt(f[1:])
			if err != nil {
				return "", fmt.Errorf("invalid absolute branch target %s: %w", f, err)
			}
			rel := abs - (ip + width)
			if rel < 0 || rel > 0xFFFF {
				return "", fmt.Errorf("branch target %s out of range from offset %d", f, ip)
			}
			tokens[2+i] = fmt.Sprintf("%d", rel)
			changed = true
		}
		if !changed {
			return line, nil
		}
		return strings.Join(tokens, " "), nil
	}
	return line, nil
}

func parseInt(s string) (int, error) {
	v, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("expected integer, got %q", s)
	}
	return int(v), nil
}
