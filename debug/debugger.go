package debug

import (
	"errors"
	"sort"

	"github.com/siyul-park/minivm/interp"
)

type Stop struct {
	Func       int
	IP         int
	Breakpoint int
}

type Breakpoint struct {
	ID      int
	Func    int
	IP      int
	Enabled bool
	Hits    uint64
	Cond    func(*interp.Interpreter) bool
}

type Debugger struct {
	mode debugMode

	breakpoints map[int]*Breakpoint

	stop        *Stop
	skip        *skipPoint
	pausedDepth int
	depth       int

	next int
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

var ErrStopped = errors.New("debug stopped")

func NewDebugger() *Debugger {
	return &Debugger{
		breakpoints: make(map[int]*Breakpoint),
		next:        1,
	}
}

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
		return d.stopped(fn, ip, fp, bp.ID)
	}

	switch d.mode {
	case debugStep:
		return d.stopped(fn, ip, fp, 0)
	case debugNext:
		if fp <= d.depth {
			return d.stopped(fn, ip, fp, 0)
		}
	case debugFinish:
		if fp < d.depth {
			return d.stopped(fn, ip, fp, 0)
		}
	}
	return nil
}

func (d *Debugger) Stop() Stop {
	if d.stop == nil {
		return Stop{}
	}
	return *d.stop
}

func (d *Debugger) Continue() {
	d.mode = debugContinue
	d.resume()
}

func (d *Debugger) Step() {
	d.mode = debugStep
	d.resume()
}

func (d *Debugger) Next() {
	d.mode = debugNext
	d.depth = d.stopDepth()
	d.resume()
}

func (d *Debugger) Finish() {
	d.mode = debugFinish
	d.depth = d.stopDepth()
	d.resume()
}

func (d *Debugger) Break(fn, ip int) int {
	return d.BreakIf(fn, ip, nil)
}

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

func (d *Debugger) Clear(id int) bool {
	d.init()
	if _, ok := d.breakpoints[id]; !ok {
		return false
	}
	delete(d.breakpoints, id)
	return true
}

func (d *Debugger) Enable(id int, enabled bool) bool {
	d.init()
	bp := d.breakpoints[id]
	if bp == nil {
		return false
	}
	bp.Enabled = enabled
	return true
}

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

func (d *Debugger) stopped(fn, ip, depth, bp int) error {
	d.stop = &Stop{
		Func:       fn,
		IP:         ip,
		Breakpoint: bp,
	}
	d.pausedDepth = depth
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
	if d.pausedDepth > 0 {
		return d.pausedDepth
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
