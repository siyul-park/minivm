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
// Field / Raw allocate a fresh heap entry per read for KindRef fields; this
// is safe inside STRUCT_GET (which retains immediately) but callers outside
// the opcode handler must take ownership themselves.
type HostObject struct {
	Typ      *types.StructType
	Receiver reflect.Value
	data     unsafe.Pointer
	slots    []hostSlot
	interp   *Interpreter
}

// hostSlot maps a VM field index to either a Go struct field (field ≥ 0) or a
// bound method (field < 0; addr is the *HostFunction heap address).
// kind is the reflect.Kind of the Go field; reflect.Interface indicates a
// types.Value-implementing interface.
type hostSlot struct {
	field  int
	method int
	addr   int
	offset uintptr
	kind   reflect.Kind
	fnType *types.FunctionType
}

var (
	_ types.Value     = (*HostFunction)(nil)
	_ types.Value     = (*HostObject)(nil)
	_ types.Traceable = (*HostObject)(nil)
)

func NewHostFunction(typ *types.FunctionType, fn func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error)) *HostFunction {
	return &HostFunction{Typ: typ, Fn: fn}
}

func (f *HostFunction) Kind() types.Kind { return types.KindRef }
func (f *HostFunction) Type() types.Type { return f.Typ }

func (f *HostFunction) String() string {
	return fmt.Sprintf("%s\n<native>", f.Typ)
}

func (h *HostObject) Kind() types.Kind { return types.KindRef }
func (h *HostObject) Type() types.Type { return h.Typ }

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
	s, ok := h.lookup(i)
	if !ok {
		return 0
	}
	if s.isMethod() {
		return types.BoxRef(s.addr)
	}
	return h.field(s)
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
	return uint64(h.field(s))
}

func (h *HostObject) SetRaw(i int, bits uint64) {
	s, ok := h.lookup(i)
	if !ok || s.isMethod() {
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

func (s hostSlot) isMethod() bool { return s.field < 0 }

func (h *HostObject) lookup(i int) (hostSlot, bool) {
	if i < 0 || i >= len(h.slots) {
		return hostSlot{}, false
	}
	return h.slots[i], true
}

func (h *HostObject) field(s hostSlot) types.Boxed {
	ptr := h.fieldPtr(s)
	switch s.kind {
	case reflect.Bool:
		return types.BoxBool(*(*bool)(ptr))
	case reflect.Int8:
		return types.BoxI32(int32(*(*int8)(ptr)))
	case reflect.Int16:
		return types.BoxI32(int32(*(*int16)(ptr)))
	case reflect.Int32:
		return types.BoxI32(*(*int32)(ptr))
	case reflect.Int:
		return h.interp.boxI64(int64(*(*int)(ptr)))
	case reflect.Int64:
		return h.interp.boxI64(*(*int64)(ptr))
	case reflect.Uint8:
		return types.BoxI32(int32(*(*uint8)(ptr)))
	case reflect.Uint16:
		return types.BoxI32(int32(*(*uint16)(ptr)))
	case reflect.Uint32:
		return types.BoxI32(int32(*(*uint32)(ptr)))
	case reflect.Uint:
		return h.interp.boxI64(int64(*(*uint)(ptr)))
	case reflect.Uint64:
		return h.interp.boxI64(int64(*(*uint64)(ptr)))
	case reflect.Uintptr:
		return h.interp.boxI64(int64(*(*uintptr)(ptr)))
	case reflect.Float32:
		return types.BoxF32(*(*float32)(ptr))
	case reflect.Float64:
		return types.BoxF64(*(*float64)(ptr))
	case reflect.String:
		return h.interp.box(types.String(*(*string)(ptr)))
	case reflect.Interface:
		return h.interp.box(*(*types.Value)(ptr))
	default:
		return 0
	}
}

func (h *HostObject) setField(s hostSlot, val types.Boxed) {
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
		if val.Kind() == types.KindRef {
			value, err := h.interp.Load(val.Ref())
			if err != nil {
				panic(err)
			}
			*(*types.Value)(ptr) = value
		} else {
			*(*types.Value)(ptr) = types.Unbox(val)
		}
	}
}

func (h *HostObject) fieldPtr(s hostSlot) unsafe.Pointer {
	return unsafe.Add(h.data, s.offset)
}

// hostFieldVMType maps a Go reflect.Kind to its VM-side types.Type.
// Returns nil for kinds that HostObject cannot represent directly.
// reflect.Interface is only valid for types implementing types.Value;
// callers must filter non-Value interfaces before reaching this point.
func hostFieldVMType(k reflect.Kind) types.Type {
	switch k {
	case reflect.Bool, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return types.TypeI32
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return types.TypeI64
	case reflect.Float32:
		return types.TypeF32
	case reflect.Float64:
		return types.TypeF64
	case reflect.String:
		return types.TypeString
	case reflect.Interface:
		return types.TypeRef
	default:
		return nil
	}
}
