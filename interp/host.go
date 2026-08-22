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
	copy     MarshalerFunc
	registry *Registry
	bounds   span
	stride   uintptr
	rtyp     reflect.Type
	ptr      unsafe.Pointer
}

// HostMap is a live view of a Go map. Go map memory is not addressable, so
// entries convert on the way through rather than being read in place.
type HostMap struct {
	typ      *types.MapType
	index    UnmarshalerFunc
	elem     *conversion
	copy     MarshalerFunc
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
	return convert(i, h.registry, f.conversion.box, unsafe.Add(h.ptr, f.offset))
}

// SetField writes val into the Go struct. The Go field takes a copy of what val
// holds rather than the slot itself, so a successful write consumes val: a
// reference it carries is released instead of retained.
func (h *HostStruct) SetField(i *Interpreter, at int, val types.Boxed) error {
	if at < 0 || at >= len(h.fields) {
		return ErrSegmentationFault
	}
	f := &h.fields[at]
	if err := assign(i, h.registry, f.conversion.set, val, unsafe.Add(h.ptr, f.offset)); err != nil {
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
	return convert(i, h.registry, h.elem.box, unsafe.Add(base, uintptr(at)*h.stride))
}

// SetElement writes val into the element at at. The Go element takes a copy of
// what val holds, so a successful write consumes val.
func (h *HostArray) SetElement(i *Interpreter, at int, val types.Boxed) error {
	base, n := h.bounds(h.ptr)
	if at < 0 || at >= n {
		return ErrIndexOutOfRange
	}
	if err := assign(i, h.registry, h.elem.set, val, unsafe.Add(base, uintptr(at)*h.stride)); err != nil {
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
	elem := h.rtyp.Elem()
	entry, err := decode(i, h.registry, h.elem.set, elem, val)
	if err != nil {
		return err
	}
	for k := range n {
		reflect.NewAt(elem, unsafe.Add(base, uintptr(at+k)*h.stride)).Elem().Set(entry)
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
	elem := h.rtyp.Elem()
	out := dst
	for _, val := range vals {
		entry, err := decode(i, h.registry, h.elem.set, elem, val)
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
	return convert(i, h.registry, h.copy, h.ptr)
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
	holder := reflect.New(h.rtyp.Elem())
	holder.Elem().Set(entry)
	result, err := convert(i, h.registry, h.elem.box, holder.UnsafePointer())
	if err != nil {
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
	entry, err := decode(i, h.registry, h.elem.set, h.rtyp.Elem(), val)
	if err != nil {
		return err
	}
	m := h.value()
	if m.IsNil() {
		return ErrTypeMismatch
	}
	m.SetMapIndex(k, entry)
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

func (h *HostMap) Clear() { h.value().Clear() }

// Map rebuilds the view as the VM map a copy of the Go value would have
// produced, for an opcode that yields a new value rather than changing this one.
func (h *HostMap) Map(i *Interpreter) (types.Value, error) {
	return convert(i, h.registry, h.copy, h.ptr)
}

// value addresses the Go map through the variable the view holds, so a map the
// Go side replaced is the one every access reaches.
func (h *HostMap) value() reflect.Value { return reflect.NewAt(h.rtyp, h.ptr).Elem() }

// key decodes a VM key into the Go key type, which is what makes a key the
// guest computed find the entry the Go side stored.
func (h *HostMap) key(i *Interpreter, key types.Boxed) (reflect.Value, error) {
	return decode(i, h.registry, h.index, h.rtyp.Key(), key)
}

// convert reads the Go value at p through one compiled conversion. A conversion
// that failed partway has no result to own what it already published, so the
// read leaves the heap as it found it.
func convert[T any](i *Interpreter, r *Registry, run func(*Encoder, unsafe.Pointer) (T, error), p unsafe.Pointer) (T, error) {
	e := i.encoder(r)
	out, err := run(e, p)
	if err != nil {
		e.discard()
		var zero T
		return zero, err
	}
	return out, nil
}

// assign writes a VM value into the live Go value at p.
func assign(i *Interpreter, r *Registry, set UnmarshalerFunc, val types.Boxed, p unsafe.Pointer) error {
	value, err := r.resolve(i, val)
	if err != nil {
		return err
	}
	return set(i.decoder(r), value, p)
}

// decode writes a VM value into a fresh Go value of type t, for a destination
// the view has no address for: a Go map entry, or an element append has yet to
// make room for.
func decode(i *Interpreter, r *Registry, set UnmarshalerFunc, t reflect.Type, val types.Boxed) (reflect.Value, error) {
	out := reflect.New(t)
	if err := assign(i, r, set, val, out.UnsafePointer()); err != nil {
		return reflect.Value{}, err
	}
	return out.Elem(), nil
}

// hosting reports the Go value val stands for, when val is a host value over
// rtyp. It is how a conversion recovers the value it handed the VM, including
// the unexported state no VM representation carries.
func hosting(val types.Value, rtyp reflect.Type) (unsafe.Pointer, bool) {
	switch h := val.(type) {
	case *HostStruct:
		if h.rtyp == rtyp {
			return h.ptr, true
		}
	case *HostArray:
		if h.rtyp == rtyp {
			return h.ptr, true
		}
	case *HostMap:
		if h.rtyp == rtyp {
			return h.ptr, true
		}
	}
	return nil, false
}
