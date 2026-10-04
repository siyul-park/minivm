package instr

// Handler defines a protected byte-IP range [Start, End) and Catch target.
// Depth restores the operand stack at entry; handlers are ordered innermost first.
type Handler struct {
	Start int
	End   int
	Catch int
	Depth int
}
