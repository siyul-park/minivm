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

// HostArray is a live view of a Go slice, or of a Go array reached through a
// pointer. It addresses the Go variable rather than its elements, so it reports
// the length the Go side has now.
type HostArray struct {
	typ      *types.ArrayType
	elem     *conversion
	copy     func(*Encoder, unsafe.Pointer) (types.Value, error)
	registry *Registry
	bounds   func(unsafe.Pointer) (unsafe.Pointer, int)
	stride   uintptr
	rtyp     reflect.Type
	ptr      unsafe.Pointer
}

// HostMap is a live view of a Go map. Go map memory is not addressable, so
// entries convert on the way through rather than being read in place.
type HostMap struct {
	typ      *types.MapType
	index    func(*Decoder, types.Value, unsafe.Pointer) error
	elem     *conversion
	copy     func(*Encoder, unsafe.Pointer) (types.Value, error)
	registry *Registry
	rtyp     reflect.Type
	ptr      unsafe.Pointer
}

var (
	_ types.Value = (*HostFunction)(nil)
	_ types.Value = (*HostStruct)(nil)
	_ types.Value = (*HostArray)(nil)
	_ types.Value = (*HostMap)(nil)
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

func (h *HostArray) Kind() types.Kind { return types.KindRef }
func (h *HostArray) Type() types.Type { return h.typ }
func (h *HostArray) String() string   { return fmt.Sprintf("%s\n<native>", h.typ) }

// Len reports the length the Go slice or array has now.
func (h *HostArray) Len() int {
	_, n := h.bounds(h.ptr)
	return n
}

// Element reads the element at at, in the form a VM slot of the element type
// holds. An element that needs a heap reference publishes one the caller owns.
func (h *HostArray) Element(i *Interpreter, at int) (types.Boxed, error) {
	base, n := h.bounds(h.ptr)
	if at < 0 || at >= n {
		return 0, ErrIndexOutOfRange
	}
	e := &i.enc
	*e = Encoder{interp: i, registry: h.registry, owned: e.owned[:0]}
	result, err := h.elem.box(e, unsafe.Add(base, uintptr(at)*h.stride))
	if err != nil {
		e.discard()
		return 0, err
	}
	return result, nil
}

// SetElement writes val into the element at at. The Go element takes a copy of
// what val holds, so a successful write consumes val.
func (h *HostArray) SetElement(i *Interpreter, at int, val types.Boxed) error {
	base, n := h.bounds(h.ptr)
	if at < 0 || at >= n {
		return ErrIndexOutOfRange
	}
	value, err := h.registry.resolve(i, val)
	if err != nil {
		return err
	}
	d := &i.dec
	*d = Decoder{interp: i, registry: h.registry}
	if err := h.elem.set(d, value, unsafe.Add(base, uintptr(at)*h.stride)); err != nil {
		return err
	}
	i.releaseBox(val)
	return nil
}

// Fill writes val into n elements starting at at. The value converts once and
// every slot takes a copy of it, so the write consumes val exactly once.
func (h *HostArray) Fill(i *Interpreter, at, n int, val types.Boxed) error {
	base, size := h.bounds(h.ptr)
	if at < 0 || n < 0 || at+n > size {
		return ErrIndexOutOfRange
	}
	entry, err := h.decode(i, val)
	if err != nil {
		return err
	}
	for k := range n {
		reflect.NewAt(h.rtyp.Elem(), unsafe.Add(base, uintptr(at+k)*h.stride)).Elem().Set(entry)
	}
	i.releaseBox(val)
	return nil
}

// Append adds vals to the Go slice and writes the result back through the view,
// so the Go side sees the growth even when append reallocates. A Go array has a
// fixed length and cannot grow.
func (h *HostArray) Append(i *Interpreter, vals []types.Boxed) error {
	dst, ok := h.slice()
	if !ok {
		return fmt.Errorf("%w: cannot append to %s", ErrUnsupportedMarshalType, h.rtyp)
	}
	out := dst
	for _, val := range vals {
		entry, err := h.decode(i, val)
		if err != nil {
			return err
		}
		out = reflect.Append(out, entry)
	}
	dst.Set(out)
	for _, val := range vals {
		i.releaseBox(val)
	}
	return nil
}

// Delete removes the element at at from the Go slice, reports it, and writes
// the shortened slice back through the view.
func (h *HostArray) Delete(i *Interpreter, at int) (types.Boxed, error) {
	dst, ok := h.slice()
	if !ok {
		return 0, fmt.Errorf("%w: cannot delete from %s", ErrUnsupportedMarshalType, h.rtyp)
	}
	n := dst.Len()
	if at < 0 || at >= n {
		return 0, ErrIndexOutOfRange
	}
	removed, err := h.Element(i, at)
	if err != nil {
		return 0, err
	}
	reflect.Copy(dst.Slice(at, n-1), dst.Slice(at+1, n))
	dst.Set(dst.Slice(0, n-1))
	return removed, nil
}

// Array rebuilds the view as the VM array a copy of the Go value would have
// produced. An opcode that yields a new array rather than reshaping this one
// works from that copy, so the result is VM-owned and the view keeps
// addressing Go memory.
func (h *HostArray) Array(i *Interpreter) (types.Value, error) {
	return materialize(i, h.registry, h.copy, h.ptr)
}

// slice addresses the Go slice through the variable the view holds, so a slice
// that append reallocated is the one the next access reaches. A Go array has no
// header to rewrite and reports false.
func (h *HostArray) slice() (reflect.Value, bool) {
	if h.rtyp.Kind() != reflect.Slice {
		return reflect.Value{}, false
	}
	return reflect.NewAt(h.rtyp, h.ptr).Elem(), true
}

// decode converts a VM value into a fresh Go element.
func (h *HostArray) decode(i *Interpreter, val types.Boxed) (reflect.Value, error) {
	value, err := h.registry.resolve(i, val)
	if err != nil {
		return reflect.Value{}, err
	}
	out := reflect.New(h.rtyp.Elem())
	d := &i.dec
	*d = Decoder{interp: i, registry: h.registry}
	if err := h.elem.set(d, value, out.UnsafePointer()); err != nil {
		return reflect.Value{}, err
	}
	return out.Elem(), nil
}

func (h *HostMap) Kind() types.Kind { return types.KindRef }
func (h *HostMap) Type() types.Type { return h.typ }
func (h *HostMap) String() string   { return fmt.Sprintf("%s\n<native>", h.typ) }

// Len reports the number of entries the Go map holds now.
func (h *HostMap) Len() int { return h.value().Len() }

// Get reads the entry keyed by key, reporting whether the Go map holds one. A
// missing key reads as the element zero value, the way every VM map reads one.
func (h *HostMap) Get(i *Interpreter, key types.Boxed) (types.Boxed, bool, error) {
	k, err := h.key(i, key)
	if err != nil {
		return 0, false, err
	}
	entry := h.value().MapIndex(k)
	if !entry.IsValid() {
		i.releaseBox(key)
		return types.Zero(h.typ.ElemKind), false, nil
	}
	// Go map memory is not addressable, so the entry converts out of a holder
	// of its own rather than in place.
	holder := reflect.New(h.rtyp.Elem())
	holder.Elem().Set(entry)
	e := &i.enc
	*e = Encoder{interp: i, registry: h.registry, owned: e.owned[:0]}
	result, err := h.elem.box(e, holder.UnsafePointer())
	if err != nil {
		e.discard()
		return 0, false, err
	}
	i.releaseBox(key)
	return result, true, nil
}

// Set writes an entry into the Go map, consuming both boxes: the Go map takes
// a copy of what each holds rather than the slots themselves.
func (h *HostMap) Set(i *Interpreter, key, val types.Boxed) error {
	k, err := h.key(i, key)
	if err != nil {
		return err
	}
	value, err := h.registry.resolve(i, val)
	if err != nil {
		return err
	}
	entry := reflect.New(h.rtyp.Elem())
	d := &i.dec
	*d = Decoder{interp: i, registry: h.registry}
	if err := h.elem.set(d, value, entry.UnsafePointer()); err != nil {
		return err
	}
	m := h.value()
	if m.IsNil() {
		return ErrTypeMismatch
	}
	m.SetMapIndex(k, entry.Elem())
	i.releaseBox(key)
	i.releaseBox(val)
	return nil
}

// Delete removes the entry keyed by key, consuming the key box.
func (h *HostMap) Delete(i *Interpreter, key types.Boxed) error {
	k, err := h.key(i, key)
	if err != nil {
		return err
	}
	h.value().SetMapIndex(k, reflect.Value{})
	i.releaseBox(key)
	return nil
}

// Clear removes every entry from the Go map.
func (h *HostMap) Clear() { h.value().Clear() }

// Map rebuilds the view as the VM map a copy of the Go value would have
// produced, for an opcode that yields a new value rather than changing this one.
func (h *HostMap) Map(i *Interpreter) (types.Value, error) {
	return materialize(i, h.registry, h.copy, h.ptr)
}

// value addresses the Go map through the variable the view holds, so a map the
// Go side replaced is the one every access reaches.
func (h *HostMap) value() reflect.Value { return reflect.NewAt(h.rtyp, h.ptr).Elem() }

// key decodes a VM key into the Go key type, which is what makes a key the
// guest computed find the entry the Go side stored.
func (h *HostMap) key(i *Interpreter, key types.Boxed) (reflect.Value, error) {
	value, err := h.registry.resolve(i, key)
	if err != nil {
		return reflect.Value{}, err
	}
	out := reflect.New(h.rtyp.Key())
	d := &i.dec
	*d = Decoder{interp: i, registry: h.registry}
	if err := h.index(d, value, out.UnsafePointer()); err != nil {
		return reflect.Value{}, err
	}
	return out.Elem(), nil
}

// materialize runs a view's copying conversion, the one its type would have
// used had it never been a view.
func materialize(i *Interpreter, r *Registry, copy func(*Encoder, unsafe.Pointer) (types.Value, error), p unsafe.Pointer) (types.Value, error) {
	e := &i.enc
	*e = Encoder{interp: i, registry: r, owned: e.owned[:0]}
	out, err := copy(e, p)
	if err != nil {
		e.discard()
		return nil, err
	}
	return out, nil
}

// hosting reports the Go value val stands for, when val is a host value over
// rtyp. It is how a conversion recovers the value it handed the VM, including
// the unexported state no VM representation carries.
func hosting(val types.Value, rtyp reflect.Type) (unsafe.Pointer, bool) {
	var (
		typ reflect.Type
		ptr unsafe.Pointer
	)
	switch h := val.(type) {
	case *HostStruct:
		typ, ptr = h.rtyp, h.ptr
	case *HostArray:
		typ, ptr = h.rtyp, h.ptr
	case *HostMap:
		typ, ptr = h.rtyp, h.ptr
	default:
		return nil, false
	}
	if typ != rtyp {
		return nil, false
	}
	return ptr, true
}
