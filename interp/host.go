package interp

import (
	"fmt"
	"reflect"

	"github.com/siyul-park/minivm/types"
)

type HostFunction struct {
	Typ *types.FunctionType
	Fn  func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)
}

var _ types.Value = (*HostFunction)(nil)

func NewHostFunction(typ *types.FunctionType, fn func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)) *HostFunction {
	return &HostFunction{Typ: typ, Fn: fn}
}

func (f *HostFunction) Kind() types.Kind { return types.KindRef }
func (f *HostFunction) Type() types.Type { return f.Typ }

func (f *HostFunction) String() string {
	return fmt.Sprintf("%s\n<native>", f.Typ)
}

// HostObject exposes a Go value to the VM with both data and methods.
// Receiver is an addressable copy of the marshaled Go value owned by the
// HostObject; reads and writes happen through reflect against this copy, and
// the original caller-side value is unaffected unless explicitly restored via
// Unmarshal. Methods are pre-bound as *HostFunction values allocated on the
// VM heap and retained via Refs.
//
// Field / Raw allocate a fresh heap entry per read for KindRef fields; this
// is safe inside STRUCT_GET (which retains immediately) but callers outside
// the opcode handler must take ownership themselves.
type HostObject struct {
	Typ      *types.StructType
	Receiver reflect.Value // addressable pointer to the original Go value
	slots    []hostSlot
	interp   *Interpreter
}

// hostSlot maps a VM field index to either a Go struct field (field ≥ 0) or a
// bound method (field < 0; addr is the *HostFunction heap address).
type hostSlot struct {
	field int
	addr  int
}

func (s hostSlot) isMethod() bool { return s.field < 0 }

var (
	_ types.Value     = (*HostObject)(nil)
	_ types.Traceable = (*HostObject)(nil)
	_ types.Fielded   = (*HostObject)(nil)
)

func (h *HostObject) Kind() types.Kind              { return types.KindRef }
func (h *HostObject) Type() types.Type              { return h.Typ }
func (h *HostObject) StructType() *types.StructType { return h.Typ }

func (h *HostObject) String() string {
	if !h.Receiver.IsValid() {
		return "host<>"
	}
	return fmt.Sprintf("host<%s>", h.Receiver.Elem().Type())
}

func (h *HostObject) Refs() []types.Ref {
	refs := make([]types.Ref, 0, len(h.slots))
	for _, s := range h.slots {
		if s.isMethod() {
			refs = append(refs, types.Ref(s.addr))
		}
	}
	return refs
}

func (h *HostObject) Field(i int) types.Boxed {
	s, _, ok := h.lookup(i)
	if !ok {
		return 0
	}
	if s.isMethod() {
		return types.BoxRef(s.addr)
	}
	return h.marshal(h.Receiver.Elem().Field(s.field))
}

func (h *HostObject) SetField(i int, val types.Boxed) {
	s, _, ok := h.lookup(i)
	if !ok || s.isMethod() {
		return
	}
	h.unmarshal(val, h.Receiver.Elem().Field(s.field))
}

func (h *HostObject) Raw(i int) uint64 {
	s, f, ok := h.lookup(i)
	if !ok {
		return 0
	}
	if s.isMethod() {
		return uint64(types.BoxRef(s.addr))
	}
	rv := h.Receiver.Elem().Field(s.field)
	if f.Kind == types.KindI64 {
		if rv.CanInt() {
			return uint64(rv.Int())
		}
		return rv.Uint()
	}
	return uint64(h.marshal(rv))
}

func (h *HostObject) SetRaw(i int, bits uint64) {
	s, f, ok := h.lookup(i)
	if !ok || s.isMethod() {
		return
	}
	rv := h.Receiver.Elem().Field(s.field)
	if f.Kind == types.KindI64 {
		if !rv.CanSet() {
			return
		}
		if rv.CanInt() {
			rv.SetInt(int64(bits))
		} else {
			rv.SetUint(bits)
		}
		return
	}
	h.unmarshal(types.Boxed(bits), rv)
}

func (h *HostObject) lookup(i int) (hostSlot, types.StructField, bool) {
	if i < 0 || i >= len(h.slots) {
		return hostSlot{}, types.StructField{}, false
	}
	return h.slots[i], h.Typ.Fields[i], true
}

// marshal delegates to the interpreter's injected Marshaler so HostObject
// stays decoupled from the reflect-based default implementation. Errors
// propagate via panic — there is no return path on the Fielded contract,
// and the opcode handler converts panics to VM errors.
func (h *HostObject) marshal(rv reflect.Value) types.Boxed {
	v, err := h.interp.Marshal(rv.Interface())
	if err != nil {
		panic(err)
	}
	return h.interp.box(v)
}

func (h *HostObject) unmarshal(val types.Boxed, rv reflect.Value) {
	if !rv.CanAddr() {
		return
	}
	if err := h.interp.Unmarshal(val, rv.Addr().Interface()); err != nil {
		panic(err)
	}
}
