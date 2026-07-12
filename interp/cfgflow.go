package interp

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type cfgSlot struct {
	kind types.Kind
	sig  *types.FunctionType
}

// blockKinds computes the exact operand kinds needed to reload each CFG block.
// It intentionally accepts less than the verifier: values whose runtime kind
// cannot be represented statically keep the function on the trace/threaded path.
func blockKinds(fn *types.Function, blocks []*analysis.BasicBlock, constants []types.Boxed, globals []types.Kind, heap []types.Value) ([][]types.Kind, bool) {
	if len(blocks) == 0 {
		return nil, true
	}
	locals := localTypes(fn)
	states := make([][]cfgSlot, len(blocks))
	seen := make([]bool, len(blocks))
	seen[0] = true
	work := []int{0}
	for len(work) > 0 {
		idx := work[len(work)-1]
		work = work[:len(work)-1]
		state := append([]cfgSlot(nil), states[idx]...)
		if !applyBlockKinds(fn, locals, constants, globals, heap, blocks[idx], &state) {
			return nil, false
		}
		for _, succ := range blocks[idx].Succs {
			if !seen[succ] {
				seen[succ] = true
				states[succ] = append([]cfgSlot(nil), state...)
				work = append(work, succ)
				continue
			}
			if len(states[succ]) != len(state) {
				return nil, false
			}
			changed := false
			for j := range state {
				if states[succ][j].kind != state[j].kind {
					return nil, false
				}
				if states[succ][j].sig != nil && states[succ][j].sig != state[j].sig {
					states[succ][j].sig = nil
					changed = true
				}
			}
			if changed {
				work = append(work, succ)
			}
		}
	}

	out := make([][]types.Kind, len(blocks))
	for idx, state := range states {
		if !seen[idx] {
			return nil, false
		}
		out[idx] = make([]types.Kind, len(state))
		for j, slot := range state {
			out[idx][j] = slot.kind
		}
	}
	return out, true
}

func applyBlockKinds(fn *types.Function, locals []types.Type, constants []types.Boxed, globals []types.Kind, heap []types.Value, block *analysis.BasicBlock, state *[]cfgSlot) bool {
	for ip := block.Start; ip < block.End; {
		inst := instr.Instruction(fn.Code[ip:])
		if !applyKind(fn, locals, constants, globals, heap, state, inst) {
			return false
		}
		ip += inst.Width()
	}
	return true
}

func applyKind(fn *types.Function, locals []types.Type, constants []types.Boxed, globals []types.Kind, heap []types.Value, state *[]cfgSlot, inst instr.Instruction) bool {
	push := func(kind types.Kind, sig *types.FunctionType) { *state = append(*state, cfgSlot{kind: kind, sig: sig}) }
	pop := func(n int) bool {
		if len(*state) < n {
			return false
		}
		*state = (*state)[:len(*state)-n]
		return true
	}

	switch inst.Opcode() {
	case instr.NOP, instr.UNREACHABLE, instr.BR:
		return true
	case instr.LOCAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(locals) {
			return false
		}
		push(locals[idx].Kind(), funcSignature(locals[idx]))
		return true
	case instr.LOCAL_TEE:
		return len(*state) > 0
	case instr.UPVAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(fn.Captures) {
			return false
		}
		push(fn.Captures[idx].Kind(), funcSignature(fn.Captures[idx]))
		return true
	case instr.GLOBAL_GET:
		idx := int(inst.Operand(0))
		if idx >= len(globals) {
			return false
		}
		push(globals[idx], nil)
		return true
	case instr.GLOBAL_TEE:
		return len(*state) > 0
	case instr.CONST_GET:
		idx := int(inst.Operand(0))
		if idx >= len(constants) {
			return false
		}
		push(constants[idx].Kind(), constFuncSignature(constants[idx], heap))
		return true
	case instr.DUP:
		if len(*state) == 0 {
			return false
		}
		*state = append(*state, (*state)[len(*state)-1])
		return true
	case instr.SWAP:
		if len(*state) < 2 {
			return false
		}
		n := len(*state)
		(*state)[n-1], (*state)[n-2] = (*state)[n-2], (*state)[n-1]
		return true
	case instr.SELECT:
		if len(*state) < 3 {
			return false
		}
		n := len(*state)
		a, b := (*state)[n-2], (*state)[n-3]
		if a.kind != b.kind {
			return false
		}
		*state = (*state)[:n-3]
		push(a.kind, nil)
		return true
	case instr.CALL, instr.RETURN_CALL:
		if len(*state) == 0 {
			return false
		}
		sig := (*state)[len(*state)-1].sig
		if sig == nil || !pop(1+len(sig.Params)) {
			return false
		}
		if inst.Opcode() == instr.CALL {
			for _, typ := range sig.Returns {
				push(typ.Kind(), funcSignature(typ))
			}
		}
		return true
	case instr.RETURN:
		return true
	case instr.STRUCT_NEW, instr.MAP_NEW, instr.CLOSURE_NEW:
		return false
	}

	t := inst.Type()
	if t.Pop == nil && t.Push == nil || !pop(len(t.Pop)) {
		return false
	}
	for _, kind := range t.Push {
		if kind == instr.KindAny {
			return false
		}
		push(types.Kind(kind), nil)
	}
	return true
}
