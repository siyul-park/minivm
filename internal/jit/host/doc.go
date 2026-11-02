// Package host is the JIT backend of the running architecture: the one place
// that says which compile.Machine the host has, if any. It is separate from
// the backends because they import jit.
//
// Adding an architecture means one host_<arch>.go and narrowing the !arm64
// build tag of host_other.go. Nothing else changes.
package host
