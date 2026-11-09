// Package host is the JIT backend of the running architecture: the one place
// that says which compile.Machine the host has, if any. It is separate from
// the backends because they import jit.
//
// Adding an architecture means an asm.Arch in internal/asm/<arch>, which owns
// the encoder and the runtime (Enter, Resume, exit stub, instruction-cache
// flush) behind its own build tags, a compile.Machine in internal/jit/<arch>,
// its host_<arch>.go here and in internal/asm/host, and narrowing the !arm64
// build tag of both host_unsupported.go files. Otherwise host.New would report a backend whose Enter
// panics.
package host
