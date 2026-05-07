//go:build !arm64

package arm64

func invoke(addr uintptr, argv uintptr, rsv uintptr) {
	panic("not implemented")
}
