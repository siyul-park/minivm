package interp

import (
	"fmt"
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

type HostFunction struct {
	Typ *types.FunctionType
	Fn  func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)
}

// HostObject exposes a Go value to the VM with both data and methods.
// Receiver is an addressable copy of the marshaled Go value owned by the
// HostObject; reads and writes use compiled offsets against this copy, and the
// original caller-side value is unaffected unless explicitly restored via
// Unmarshal. Methods are pre-bound as *HostFunction values allocated on the VM
// heap and retained via Refs.
//
// Data reads through Field or Raw return one owned KindRef when boxing creates
// a heap value. Bound-method and direct stored-ref reads are borrowed; callers
// outside the opcode handler must retain those refs before keeping them.
// SetField and SetRaw consume an owned KindRef when a data write copies its
// value into the Go receiver; invalid and bound-method writes consume nothing.
type HostObject struct {
	Typ      *types.StructType
	Receiver reflect.Value
	codec    *codec
	interp   *Interpreter

	layout   *types.StructType
	receiver reflect.Type
	data     unsafe.Pointer
	slots    []hostSlot
}

// hostSlot maps a VM field to storage metadata or a bound method. Negative
// index values encode method indexes as -index-1; addr is the heap address.
// kind is the reflect.Kind of the Go field; reflect.Interface indicates a
// types.Value-implementing interface. A non-nil conversion handles a custom
// field conversion; nil keeps the primitive unsafe fast path.
type hostSlot struct {
	index  int
	addr   int
	offset uintptr
	kind   reflect.Kind
	fnType *types.FunctionType

	conversion *conversion
}

var (
	_ types.Value     = (*HostFunction)(nil)
	_ types.Value     = (*HostObject)(nil)
	_ types.Traceable = (*HostObject)(nil)
)

func NewHostFunction(typ *types.FunctionType, fn func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)) *HostFunction {
	return &HostFunction{Typ: typ, Fn: fn}
}

// NewHostObject exposes receiver through the built-in reflection codec. It
// owns an addressable copy, so field writes and pointer-receiver methods do
// not mutate the caller's value.
func NewHostObject(i *Interpreter, receiver any) (*HostObject, error) {
	if i == nil || i.planner == nil || receiver == nil {
		return nil, fmt.Errorf("%w: interpreter and receiver must be non-nil", ErrTypeMismatch)
	}
	v := reflect.ValueOf(receiver)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, fmt.Errorf("%w: receiver must be non-nil", ErrTypeMismatch)
		}
		v = v.Elem()
	}
	vm, slots, _, err := i.planner.compileHostObject(v.Type(), make(map[reflect.Type]bool))
	if err != nil {
		return nil, err
	}
	ptr := reflect.New(v.Type())
	ptr.Elem().Set(v)
	state := &marshalState{codec: i.planner, interp: i}
	return state.hostObject(ptr, slots, vm)
}

func (f *HostFunction) Kind() types.Kind { return types.KindRef }
func (f *HostFunction) Type() types.Type { return f.Typ }

func (f *HostFunction) String() string {
	return fmt.Sprintf("%s\n<native>", f.Typ)
}

func (h *HostObject) Kind() types.Kind { return types.KindRef }
func (h *HostObject) Type() types.Type {
	if h == nil {
		return nil
	}
	return h.Typ
}

func (h *HostObject) String() string {
	if !h.valid() {
		return "host<>"
	}
	return fmt.Sprintf("host<%s>", h.Receiver.Elem().Type())
}

func (h *HostObject) Refs(dst []types.Ref) []types.Ref {
	if h == nil {
		return dst
	}
	for _, s := range h.slots {
		if s.isMethod() {
			dst = append(dst, types.Ref(s.addr))
		} else if ref, ok := h.storedRef(s); ok {
			dst = append(dst, ref)
		}
	}
	return dst
}

func (h *HostObject) Field(i int) types.Boxed {
	value, _, ok := h.read(i)
	if !ok {
		return 0
	}
	return value
}

func (h *HostObject) SetField(i int, val types.Boxed) {
	s, ok := h.lookup(i)
	if !ok || s.isMethod() {
		return
	}
	h.setField(s, val)
}

func (h *HostObject) Raw(i int) uint64 {
	s, ok := h.lookup(i)
	if !ok {
		return 0
	}
	if s.isMethod() {
		return uint64(types.BoxRef(s.addr))
	}
	if s.conversion != nil {
		value, _ := h.field(s)
		if h.layout.Fields[i].Kind == types.KindI64 {
			return uint64(h.interp.unboxI64(value))
		}
		return uint64(value)
	}
	ptr := h.fieldPtr(s)
	switch s.kind {
	case reflect.Int:
		return uint64(*(*int)(ptr))
	case reflect.Int64:
		return uint64(*(*int64)(ptr))
	case reflect.Uint:
		return uint64(*(*uint)(ptr))
	case reflect.Uint64:
		return *(*uint64)(ptr)
	case reflect.Uintptr:
		return uint64(*(*uintptr)(ptr))
	}
	value, _ := h.field(s)
	return uint64(value)
}

func (h *HostObject) SetRaw(i int, bits uint64) {
	s, ok := h.lookup(i)
	if !ok || s.isMethod() {
		return
	}
	if s.conversion != nil {
		if h.layout.Fields[i].Kind == types.KindI64 {
			h.setField(s, h.interp.boxI64(int64(bits)))
		} else {
			h.setField(s, types.Boxed(bits))
		}
		return
	}
	ptr := h.fieldPtr(s)
	switch s.kind {
	case reflect.Int:
		*(*int)(ptr) = int(bits)
	case reflect.Int64:
		*(*int64)(ptr) = int64(bits)
	case reflect.Uint:
		*(*uint)(ptr) = uint(bits)
	case reflect.Uint64:
		*(*uint64)(ptr) = bits
	case reflect.Uintptr:
		*(*uintptr)(ptr) = uintptr(bits)
	default:
		h.setField(s, types.Boxed(bits))
	}
}

func (h *HostObject) kind(i int) (types.Kind, bool) {
	if _, ok := h.lookup(i); !ok {
		return 0, false
	}
	return h.layout.Fields[i].Kind, true
}

func (h *HostObject) writeKind(i int) (types.Kind, bool) {
	s, ok := h.lookup(i)
	if !ok || s.isMethod() {
		return 0, false
	}
	return h.layout.Fields[i].Kind, true
}

func (h *HostObject) read(i int) (types.Boxed, bool, bool) {
	s, ok := h.lookup(i)
	if !ok {
		return 0, false, false
	}
	if s.isMethod() {
		return types.BoxRef(s.addr), false, true
	}
	value, owned := h.field(s)
	return value, owned, true
}

func (h *HostObject) lookup(i int) (hostSlot, bool) {
	if !h.valid() || i < 0 || i >= len(h.slots) || i >= len(h.layout.Fields) {
		return hostSlot{}, false
	}
	return h.slots[i], true
}

func (h *HostObject) valid() bool {
	return h != nil &&
		h.Typ != nil && h.Typ == h.layout &&
		h.Receiver.IsValid() && h.Receiver.Kind() == reflect.Pointer && !h.Receiver.IsNil() &&
		h.Receiver.Type() == h.receiver &&
		h.codec != nil && h.interp != nil && unsafe.Pointer(h.Receiver.Pointer()) == h.data
}

func (h *HostObject) field(s hostSlot) (types.Boxed, bool) {
	if s.conversion != nil {
		return h.convert(s)
	}
	ptr := h.fieldPtr(s)
	switch s.kind {
	case reflect.Bool:
		return types.BoxI1(*(*bool)(ptr)), false
	case reflect.Int8:
		return types.BoxI32(int32(*(*int8)(ptr))), false
	case reflect.Int16:
		return types.BoxI32(int32(*(*int16)(ptr))), false
	case reflect.Int32:
		return types.BoxI32(*(*int32)(ptr)), false
	case reflect.Int:
		return h.box(types.I64(*(*int)(ptr)))
	case reflect.Int64:
		return h.box(types.I64(*(*int64)(ptr)))
	case reflect.Uint8:
		return types.BoxI32(int32(*(*uint8)(ptr))), false
	case reflect.Uint16:
		return types.BoxI32(int32(*(*uint16)(ptr))), false
	case reflect.Uint32:
		return types.BoxI32(int32(*(*uint32)(ptr))), false
	case reflect.Uint:
		return h.box(types.I64(*(*uint)(ptr)))
	case reflect.Uint64:
		return h.box(types.I64(*(*uint64)(ptr)))
	case reflect.Uintptr:
		return h.box(types.I64(*(*uintptr)(ptr)))
	case reflect.Float32:
		return types.BoxF32(*(*float32)(ptr)), false
	case reflect.Float64:
		return types.BoxF64(*(*float64)(ptr)), false
	case reflect.String:
		return h.box(types.String(*(*string)(ptr)))
	case reflect.Interface:
		return h.box(*(*types.Value)(ptr))
	default:
		return 0, false
	}
}

func (h *HostObject) setField(s hostSlot, val types.Boxed) {
	if s.conversion != nil {
		h.setConverted(s, val)
		return
	}
	ptr := h.fieldPtr(s)
	switch s.kind {
	case reflect.Bool:
		*(*bool)(ptr) = val.I32() != 0
	case reflect.Int8:
		*(*int8)(ptr) = int8(val.I32())
	case reflect.Int16:
		*(*int16)(ptr) = int16(val.I32())
	case reflect.Int32:
		*(*int32)(ptr) = val.I32()
	case reflect.Int:
		*(*int)(ptr) = int(val.I64())
	case reflect.Int64:
		*(*int64)(ptr) = val.I64()
	case reflect.Uint8:
		*(*uint8)(ptr) = uint8(val.I32())
	case reflect.Uint16:
		*(*uint16)(ptr) = uint16(val.I32())
	case reflect.Uint32:
		*(*uint32)(ptr) = uint32(val.I32())
	case reflect.Uint:
		*(*uint)(ptr) = uint(val.I64())
	case reflect.Uint64:
		*(*uint64)(ptr) = uint64(val.I64())
	case reflect.Uintptr:
		*(*uintptr)(ptr) = uintptr(val.I64())
	case reflect.Float32:
		*(*float32)(ptr) = val.F32()
	case reflect.Float64:
		*(*float64)(ptr) = val.F64()
	case reflect.String:
		if val.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		defer h.interp.releaseBox(val)
		value, err := h.interp.Load(val.Ref())
		if err != nil {
			panic(err)
		}
		str, ok := value.(types.String)
		if !ok {
			panic(ErrTypeMismatch)
		}
		*(*string)(ptr) = string(str)
	case reflect.Interface:
		var value types.Value
		if val.Kind() == types.KindRef {
			if _, err := h.interp.Load(val.Ref()); err != nil {
				panic(err)
			}
			value = types.Ref(val.Ref())
		} else {
			value = types.Unbox(val)
		}
		old, stored := h.storedRef(s)
		*(*types.Value)(ptr) = value
		if stored {
			h.interp.release(int(old))
		}
	}
}

func (h *HostObject) storedRef(s hostSlot) (types.Ref, bool) {
	if s.conversion != nil || s.kind != reflect.Interface {
		return 0, false
	}
	value := *(*types.Value)(h.fieldPtr(s))
	switch v := value.(type) {
	case types.Boxed:
		if v.Kind() == types.KindRef {
			return types.Ref(v.Ref()), true
		}
	case types.Ref:
		return v, true
	}
	return 0, false
}

func (h *HostObject) convert(s hostSlot) (types.Boxed, bool) {
	state := &marshalState{codec: h.codec, interp: h.interp}
	v := reflect.NewAt(s.conversion.typ, h.fieldPtr(s)).Elem()
	value, err := s.conversion.marshal(state, v)
	if err != nil {
		panic(err)
	}
	boxed, owned := h.box(value)
	if boxed.Kind() == types.KindRef && !owned {
		h.interp.retainBox(boxed)
		owned = true
	}
	return boxed, owned
}

func (h *HostObject) setConverted(s hostSlot, val types.Boxed) {
	defer h.interp.releaseBox(val)
	value, err := h.codec.resolve(h.interp, val)
	if err != nil {
		panic(err)
	}
	dst := reflect.NewAt(s.conversion.typ, h.fieldPtr(s)).Elem()
	if err := s.conversion.unmarshal(&unmarshalState{codec: h.codec, interp: h.interp}, value, dst); err != nil {
		panic(err)
	}
}

func (h *HostObject) fieldPtr(s hostSlot) unsafe.Pointer {
	return unsafe.Add(h.data, s.offset)
}

func (h *HostObject) box(value types.Value) (types.Boxed, bool) {
	boxed := h.interp.box(value)
	if boxed.Kind() != types.KindRef {
		return boxed, false
	}
	switch value.(type) {
	case types.Boxed, types.Ref:
		return boxed, false
	default:
		return boxed, true
	}
}

func (s hostSlot) isMethod() bool { return s.index < 0 }

func (s hostSlot) method() int { return -s.index - 1 }
