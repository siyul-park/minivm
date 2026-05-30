package asm

import (
	"errors"
	"fmt"
	"unsafe"
)

// Resolver returns the runtime address of a label bound outside the current
// set of Codes (typically: a previously linked Code, an indirection slot,
// or a host function). Implementations may return ErrUnresolvedLabel to
// signal a hard miss.
type Resolver func(Label) (unsafe.Pointer, error)

var ErrUnresolvedLabel = errors.New("unresolved label")

// Link installs each Code into buf, patches its external relocations using
// resolve, and constructs one Callable per Code via the architecture's ABI.
//
// The order of returned Callables matches the order of codes.
func Link(buf *Buffer, arch Arch, codes []*Code, resolve Resolver) ([]Callable, error) {
	if buf == nil {
		return nil, fmt.Errorf("%w: nil buffer", ErrInvalidArgs)
	}

	bases := make([]unsafe.Pointer, len(codes))
	for i, c := range codes {
		base, err := buf.Write(c.Bytes)
		if err != nil {
			return nil, err
		}
		bases[i] = base
	}

	if err := patchExternalRelocs(buf, arch, codes, bases, resolve); err != nil {
		return nil, err
	}

	callables := make([]Callable, len(codes))
	for i, c := range codes {
		callable, err := arch.ABI().NewCallable(c.Signature, bases[i])
		if err != nil {
			return nil, err
		}
		callables[i] = callable
	}
	return callables, nil
}

// patchExternalRelocs re-encodes every Relocation whose target lives
// outside the corresponding Code and overwrites the placeholder bytes in
// the buffer. Targets resolve in this priority order: (1) a label bound in
// any of the freshly linked Codes, (2) the provided resolve callback.
func patchExternalRelocs(buf *Buffer, arch Arch, codes []*Code, bases []unsafe.Pointer, resolve Resolver) error {
	external := make(map[Label]unsafe.Pointer)
	for i, c := range codes {
		for id, off := range c.Labels {
			external[id] = unsafe.Add(bases[i], off)
		}
	}

	enc := arch.Encoder()
	for i, c := range codes {
		for _, rel := range c.Relocs {
			target, ok := external[rel.Label]
			if !ok {
				if resolve == nil {
					return fmt.Errorf("%w: label %d", ErrUnresolvedLabel, rel.Label)
				}
				addr, err := resolve(rel.Label)
				if err != nil {
					return fmt.Errorf("%w: label %d: %w", ErrUnresolvedLabel, rel.Label, err)
				}
				target = addr
			}

			src := unsafe.Add(bases[i], rel.Offset)
			delta := int64(uintptr(target)) - int64(uintptr(src))

			patched := rel.Inst
			patched.Src2 = Imm(delta)
			code, err := enc.Encode(patched)
			if err != nil {
				return err
			}
			if _, err := buf.writeAt(src, code); err != nil {
				return err
			}
		}
	}
	return nil
}
