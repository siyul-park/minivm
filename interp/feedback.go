package interp

import (
	"maps"
	"slices"

	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/transform"
)

// feedback is what running code observed, per address: its profiles and a
// consistent snapshot of them for compile requests.
type feedback struct {
	// profiles is each address's current feedback: its dynamic CALL sites'
	// observed callees (see) and its refuted guard sites (record). An entry
	// is never mutated once installed; a change replaces it with the next
	// generation, so a snapshot of the table is a consistent view.
	profiles []*transform.Profile
	// view is the snapshot of profiles a Submit hands out, rebuilt after a
	// change.
	view transform.Profiles
}

// stale reports whether the generation of any profile in deps moved.
func (f *feedback) stale(deps []jit.Dep) bool {
	return slices.ContainsFunc(deps, func(d jit.Dep) bool {
		return d.Address < len(f.profiles) && f.profiles[d.Address] != nil && f.profiles[d.Address].Generation != d.Generation
	})
}

// see records the callee seen at the current dynamic CALL: i.fr's own
// address and ip (the CALL's own, per call's doc). A caller past construction
// (bound dynamically) is skipped. The transition is unset -> callee -> mixed
// (the zero Function) once a second, different callee is seen; it never
// moves back, and a mixed site keeps its signature until a callee of another
// one is seen. Only a transition revises the profile.
func (f *feedback) see(i *Interpreter, callee transform.Callee) {
	addr, ip := i.fr.addr, i.fr.ip
	if addr >= len(f.profiles) {
		return
	}
	callee.Type = i.function(callee.Function).Typ
	site, seen := f.current(addr).Callees[ip]
	next := callee
	switch {
	case !seen:
	case site == callee:
		return
	case site.Type != nil && !site.Type.Equals(callee.Type):
		next = transform.Callee{}
	default:
		next = transform.Callee{Type: site.Type}
	}
	if seen && next == site {
		return
	}
	p := f.revise(addr)
	p.Callees[ip] = next
}

// record notes exit's site, a guard that failed, so the next compile of its
// function translates the site generic (transform.Profile.Refuted). The
// site belongs to the function of the exit's frame, which is a member of the
// code that took the exit when it is not the code's root.
func (f *feedback) record(exit jit.Exit) {
	addr, ip := exit.Frame.Address, exit.Frame.IP
	if addr >= len(f.profiles) || f.current(addr).Refuted[ip] {
		return
	}
	f.revise(addr).Refuted[ip] = true
}

// revise installs a copy of addr's profile at the next generation and
// returns it for the caller to change before anything reads it.
func (f *feedback) revise(addr int) *transform.Profile {
	old := f.current(addr)
	p := &transform.Profile{Callees: maps.Clone(old.Callees), Refuted: maps.Clone(old.Refuted), Generation: old.Generation + 1}
	if p.Callees == nil {
		p.Callees = map[int]transform.Callee{}
	}
	if p.Refuted == nil {
		p.Refuted = map[int]bool{}
	}
	f.profiles[addr] = p
	f.view = nil
	return p
}

// generation is addr's current profile generation.
func (f *feedback) generation(addr int) uint64 {
	return f.current(addr).Generation
}

// current is addr's current profile, the zero one when none was
// recorded.
func (f *feedback) current(addr int) transform.Profile {
	if f.profiles[addr] == nil {
		return transform.Profile{}
	}
	return *f.profiles[addr]
}

// snapshot is the profile table a compile reads. It is never mutated after
// it is handed out: a revision replaces the entry it came from, not the
// snapshot.
func (f *feedback) snapshot() transform.Profiles {
	if f.view == nil {
		f.view = slices.Clone(f.profiles)
	}
	return f.view
}
