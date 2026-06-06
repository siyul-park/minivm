//go:build arm64

package arm64

// invoke calls the compiled native block at addr.
// argv layout: five scratch slots for X10..X14; see abi_arm64.s for details.
func invoke(addr uintptr, argv uintptr)
