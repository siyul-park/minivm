// Package journal defines the frame-journal layout: the single contract
// between the threaded interpreter and native JIT code. Callable.Call passes
// a journal's address to native code, which mirrors header cells into pinned
// scratch registers (X10-X14 on ARM64) on external entry and writes result
// and trap state back before returning; the Go wrapper rebuilds interpreter
// state from it. Header cells precede a stack of fixed-stride frame records,
// each mirroring the int fields the threaded interpreter needs to resume a
// frame.
package journal

// Cell indexes one header cell in the journal.
type Cell int

// Record indexes one field within a frame record, relative to the record's
// own start.
type Record int

// Trap is the exit kind native code reports back to the interpreter through
// CellTrap.
type Trap uint64

const (
	CellStack   Cell = iota // &i.stack[0]; external entry in
	CellGlobals             // &i.globals[0]; external entry in
	CellBP                  // current frame bp; external entry in
	CellSP                  // interpreter sp; external entry in/out
	CellEntry               // bridge resume IP in; zero starts at the anchor
	CellDepth               // trap-time frame records written; native read/write
	CellCap                 // frame budget capped by nativeFrameLimit; read-only
	CellTrap                // exit kind out; see Trap
	CellNextIP              // resume/fallback IP out for the single-frame path
	CellBudget              // back-edges remaining before the next safepoint; native read/write
	CellActive              // active native call depth for frame-budget checks
	CellRC                  // &i.rc[0]; read/write for guarded native refcount fast paths
	CellUpvals              // &i.fr.upvals[0] or 0; read/write for closure body fast paths
	CellHeap                // &i.heap[0]; read-only for heap object fast paths
	CellNatives             // &i.natives[0]; atomic per-function entry slots
	CellExitID              // fallback descriptor ID + 1; zero means none
	CellHead                // first frame record cell
)

// Stride is the number of cells in one frame record.
const Stride = 4

// Shift is the left shift that scales a frame-record index into a byte
// offset: one record is 1 << Shift bytes, which must equal Stride * 8.
const Shift = 5

const (
	RecordAddr Record = iota
	RecordBP
	RecordIP
	RecordReturns
)

const (
	TrapNone Trap = iota
	TrapFallback
	TrapOverflow
	TrapYield
	// TrapBridge reports the IP of one opcode the backend cannot lower.
	// CellNextIP carries that opcode's own IP; the Go wrapper runs its
	// threaded closure exactly once and re-enters the same callable at the
	// closure's new IP (see interp.Interpreter.bridge and internal/jit/arm64's
	// dispatch).
	TrapBridge
)

// Len returns the number of cells a journal must have to hold n frame
// records.
func Len(n int) int {
	return int(CellHead) + n*Stride
}

// At returns the cell holding field f of frame record n.
func At(n int, f Record) Cell {
	return CellHead + Cell(n)*Stride + Cell(f)
}
