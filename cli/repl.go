package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/siyul-park/minivm/debug"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

const prompt = "> "
const blockPrompt = "... "
const debugPrompt = "debug> "

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
  .profile            profile accumulated program
  .load <file>        replace REPL state with the program parsed from <file>
  .save <file>        write the current program (code, constants, types) to <file>
  .reset              clear all accumulated instructions, stack, constants, types, and breakpoints

Debug commands:
  .break <ip>         set breakpoint at bytecode offset ip (func 0)
  .break <fn>:<ip>    set breakpoint at func fn, offset ip
  .breaks             list all breakpoints
  .clear <id>         remove breakpoint by ID
  .enable <id>        enable a breakpoint
  .disable <id>       disable a breakpoint
  .debug              run accumulated program in debug mode (stops at first instruction)
                        In debug mode:
                          step/s       execute one instruction (entering calls)
                          next/n       execute one instruction (stepping over calls)
                          finish/f     run until current frame returns
                          continue/c   run until next breakpoint or end
                          stack        show operand stack
                          locals       show local variables
                          globals      show global variables
                          frames       show call stack
                          breaks       list breakpoints
                          break <spec> add breakpoint
                          clear <id>   remove breakpoint
                          quit/q       exit debug session

  .help               show this help
  .quit  /  .exit     exit the REPL
`

// REPL evaluates one assembly instruction per iteration, rebuilding a
// program from accumulated history and re-running it through a fresh
// interpreter each step.
type REPL struct {
	in        io.Reader
	out       io.Writer
	fs        WriteFS
	instrs    []instr.Instruction
	codeLen   int // byte length of instr.Marshal(instrs); updated incrementally
	constants []types.Value
	types     []types.Type
	debugger  *debug.Debugger // nil until first .break; breakpoint storage only
}

// NewREPL returns a new REPL that reads from in and writes to out. fs
// backs .load and .save; pass nil to disable those commands (callers
// that need them typically pass OS()).
func NewREPL(in io.Reader, out io.Writer, fs WriteFS) *REPL {
	return &REPL{in: in, out: out, fs: fs}
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
			done, err := r.command(ctx, scanner, line)
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

func (r *REPL) command(ctx context.Context, scanner *bufio.Scanner, line string) (bool, error) {
	cmd, arg, _ := strings.Cut(strings.TrimSpace(line), " ")
	arg = strings.TrimSpace(arg)

	switch strings.ToLower(cmd) {
	case ".quit", ".exit":
		fmt.Fprintln(r.out, "bye")
		return true, nil
	case ".reset":
		r.clear()
		fmt.Fprintln(r.out, "reset.")
	case ".show":
		r.show()
	case ".profile":
		if err := r.profile(ctx); err != nil {
			r.printErr(err)
		}
	case ".load":
		if err := r.load(arg); err != nil {
			r.printErr(err)
		}
	case ".save":
		if err := r.save(arg); err != nil {
			r.printErr(err)
		}
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
	case ".break", ".b":
		if err := r.doBreak(arg); err != nil {
			r.printErr(err)
		}
	case ".breaks":
		r.printBreakpoints()
	case ".clear":
		if err := r.doClear(arg); err != nil {
			r.printErr(err)
		}
	case ".enable":
		if err := r.doEnable(arg, true); err != nil {
			r.printErr(err)
		}
	case ".disable":
		if err := r.doEnable(arg, false); err != nil {
			r.printErr(err)
		}
	case ".debug":
		if err := r.doDebug(ctx, scanner); err != nil {
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

func (r *REPL) clear() {
	r.instrs = nil
	r.codeLen = 0
	r.constants = nil
	r.types = nil
	r.debugger = nil
}

// load replaces REPL state with the program parsed from path. Merging
// into existing state would require renumbering instruction-embedded
// constant and type indices; replace keeps the semantics simple and
// matches what users expect from a "load this file" command.
func (r *REPL) load(path string) error {
	if path == "" {
		return fmt.Errorf("usage: .load <file>")
	}
	if r.fs == nil {
		return fmt.Errorf(".load disabled: no filesystem configured")
	}
	file, err := r.fs.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()

	prog, err := program.Parse(file)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	r.clear()
	r.instrs = instr.Unmarshal(prog.Code)
	r.codeLen = len(prog.Code)
	r.constants = append([]types.Value(nil), prog.Constants...)
	r.types = append([]types.Type(nil), prog.Types...)
	fmt.Fprintf(r.out, "loaded %s\n", path)
	return nil
}

// save writes the current program to path in Program.String() format,
// which program.Parse can read back. Host values (HostFunction,
// HostObject) have no textual form, so a program containing them cannot
// round-trip; reject the save before producing an unreadable file.
func (r *REPL) save(path string) error {
	if path == "" {
		return fmt.Errorf("usage: .save <file>")
	}
	if r.fs == nil {
		return fmt.Errorf(".save disabled: no filesystem configured")
	}
	for i, c := range r.constants {
		if _, ok := c.(*interp.HostFunction); ok {
			return fmt.Errorf("cannot save: constant %d is a host function (no textual form)", i)
		}
		if _, ok := c.(*interp.HostObject); ok {
			return fmt.Errorf("cannot save: constant %d is a host object (no textual form)", i)
		}
	}

	file, err := r.fs.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer file.Close()

	if _, err := io.WriteString(file, r.build().String()); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(r.out, "saved %s\n", path)
	return nil
}

func (r *REPL) show() {
	if len(r.instrs) == 0 && len(r.constants) == 0 && len(r.types) == 0 {
		fmt.Fprintln(r.out, "(empty)")
		return
	}
	fmt.Fprint(r.out, r.build().String())
}

func (r *REPL) profile(ctx context.Context) error {
	if len(r.instrs) == 0 {
		fmt.Fprintln(r.out, "(empty)")
		return nil
	}

	p := prof.New()
	vm := interp.New(r.build(), interp.WithProfiler(p), interp.WithTick(1))
	if err := vm.Run(ctx); err != nil {
		_ = vm.Close()
		return err
	}
	if err := vm.Close(); err != nil {
		return err
	}

	newProfileReport(p.Metrics()).print(r.out)
	return nil
}

func (r *REPL) doBreak(spec string) error {
	if spec == "" {
		return fmt.Errorf("usage: .break <ip> or .break <fn>:<ip>")
	}
	fn, ip, err := parseBreakSpec(spec)
	if err != nil {
		return err
	}
	r.ensureDebugger()
	id := r.debugger.Break(fn, ip)
	fmt.Fprintf(r.out, "breakpoint %d set at func=%d ip=%d\n", id, fn, ip)
	return nil
}

func (r *REPL) doClear(arg string) error {
	if arg == "" {
		return fmt.Errorf("usage: .clear <id>")
	}
	id, err := parseInt(arg)
	if err != nil {
		return fmt.Errorf("invalid breakpoint id %q: %w", arg, err)
	}
	r.ensureDebugger()
	if !r.debugger.Clear(id) {
		return fmt.Errorf("breakpoint %d not found", id)
	}
	fmt.Fprintf(r.out, "breakpoint %d cleared\n", id)
	return nil
}

func (r *REPL) doEnable(arg string, on bool) error {
	if arg == "" {
		verb := "enable"
		if !on {
			verb = "disable"
		}
		return fmt.Errorf("usage: .%s <id>", verb)
	}
	id, err := parseInt(arg)
	if err != nil {
		return fmt.Errorf("invalid breakpoint id %q: %w", arg, err)
	}
	r.ensureDebugger()
	if !r.debugger.Enable(id, on) {
		return fmt.Errorf("breakpoint %d not found", id)
	}
	state := "enabled"
	if !on {
		state = "disabled"
	}
	fmt.Fprintf(r.out, "breakpoint %d %s\n", id, state)
	return nil
}

func (r *REPL) printBreakpoints() {
	if r.debugger == nil {
		fmt.Fprintln(r.out, "no breakpoints")
		return
	}
	bps := r.debugger.Breakpoints()
	if len(bps) == 0 {
		fmt.Fprintln(r.out, "no breakpoints")
		return
	}
	for _, bp := range bps {
		state := "enabled"
		if !bp.Enabled {
			state = "disabled"
		}
		fmt.Fprintf(r.out, "breakpoint %d: func=%d ip=%d %s hits=%d\n", bp.ID, bp.Func, bp.IP, state, bp.Hits)
	}
}

func (r *REPL) doDebug(ctx context.Context, scanner *bufio.Scanner) error {
	if len(r.instrs) == 0 {
		fmt.Fprintln(r.out, "(empty)")
		return nil
	}

	dbg := debug.NewDebugger()
	if r.debugger != nil {
		for _, bp := range r.debugger.Breakpoints() {
			if bp.Enabled {
				dbg.Break(bp.Func, bp.IP)
			}
		}
	}
	dbg.Step()

	vm := interp.New(r.build(), interp.WithHook(dbg.Hook), interp.WithTick(1), interp.WithThreshold(-1))
	defer vm.Close()

	for {
		err := vm.Run(ctx)
		if errors.Is(err, debug.ErrStopped) {
			r.printStop(dbg.Stop(), vm)
			done, loopErr := r.debugLoop(ctx, scanner, vm, dbg)
			if loopErr != nil {
				return loopErr
			}
			if done {
				fmt.Fprintln(r.out, "debug session ended")
				return nil
			}
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	printStack(r.out, vm)
	return nil
}

func (r *REPL) debugLoop(ctx context.Context, scanner *bufio.Scanner, vm *interp.Interpreter, dbg *debug.Debugger) (done bool, err error) {
	for {
		fmt.Fprint(r.out, debugPrompt)
		if !scanner.Scan() {
			return true, scanner.Err()
		}

		line := strings.TrimSpace(scanner.Text())
		cmd, arg, _ := strings.Cut(line, " ")
		arg = strings.TrimSpace(arg)

		switch strings.ToLower(cmd) {
		case "step", "s":
			dbg.Step()
			return false, nil
		case "next", "n":
			dbg.Next()
			return false, nil
		case "finish", "f":
			dbg.Finish()
			return false, nil
		case "continue", "c":
			dbg.Continue()
			return false, nil
		case "stack":
			printStack(r.out, vm)
		case "locals":
			printIndexed(r.out, vm, "local", vm.Local)
		case "globals":
			printIndexed(r.out, vm, "global", vm.Global)
		case "frames":
			printFrames(r.out, vm)
		case "breaks":
			r.printBreakpoints()
		case "break", "b":
			if arg == "" {
				r.printErr(fmt.Errorf("usage: break <ip> or break <fn>:<ip>"))
				continue
			}
			fn, ip, perr := parseBreakSpec(arg)
			if perr != nil {
				r.printErr(perr)
				continue
			}
			r.ensureDebugger()
			rid := r.debugger.Break(fn, ip)
			dbg.Break(fn, ip)
			fmt.Fprintf(r.out, "breakpoint %d set at func=%d ip=%d\n", rid, fn, ip)
		case "clear":
			if arg == "" {
				r.printErr(fmt.Errorf("usage: clear <id>"))
				continue
			}
			id, perr := parseInt(arg)
			if perr != nil {
				r.printErr(fmt.Errorf("invalid breakpoint id %q: %w", arg, perr))
				continue
			}
			r.ensureDebugger()
			if !r.debugger.Clear(id) {
				r.printErr(fmt.Errorf("breakpoint %d not found", id))
				continue
			}
			fmt.Fprintf(r.out, "breakpoint %d cleared\n", id)
		case "quit", "exit", "q":
			return true, nil
		case "":
			// empty line: re-print current location
			r.printStop(dbg.Stop(), vm)
		default:
			fmt.Fprintf(r.out, "unknown debug command: %q (step/next/finish/continue/stack/locals/globals/frames/breaks/break/clear/quit)\n", line)
		}
	}
}

func (r *REPL) printStop(stop debug.Stop, vm *interp.Interpreter) {
	if stop.Breakpoint != 0 {
		fmt.Fprintf(r.out, "breakpoint %d at func=%d ip=%04d", stop.Breakpoint, stop.Func, stop.IP)
	} else {
		fmt.Fprintf(r.out, "stopped at func=%d ip=%04d", stop.Func, stop.IP)
	}
	if op, err := vm.Opcode(); err == nil {
		if typ := instr.TypeOf(op); typ.Mnemonic != "" {
			fmt.Fprintf(r.out, " (%s)", typ.Mnemonic)
		}
	}
	fmt.Fprintln(r.out)
}

func (r *REPL) ensureDebugger() {
	if r.debugger == nil {
		r.debugger = debug.NewDebugger()
	}
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

func printIndexed(out io.Writer, vm *interp.Interpreter, label string, get func(int) (types.Boxed, error)) {
	var parts []string
	for i := 0; ; i++ {
		v, err := get(i)
		if err != nil {
			break
		}
		parts = append(parts, fmt.Sprintf("%s[%d]=%s", label, i, formatValue(v, vm)))
	}
	if len(parts) == 0 {
		fmt.Fprintf(out, "(no %ss)\n", label)
		return
	}
	fmt.Fprintln(out, strings.Join(parts, "\n"))
}

func printFrames(out io.Writer, vm *interp.Interpreter) {
	depth := vm.FP()
	if depth == 0 {
		fmt.Fprintln(out, "(no frames)")
		return
	}
	for n := 0; n < depth; n++ {
		fn, ip, _, err := vm.Frame(n)
		if err != nil {
			break
		}
		marker := " "
		if n == 0 {
			marker = ">"
		}
		fmt.Fprintf(out, "%s frame[%d] func=%d ip=%04d\n", marker, n, fn, ip)
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

func parseBreakSpec(spec string) (fn, ip int, err error) {
	if idx := strings.Index(spec, ":"); idx >= 0 {
		fn, err = parseInt(spec[:idx])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid function index: %w", err)
		}
		ip, err = parseInt(spec[idx+1:])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid bytecode offset: %w", err)
		}
	} else {
		ip, err = parseInt(spec)
		if err != nil {
			return 0, 0, fmt.Errorf("invalid bytecode offset: %w", err)
		}
	}
	return fn, ip, nil
}

func parseInt(s string) (int, error) {
	v, err := strconv.ParseInt(s, 0, 64)
	if err != nil {
		return 0, fmt.Errorf("expected integer, got %q", s)
	}
	return int(v), nil
}
