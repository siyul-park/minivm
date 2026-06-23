package instr

// Handler is one entry of a function's exception table: a protected byte-IP
// range [Start, End) whose throws and traps transfer control to Catch. Depth is
// the stack height relative to the frame base (sp-bp, i.e. params + locals +
// live operands) at the protected region's entry; on entry to Catch the stack is
// truncated to bp+Depth and the exception value is pushed. Entries are ordered
// innermost-first, so the first range covering an IP is the handler that
// applies.
type Handler struct {
	Start int
	End   int
	Catch int
	Depth int
}
