// Package amd64 ships a stub asm.Arch backend so the JIT pipeline can be
// type-checked and exercised through unit tests on x86_64. It deliberately
// does NOT call jit.Register: leaving runtime.GOARCH unregistered makes
// jit.Active() return nil, which lets the interpreter (and tests gated by
// requireJIT) treat x86_64 as "no JIT available" and fall back cleanly to
// the threaded path.
package amd64
