package jit

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// jump builds an edge naming the anchor at addr:ip, unresolved until wire or
// store binds it to a block index.
func jump(addr, ip int) Edge {
	return Edge{Anchor: Anchor{Addr: addr, IP: ip}, Index: NoBlock}
}

// jumps builds one unresolved edge per target IP, all within addr.
func jumps(addr int, ips []int) []Edge {
	edges := make([]Edge, len(ips))
	for i, ip := range ips {
		edges[i] = jump(addr, ip)
	}
	return edges
}

// wire resolves every edge still pointing at NoBlock to the block roots names
// its anchor, leaving an edge whose anchor has no root (a cold exit out of
// the plan) unresolved.
func wire(p *Plan, roots map[Anchor]int) {
	for id := range p.Blocks {
		for i := range p.Blocks[id].Term.Edges {
			edge := &p.Blocks[id].Term.Edges[i]
			if edge.Index != NoBlock {
				continue
			}
			if target, ok := roots[edge.Anchor]; ok {
				edge.Index = target
			}
		}
	}
}

// carried returns the inline scalar locals that a call-free native loop may
// keep authoritative in registers until an exit. The blocks must contain a
// real backward edge; straight-line prefixes keep the VM slots authoritative.
// Refs and i64s stay slot-backed because their load and ownership guards can
// deopt while the register set is only partly prepared.
//
// A bridge among the blocks disqualifies them all: a bridge cycle re-enters
// through a fresh external Call, which never runs the loop-carry prologue, so
// a carried register would be uninitialized garbage on resume.
func carried(fn *types.Function, blocks []Block) []int {
	if fn == nil {
		return nil
	}
	for _, block := range blocks {
		if block.Bridge {
			return nil
		}
	}
	type loopRange struct {
		addr       int
		start, end int
	}
	var loops []loopRange
	for _, block := range blocks {
		for _, edge := range block.Term.Edges {
			if edge.Index != NoBlock && edge.Anchor.Addr == block.Anchor.Addr && edge.Anchor.IP <= block.Anchor.IP {
				loops = append(loops, loopRange{addr: block.Anchor.Addr, start: edge.Anchor.IP, end: block.Anchor.IP})
			}
		}
	}
	if len(loops) == 0 {
		return nil
	}

	locals := fn.Slots()
	read := make([]bool, len(locals))
	written := make([]bool, len(locals))
	for _, block := range blocks {
		inside := false
		for _, loop := range loops {
			if block.Anchor.Addr == loop.addr && block.Anchor.IP >= loop.start && block.Anchor.IP <= loop.end {
				inside = true
				break
			}
		}
		for _, step := range block.Steps {
			if instr.IsCall(step.Op) {
				return nil
			}
			if !inside {
				continue
			}
			switch step.Op {
			case instr.LOCAL_GET:
				local := int(step.Args[0])
				if local >= 0 && local < len(read) {
					read[local] = true
				}
			case instr.LOCAL_SET, instr.LOCAL_TEE:
				local := int(step.Args[0])
				if local >= 0 && local < len(written) {
					written[local] = true
				}
			}
		}
	}

	var carried []int
	for local, ok := range written {
		if !ok || !read[local] || local > MaxHoistSlot {
			continue
		}
		switch locals[local] {
		case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64:
			carried = append(carried, local)
		}
	}
	if len(carried) > maxCarried {
		return nil
	}
	return carried
}
