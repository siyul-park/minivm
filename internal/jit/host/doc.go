// Package host is the JIT backend of the running architecture: the one place
// that says which compile.Machine the host has, if any. It is separate from
// the backends because they import jit.
//
// Adding an architecture means its host_<arch>.go backend, narrowing the
// !arm64 build tag of host_unsupported.go, and internal/asm support:
// enter_<arch>.go/.s (Enter, Resume, exit stub), state_<arch>.go (the
// registers save area the trampoline reaches through go_asm.h), and
// icache_<arch>, with the _noasm build tags narrowed to match. Otherwise host.New would report a
// backend while asm.Enter panics.
package host
