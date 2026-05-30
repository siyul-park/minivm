package jit

import (
	"runtime"
	"sync"
)

// Register installs the Lowerer that the compiler will dispatch to. Arch
// packages call this from init(); the consumer blank-imports the desired
// arch package to opt in.
func Register(arch string, l Lowerer) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[arch] = l
}

// Lookup returns the Lowerer registered for arch, or nil if none.
func Lookup(arch string) Lowerer {
	registryMu.Lock()
	defer registryMu.Unlock()
	return registry[arch]
}

// Active returns the Lowerer matching runtime.GOARCH, or nil if none.
func Active() Lowerer {
	return Lookup(runtime.GOARCH)
}

var (
	registryMu sync.Mutex
	registry   = make(map[string]Lowerer)
)
