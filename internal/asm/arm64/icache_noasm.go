//go:build !arm64

package arm64

// Flush has nothing to do on this host: no native code runs here.
func (Arch) Flush([]byte) {}
