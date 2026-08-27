package jit

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/prof"
)

// Code is one compiled unit: every native entry point one Compiler.Compile
// call produced for a requested anchor, plus their combined size.
type Code struct {
	Entries map[Anchor]Entry
	Bytes   int
}

// Entry is one native entry point in a Code: its callable, the anchor kind
// and frontend it was compiled from, its code size, the exits it may take,
// and the bridge-resumable IPs among its blocks.
type Entry struct {
	Callable  asm.Callable
	Kind      EntryKind
	Frontend  prof.Frontend
	Bytes     int
	Exits     []ExitDescriptor
	Resumable []int
}

// ExitDescriptor names one guard, cold-branch, or terminal-opcode exit an
// Entry may take: the reason it exited and the opcode it exited from.
type ExitDescriptor struct {
	Reason prof.ExitReason
	Opcode int
}
