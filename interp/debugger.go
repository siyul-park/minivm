package interp

import (
	"errors"
	"sort"
)

var ErrStopped = errors.New("debug stopped")

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
	Cond    func(*Interpreter) bool
}

type Debugger struct {
	breakpoints map[int]*Breakpoint
	next        int
	mode        debugMode
	stop        Stop
	stoppedFlag bool
	skip        bool
	skipFunc    int
	skipIP      int
	skipDepth   int
	pausedDepth int
	depth       int
}

type debugMode int

const (
	debugContinue debugMode = iota
	debugStep
	debugNext
	debugFinish
)

func WithDebugger(d *Debugger) func(*option) {
	return func(o *option) {
		if d == nil {
			d = NewDebugger()
		}
		o.hook = d.Hook
		o.tick = 1
		o.threshold = -1
	}
}

func NewDebugger() *Debugger {
	return &Debugger{
		breakpoints: make(map[int]*Breakpoint),
		next:        1,
	}
}

func (d *Debugger) Hook(i *Interpreter) error {
	fn, ip, depth := i.Func(), i.IP(), i.FrameDepth()
	if d.skip && d.skipFunc == fn && d.skipIP == ip && d.skipDepth == depth {
		d.skip = false
		return nil
	}
	d.skip = false

	if bp := d.breakpoint(i, fn, ip); bp != nil {
		bp.Hits++
		return d.stopped(fn, ip, depth, bp.ID)
	}

	switch d.mode {
	case debugStep:
		return d.stopped(fn, ip, depth, 0)
	case debugNext:
		if depth <= d.depth {
			return d.stopped(fn, ip, depth, 0)
		}
	case debugFinish:
		if depth < d.depth {
			return d.stopped(fn, ip, depth, 0)
		}
	}
	return nil
}

func (d *Debugger) Stop() Stop {
	return d.stop
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

func (d *Debugger) BreakIf(fn, ip int, cond func(*Interpreter) bool) int {
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

func (d *Debugger) breakpoint(i *Interpreter, fn, ip int) *Breakpoint {
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
	d.stop = Stop{
		Func:       fn,
		IP:         ip,
		Breakpoint: bp,
	}
	d.stoppedFlag = true
	d.pausedDepth = depth
	d.mode = debugContinue
	return ErrStopped
}

func (d *Debugger) resume() {
	if !d.stoppedFlag {
		return
	}
	d.skip = true
	d.skipFunc = d.stop.Func
	d.skipIP = d.stop.IP
	d.skipDepth = d.stopDepth()
	d.stop = Stop{}
	d.stoppedFlag = false
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
