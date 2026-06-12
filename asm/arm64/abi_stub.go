//go:build !arm64

package arm64

func invoke(addr uintptr, ctx uintptr) {
	panic("not implemented")
}
