//go:build darwin || linux

package testenv

import (
	"syscall"
	"testing"
)

// Guarded returns n writable bytes whose last byte ends a page followed by an
// unreadable page, so a load past the slice faults. The memory is unmapped when
// the test ends.
func Guarded(t testing.TB, n int) []byte {
	t.Helper()
	page := syscall.Getpagesize()
	pages := (n+page-1)/page + 1
	mem, err := syscall.Mmap(-1, 0, (pages+1)*page, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Munmap(mem); err != nil {
			t.Error(err)
		}
	})
	if err := syscall.Mprotect(mem[pages*page:], syscall.PROT_NONE); err != nil {
		t.Fatal(err)
	}
	return mem[pages*page-n : pages*page]
}
