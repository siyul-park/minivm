package interp

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

// HostFunction exposes a Go function to the VM. The codec produces one for a
// marshaled Go function, and host code may build one directly with
// NewHostFunction.
type HostFunction struct {
	Typ *types.FunctionType
	Fn  func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)
}

// HostStruct is a live view of a Go struct. The codec produces one for a struct
// a copy cannot reproduce - one carrying unexported state, or one a pointer
// receiver mutates - so a field the guest writes and a method the host calls
// address the same memory.
//
// The pointer is what the GC traces, so the Go struct stays alive for as long as
// the VM holds the view, and the Go type is what identifies it on the way back
// out. Both are fixed at construction: there is no layout to re-validate on the
// way into a field.
type HostStruct struct {
	typ      *types.StructType
	fields   []field
	registry *Registry
	rtyp     reflect.Type
	ptr      unsafe.Pointer
}

var (
	_ types.Value = (*HostFunction)(nil)
	_ types.Value = (*HostStruct)(nil)
)

func NewHostFunction(typ *types.FunctionType, fn func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)) *HostFunction {
	return &HostFunction{Typ: typ, Fn: fn}
}

func (f *HostFunction) Kind() types.Kind { return types.KindRef }
func (f *HostFunction) Type() types.Type { return f.Typ }

func (f *HostFunction) String() string {
	return fmt.Sprintf("%s\n<native>", f.Typ)
}

func (h *HostStruct) Kind() types.Kind { return types.KindRef }
func (h *HostStruct) Type() types.Type { return h.typ }
func (h *HostStruct) String() string   { return fmt.Sprintf("%s\n<native>", h.typ) }

// Field reads the field at at out of the Go struct, in the form a VM slot of
// that field's type holds. A field that needs a heap reference publishes one on
// i, so the result is owned by the caller exactly as a *types.Struct field read
// is. i is the interpreter executing the read, not the one that marshaled the
// value, so a pooled interpreter reads through a shared view safely.
func (h *HostStruct) Field(i *Interpreter, at int) (types.Boxed, error) {
	if at < 0 || at >= len(h.fields) {
		return 0, ErrSegmentationFault
	}
	f := &h.fields[at]
	e := &i.enc
	*e = Encoder{interp: i, registry: h.registry, owned: e.owned[:0]}
	result, err := f.conversion.box(e, unsafe.Add(h.ptr, f.offset))
	if err != nil {
		// A conversion that failed partway has no result to own what it
		// already published, so the read leaves the heap as it found it.
		e.discard()
		return 0, err
	}
	return result, nil
}

// SetField writes val into the Go struct. The Go field takes a copy of what val
// holds rather than the slot itself, so a successful write consumes val: a
// reference it carries is released instead of retained.
func (h *HostStruct) SetField(i *Interpreter, at int, val types.Boxed) error {
	if at < 0 || at >= len(h.fields) {
		return ErrSegmentationFault
	}
	f := &h.fields[at]
	value, err := h.registry.resolve(i, val)
	if err != nil {
		return err
	}
	d := &i.dec
	*d = Decoder{interp: i, registry: h.registry}
	if err := f.conversion.set(d, value, unsafe.Add(h.ptr, f.offset)); err != nil {
		return err
	}
	i.releaseBox(val)
	return nil
}

// hosting reports the Go value val stands for, when val is a host value over
// rtyp. It is how a conversion recovers the value it handed the VM, including
// the unexported state no VM representation carries.
func hosting(val types.Value, rtyp reflect.Type) (unsafe.Pointer, bool) {
	h, ok := val.(*HostStruct)
	if !ok || h.rtyp != rtyp {
		return nil, false
	}
	return h.ptr, true
}
