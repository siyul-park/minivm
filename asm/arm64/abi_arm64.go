//go:build arm64

package arm64

// invoke calls the compiled native block at addr.
// argv points to [header, param0, param1, …]; nReserved is decoded from the header.
// rsv points to the pre-allocated []uint64 backing array for scratch-register
// outputs; nil means no reserved outputs are needed.
func invoke(addr uintptr, argv uintptr, rsv uintptr)
