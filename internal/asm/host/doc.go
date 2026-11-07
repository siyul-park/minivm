// Package host names the asm.Arch of the running architecture as a type, so
// the native runtime calls it directly rather than through an interface: an
// exit-heavy program crosses Enter and Resume too often to afford dispatch.
// It is separate from the architectures because they import asm.
package host
