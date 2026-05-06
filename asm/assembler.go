package asm

import (
	"fmt"
	"unsafe"
)

// reservedSlot is a metadata slot allocated via Reserve, pinned to a canonical register.
type reservedSlot struct {
	typ  RegType
	w    RegWidth
	vreg VReg
}

// globalLabelEntry records where a label landed after a Compile call.
type globalLabelEntry struct {
	chunk  *Chunk
	offset int // byte offset from chunk start
}

type Assembler struct {
	arch      *Arch
	vregAlloc *VRegAlloc
	regAlloc  *RegAlloc
	buffer    *Buffer
	stack     []VReg
	params    []VReg
	insts     []Instruction

	// Survive Reset — shared across all blocks in a function.
	nextLabel    int
	globalLabels map[int]globalLabelEntry

	// Reset with each block.
	localLabels map[int]int // labelID → index in a.insts (set by PlaceLabel)
	reserved    []reservedSlot
}

func NewAssembler(arch *Arch, buffer *Buffer) *Assembler {
	return &Assembler{
		arch:         arch,
		vregAlloc:    NewVRegAlloc(),
		regAlloc:     NewRegAlloc(arch.Registers),
		buffer:       buffer,
		globalLabels: make(map[int]globalLabelEntry),
	}
}

// NewLabel returns a globally unique label ID. IDs survive Reset.
func (a *Assembler) NewLabel() int {
	id := a.nextLabel
	a.nextLabel++
	return id
}

// PlaceLabel marks the current IR position as the definition of label id.
// Intra-block references are resolved immediately in Compile; cross-block
// references become Relocations resolved by Link.
func (a *Assembler) PlaceLabel(id int) {
	if a.localLabels == nil {
		a.localLabels = make(map[int]int)
	}
	a.localLabels[id] = len(a.insts) // points to next real instruction (0-byte pseudo)
	a.insts = append(a.insts, Instruction{Op: OpPseudoLabel, Dst: Label(id)})
}

// Reserve allocates a VReg pinned to the leading canonical register for its
// type (X0 for int, D0 for float).  It appears first in the Signature.Reserved
// slice so callers can write metadata (e.g. next IP) before each RET.
func (a *Assembler) Reserve(typ RegType, w RegWidth) VReg {
	vreg := a.vregAlloc.Alloc(typ, w)
	a.reserved = append(a.reserved, reservedSlot{typ: typ, w: w, vreg: vreg})
	return vreg
}

func (a *Assembler) Params() []VReg {
	return append([]VReg(nil), a.params...)
}

func (a *Assembler) Returns() []VReg {
	return append([]VReg(nil), a.stack...)
}

func (a *Assembler) NewVReg(typ RegType, w RegWidth) VReg {
	return a.vregAlloc.Alloc(typ, w)
}

func (a *Assembler) Take(typ RegType, w RegWidth) (VReg, bool) {
	if len(a.stack) == 0 {
		reg := a.vregAlloc.Alloc(typ, w)
		a.params = append(a.params, reg)
		return reg, true
	}
	last := a.stack[len(a.stack)-1]
	if last.Type() != typ || last.Width() != w {
		return VReg{}, false
	}
	a.stack = a.stack[:len(a.stack)-1]
	return last, true
}

func (a *Assembler) Top(i int) (VReg, bool) {
	if len(a.stack) <= i {
		return VReg{}, false
	}
	return a.stack[len(a.stack)-1-i], true
}

func (a *Assembler) Push(reg VReg) {
	a.stack = append(a.stack, reg)
}

func (a *Assembler) Pop() (VReg, bool) {
	if len(a.stack) == 0 {
		return VReg{}, false
	}
	last := a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
	return last, true
}

func (a *Assembler) Emit(inst Instruction) int {
	a.insts = append(a.insts, inst)
	return len(a.insts) - 1
}

// Emits appends multiple instructions in one call.
// Useful for instruction sequences returned by helpers such as LoadImm64.
func (a *Assembler) Emits(insts ...Instruction) {
	a.insts = append(a.insts, insts...)
}

// Compile encodes the current IR into a RelocObject for one block.
//
// Intra-block labels are resolved to PC-relative immediates immediately.
// Cross-block LabelOperands are preserved in RelocObject.Instrs and listed as
// Relocations; Link re-encodes them once target addresses are known.
//
// Per-block state is cleared by resetBlock; nextLabel and globalLabels survive
// so all blocks in a function share one label namespace.
func (a *Assembler) Compile() (*RelocObject, error) {
	physical, localBytes, relocs, err := a.resolveLabels()
	if err != nil {
		return nil, err
	}

	saved := a.insts
	a.insts = physical // assign() reads from a.insts

	sig, err := a.signature()
	if err != nil {
		a.insts = saved
		return nil, err
	}
	physAssigned, err := a.assign()
	if err != nil {
		a.insts = saved
		return nil, err
	}
	a.insts = saved

	// assign() preserves LabelOperands (they pass through rewrite unchanged).
	// Build an encodable copy with Imm(0) placeholder for each unresolved label
	// so the encoder sees a syntactically valid branch offset.
	encodable := make([]Instruction, len(physAssigned))
	copy(encodable, physAssigned)
	for i, inst := range encodable {
		if _, ok := inst.Src2.(LabelOperand); ok {
			encodable[i].Src2 = Imm(0)
		}
	}

	code, err := Encode(a.arch.Encoder, encodable)
	if err != nil {
		return nil, err
	}

	if err := a.buffer.Unseal(); err != nil {
		return nil, err
	}
	chunk, err := a.buffer.Append(code)
	if err != nil {
		return nil, err
	}
	if err := a.buffer.Seal(); err != nil {
		return nil, err
	}

	for id, off := range localBytes {
		a.globalLabels[id] = globalLabelEntry{chunk: chunk, offset: off}
	}

	a.resetBlock()

	return &RelocObject{
		Chunk:  chunk,
		Sig:    sig,
		Instrs: physAssigned,
		Labels: localBytes,
		Relocs: relocs,
	}, nil
}

// Link resolves all Relocations across the given objects and returns one Caller
// per object.  Objects whose labels cannot all be resolved get a nil Caller;
// the first such failure is returned as the error (others still succeed).
// After Link the buffer is sealed and callers can be invoked immediately.
func (a *Assembler) Link(objects []*RelocObject) ([]Caller, error) {
	if err := a.buffer.Unseal(); err != nil {
		return nil, err
	}

	var firstErr error
	failed := make([]bool, len(objects))

	for i, obj := range objects {
		base := uintptr(obj.Chunk.Ptr())
		for _, r := range obj.Relocs {
			entry, ok := a.globalLabels[r.Label]
			if !ok {
				if firstErr == nil {
					firstErr = fmt.Errorf("unresolved label %d", r.Label)
				}
				failed[i] = true
				continue
			}
			target := uintptr(entry.chunk.Ptr()) + uintptr(entry.offset)
			src := base + uintptr(r.Offset)
			off := int64(target) - int64(src)

			// Re-encode the instruction with the resolved offset.
			// This is architecture-agnostic: the encoder owns the bit layout.
			inst := obj.Instrs[r.InstrIdx]
			inst.Src2 = Imm(off)
			patched, err := a.arch.Encoder.Encode(inst)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				failed[i] = true
				continue
			}
			writeBytes(unsafe.Pointer(src), patched)
		}
	}

	if err := a.buffer.Seal(); err != nil {
		return nil, err
	}

	callers := make([]Caller, len(objects))
	for i, obj := range objects {
		if failed[i] {
			continue
		}
		caller, err := a.arch.NewCaller(obj.Sig, obj.Chunk)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		callers[i] = caller
	}
	return callers, firstErr
}

// Abort discards any partial compilation and resets per-block state.
// Call this when a block fails before Compile() is reached.
func (a *Assembler) Abort() {
	a.resetBlock()
}

// Reset fully resets the assembler, including the shared label namespace.
// Call this between function compilations (not between blocks of the same function).
func (a *Assembler) Reset() {
	a.resetBlock()
	a.nextLabel = 0
	a.globalLabels = make(map[int]globalLabelEntry)
}

// resolveLabels strips OpPseudoLabel pseudo-instructions, computes intra-block
// label byte offsets, resolves local LabelOperands to PC-relative immediates,
// and returns cross-block LabelOperands intact so that Link can re-encode them.
//
// Returns:
//
//	physical   – instruction list ready for assign() (ARM64: 4 bytes each)
//	localBytes – intra-block label positions (byte offset from chunk start)
//	relocs     – unresolved cross-block references
func (a *Assembler) resolveLabels() (physical []Instruction, localBytes map[int]int, relocs []Relocation, err error) {
	// Pass 1: compute byte position of each intra-block label.
	localBytes = make(map[int]int)
	bytePos := 0
	for _, inst := range a.insts {
		if inst.Op == OpPseudoLabel {
			if lbl, ok := inst.Dst.(LabelOperand); ok {
				localBytes[lbl.ID] = bytePos
			}
			continue
		}
		bytePos += 4
	}

	// Pass 2: strip pseudo-instructions, patch or record label references.
	instrIdx := 0
	bytePos = 0
	for _, inst := range a.insts {
		if inst.Op == OpPseudoLabel {
			continue
		}
		if lbl, ok := inst.Src2.(LabelOperand); ok {
			if off, found := localBytes[lbl.ID]; found {
				// Intra-block: resolve immediately.
				inst.Src2 = Imm(int64(off - bytePos))
			} else {
				// Cross-block: keep LabelOperand for re-encoding at Link time.
				relocs = append(relocs, Relocation{
					InstrIdx: instrIdx,
					Offset:   bytePos,
					Label:    lbl.ID,
				})
			}
		}
		physical = append(physical, inst)
		instrIdx++
		bytePos += 4
	}
	return
}

// resetBlock clears per-block state. nextLabel and globalLabels are preserved.
func (a *Assembler) resetBlock() {
	a.stack = a.stack[:0]
	a.params = a.params[:0]
	a.insts = a.insts[:0]
	a.localLabels = nil
	a.reserved = a.reserved[:0]
	if a.vregAlloc != nil {
		a.vregAlloc.Reset()
	}
	if a.regAlloc != nil {
		a.regAlloc.Reset()
	}
}

func (a *Assembler) signature() (*Signature, error) {
	nRes := len(a.reserved)
	nParams := len(a.params)
	nReturns := len(a.stack)
	if nRes+nParams > a.arch.ABI.MaxParams() {
		return nil, ErrTooManyParams
	}
	if nRes+nReturns > a.arch.ABI.MaxReturns() {
		return nil, ErrTooManyReturns
	}

	reserved := make([]RegType, nRes)
	reservedWidths := make([]RegWidth, nRes)
	for i, rs := range a.reserved {
		reserved[i] = rs.typ
		reservedWidths[i] = rs.w
	}

	params := make([]RegType, nParams)
	paramWidths := make([]RegWidth, nParams)
	for i, reg := range a.params {
		params[i] = reg.Type()
		paramWidths[i] = reg.Width()
	}

	returns := make([]RegType, nReturns)
	returnWidths := make([]RegWidth, nReturns)
	for i, reg := range a.stack {
		returns[i] = reg.Type()
		returnWidths[i] = reg.Width()
	}

	return &Signature{
		Reserved:       reserved,
		ReservedWidths: reservedWidths,
		Params:         params,
		ParamWidths:    paramWidths,
		Returns:        returns,
		ReturnWidths:   returnWidths,
	}, nil
}

func (a *Assembler) assign() ([]Instruction, error) {
	last := make(map[int32]int)
	for i, inst := range a.insts {
		for _, v := range a.srcs(inst) {
			last[v.ID()] = i
		}
	}

	intRegs := a.allocatable(RegTypeInt)
	floatRegs := a.allocatable(RegTypeFloat)

	// physical maps VReg.ID → PReg (WidthUndefined; Width is taken from VReg at rewrite time)
	physical := make(map[int32]PReg)
	virtual := make(map[uint8]VReg)
	fixed := make(map[int32]PReg)

	// Reserved output slots occupy leading canonical registers for their type.
	intR, floatR := 0, 0
	for _, rs := range a.reserved {
		var p PReg
		if rs.typ == RegTypeFloat {
			p = floatRegs[floatR]
			floatR++
		} else {
			p = intRegs[intR]
			intR++
		}
		fixed[rs.vreg.ID()] = p
	}
	// Stack outputs follow reserved slots.
	for _, v := range a.stack {
		var p PReg
		if v.Type() == RegTypeFloat {
			p = floatRegs[floatR]
			floatR++
		} else {
			p = intRegs[intR]
			intR++
		}
		fixed[v.ID()] = p
	}

	intP, floatP := 0, 0
	for _, v := range a.params {
		var p PReg
		if v.Type() == RegTypeFloat {
			if floatP >= a.arch.ABI.MaxParams() {
				return nil, ErrTooManyParams
			}
			p = floatRegs[floatP]
			floatP++
		} else {
			if intP >= a.arch.ABI.MaxParams() {
				return nil, ErrTooManyParams
			}
			p = intRegs[intP]
			intP++
		}

		if err := a.regAlloc.Reserve(v, p); err != nil {
			return nil, err
		}

		physical[v.ID()] = p
		virtual[p.ID()] = v
	}

	for i, inst := range a.insts {
		for _, v := range a.srcs(inst) {
			if _, ok := physical[v.ID()]; ok {
				continue
			}

			p, err := a.regAlloc.Alloc(v)
			if err != nil {
				return nil, err
			}

			physical[v.ID()] = p
			virtual[p.ID()] = v
		}

		if dst, ok := a.dst(inst); ok {
			if _, exists := physical[dst.ID()]; !exists {
				if want, ok := fixed[dst.ID()]; ok {
					owner, occupied := virtual[want.ID()]
					fix := false
					if owner.ID() != 0 {
						if _, ok := fixed[owner.ID()]; ok {
							fix = true
						}
					}
					if !occupied || owner.ID() == dst.ID() || (last[owner.ID()] == i && !fix) {
						if occupied && owner.ID() != dst.ID() {
							a.regAlloc.Free(owner)
							delete(virtual, want.ID())
						}

						if err := a.regAlloc.Reserve(dst, want); err == nil {
							physical[dst.ID()] = want
							virtual[want.ID()] = dst
							continue
						}
					}
				}

				p, err := a.regAlloc.Alloc(dst)
				if err != nil {
					return nil, err
				}

				physical[dst.ID()] = p
				virtual[p.ID()] = dst
			}
		}

		for _, v := range a.srcs(inst) {
			if last[v.ID()] != i {
				continue
			}
			if _, ok := fixed[v.ID()]; ok {
				continue
			}
			if p, ok := physical[v.ID()]; ok {
				a.regAlloc.Free(v)
				delete(physical, v.ID())
				delete(virtual, p.ID())
			}
		}
	}

	for vid, p := range fixed {
		if _, ok := physical[vid]; ok {
			continue
		}
		if err := a.regAlloc.Reserve(NewVReg(vid, p.Type(), p.Width()), p); err != nil {
			return nil, err
		}
		physical[vid] = p
	}

	widths := make(map[int32]RegWidth, len(physical))
	for _, v := range a.params {
		widths[v.ID()] = v.Width()
	}
	for _, v := range a.stack {
		widths[v.ID()] = v.Width()
	}
	for _, inst := range a.insts {
		for _, v := range a.srcs(inst) {
			if _, ok := widths[v.ID()]; !ok {
				widths[v.ID()] = v.Width()
			}
		}
		if dst, ok := a.dst(inst); ok {
			if _, ok := widths[dst.ID()]; !ok {
				widths[dst.ID()] = dst.Width()
			}
		}
	}

	out := make([]Instruction, 0, len(a.insts))
	for _, inst := range a.insts {
		out = append(out, a.rewrite(inst, physical, widths))
	}

	return out, nil
}

func (a *Assembler) allocatable(typ RegType) []PReg {
	mask := a.arch.Registers.Allocatable(typ)
	regs := make([]PReg, 0, mask.Count())
	for !mask.Empty() {
		var id uint8
		id, mask = mask.PopFirst()
		regs = append(regs, NewPReg(id, typ, WidthUndefined))
	}
	return regs
}

func (a *Assembler) srcs(inst Instruction) []VReg {
	var regs []VReg
	if r, ok := a.vreg(inst.Src1); ok {
		regs = append(regs, r)
	}
	if r, ok := a.vreg(inst.Src2); ok {
		regs = append(regs, r)
	}
	return regs
}

func (a *Assembler) dst(inst Instruction) (VReg, bool) {
	return a.vreg(inst.Dst)
}

func (a *Assembler) vreg(op Operand) (VReg, bool) {
	switch v := op.(type) {
	case VRegOperand:
		return v.Reg, true
	case MemOperand:
		if b, ok := v.Base.(VRegOperand); ok {
			return b.Reg, true
		}
	}
	return VReg{}, false
}

func (a *Assembler) rewrite(inst Instruction, mapping map[int32]PReg, widths map[int32]RegWidth) Instruction {
	return Instruction{
		Op:   inst.Op,
		Dst:  a.rewriteOP(inst.Dst, mapping, widths),
		Src1: a.rewriteOP(inst.Src1, mapping, widths),
		Src2: a.rewriteOP(inst.Src2, mapping, widths),
	}
}

// rewriteOP replaces a VRegOperand with the allocated PReg, preserving the
func (a *Assembler) rewriteOP(op Operand, mapping map[int32]PReg, widths map[int32]RegWidth) Operand {
	switch v := op.(type) {
	case VRegOperand:
		if p, ok := mapping[v.Reg.ID()]; ok {
			w := v.Reg.Width()
			if w == WidthUndefined {
				if ww, ok := widths[v.Reg.ID()]; ok {
					w = ww
				}
			}
			return P(NewPReg(p.ID(), p.Type(), w))
		}
		return v
	case MemOperand:
		base := v.Base
		if vr, ok := base.(VRegOperand); ok {
			if p, ok := mapping[vr.Reg.ID()]; ok {
				w := vr.Reg.Width()
				if w == WidthUndefined {
					if ww, ok := widths[vr.Reg.ID()]; ok {
						w = ww
					}
				}
				base = P(NewPReg(p.ID(), p.Type(), w))
			}
		}
		return Mem(base, v.Offset)
	default:
		return op
	}
}
