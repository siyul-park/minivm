package debug

import (
	"errors"
	"sort"

	"github.com/siyul-park/minivm/interp"
)

// Debugger pauses an interpreter at breakpoints and single-step boundaries through its Hook.
type Debugger struct {
	mode debugMode

	breakpoints map[int]*Breakpoint

	stop       *Stop
	skip       *skipPoint
	pauseDepth int
	depth      int

	next int
}

// Stop identifies where a debugger paused: function, instruction pointer, and the breakpoint ID that fired (0 for a step).
type Stop struct {
	Func       int
	IP         int
	Breakpoint int
}

// Breakpoint pauses execution at an instruction pointer of a function while enabled and its condition holds.
type Breakpoint struct {
	ID      int
	Func    int
	IP      int
	Enabled bool
	Hits    uint64
	Cond    func(*interp.Interpreter) bool
}

type debugMode int

// skipPoint marks the instruction a resumed debugger steps over once so it
// does not immediately re-trigger at the position it stopped on.
type skipPoint struct {
	fn    int
	ip    int
	depth int
}

const (
	debugContinue debugMode = iota
	debugStep
	debugNext
	debugFinish
)

// ErrStopped is returned by Hook when execution pauses.
var ErrStopped = errors.New("debug stopped")

// NewDebugger returns a debugger with no breakpoints that runs until the first one is hit.
func NewDebugger() *Debugger {
	return &Debugger{}
}

// Hook is the interpreter instruction hook; it returns ErrStopped when the position pauses.
func (d *Debugger) Hook(i *interp.Interpreter) error {
	fn, ip, fp := i.Func(), i.IP(), i.FP()
	if s := d.skip; s != nil {
		d.skip = nil
		if s.fn == fn && s.ip == ip && s.depth == fp {
			return nil
		}
	}

	if bp := d.breakpoint(i, fn, ip); bp != nil {
		bp.Hits++
		return d.pause(fn, ip, fp, bp.ID)
	}

	switch d.mode {
	case debugStep:
		return d.pause(fn, ip, fp, 0)
	case debugNext:
		if fp <= d.depth {
			return d.pause(fn, ip, fp, 0)
		}
	case debugFinish:
		if fp < d.depth {
			return d.pause(fn, ip, fp, 0)
		}
	}
	return nil
}

// Stop returns the last pause position, or the zero Stop when not paused.
func (d *Debugger) Stop() Stop {
	if d.stop == nil {
		return Stop{}
	}
	return *d.stop
}

// Reset clears the pause state and returns to continue mode; breakpoints remain.
func (d *Debugger) Reset() {
	d.mode = debugContinue
	d.stop = nil
	d.skip = nil
	d.pauseDepth = 0
	d.depth = 0
}

// Continue resumes until the next breakpoint.
func (d *Debugger) Continue() {
	d.mode = debugContinue
	d.resume()
}

// Step resumes until the next instruction.
func (d *Debugger) Step() {
	d.mode = debugStep
	d.resume()
}

// Next resumes until the next instruction at the current call depth or shallower, stepping over calls.
func (d *Debugger) Next() {
	d.mode = debugNext
	d.depth = d.stopDepth()
	d.resume()
}

// Finish resumes until the current function returns.
func (d *Debugger) Finish() {
	d.mode = debugFinish
	d.depth = d.stopDepth()
	d.resume()
}

// Break adds an unconditional breakpoint and returns its ID.
func (d *Debugger) Break(fn, ip int) int {
	return d.BreakIf(fn, ip, nil)
}

// BreakIf adds a breakpoint that pauses only when cond reports true, and returns its ID.
func (d *Debugger) BreakIf(fn, ip int, cond func(*interp.Interpreter) bool) int {
	d.init()
	id := d.next
	d.next++
	d.breakpoints[id] = &Breakpoint{
		ID:      id,
		Func:    fn,
		IP:      ip,
		Enabled: true,
		Cond:    cond,
	}
	return id
}

// Clear removes the breakpoint id and reports whether it existed.
func (d *Debugger) Clear(id int) bool {
	if d.lookup(id) == nil {
		return false
	}
	delete(d.breakpoints, id)
	return true
}

// Enable sets whether the breakpoint id can fire and reports whether it exists.
func (d *Debugger) Enable(id int, enabled bool) bool {
	bp := d.lookup(id)
	if bp == nil {
		return false
	}
	bp.Enabled = enabled
	return true
}

// Breakpoints returns a snapshot of all breakpoints ordered by ID.
func (d *Debugger) Breakpoints() []Breakpoint {
	d.init()
	out := make([]Breakpoint, 0, len(d.breakpoints))
	for _, bp := range d.breakpoints {
		out = append(out, *bp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func (d *Debugger) lookup(id int) *Breakpoint {
	d.init()
	return d.breakpoints[id]
}

func (d *Debugger) breakpoint(i *interp.Interpreter, fn, ip int) *Breakpoint {
	d.init()
	var hit *Breakpoint
	for _, bp := range d.breakpoints {
		if !bp.Enabled || bp.Func != fn || bp.IP != ip {
			continue
		}
		if bp.Cond != nil && !bp.Cond(i) {
			continue
		}
		if hit == nil || bp.ID < hit.ID {
			hit = bp
		}
	}
	return hit
}

func (d *Debugger) pause(fn, ip, depth, bp int) error {
	d.stop = &Stop{
		Func:       fn,
		IP:         ip,
		Breakpoint: bp,
	}
	d.pauseDepth = depth
	d.mode = debugContinue
	return ErrStopped
}

func (d *Debugger) resume() {
	if d.stop == nil {
		return
	}
	d.skip = &skipPoint{
		fn:    d.stop.Func,
		ip:    d.stop.IP,
		depth: d.stopDepth(),
	}
	d.stop = nil
}

func (d *Debugger) stopDepth() int {
	if d.pauseDepth > 0 {
		return d.pauseDepth
	}
	return 1
}

func (d *Debugger) init() {
	if d.breakpoints == nil {
		d.breakpoints = make(map[int]*Breakpoint)
	}
	if d.next == 0 {
		d.next = 1
	}
}
