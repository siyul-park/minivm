package interp

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

// Marshaler converts values of one Go type into VM values. Register one with
// WithMarshaler to override how the built-in codec handles that type.
type Marshaler interface {
	// Marshal converts the value at p. p addresses a live Go value of the
	// registered type and is valid only for the duration of the call.
	Marshal(*Encoder, unsafe.Pointer) (types.Value, error)
}

// MarshalerFunc adapts a function to Marshaler.
type MarshalerFunc func(*Encoder, unsafe.Pointer) (types.Value, error)

// VMMarshaler lets a Go type convert itself. The registry compiles an
// implementing type into a Marshaler and resolves it ahead of the structural
// mapping.
type VMMarshaler interface {
	MarshalVM(*Encoder) (types.Value, error)
}

// sliceHeader mirrors the runtime layout of a Go slice, so a compiled conversion
// reaches the elements without materializing a reflect.Value per access.
type sliceHeader struct {
	data unsafe.Pointer
	len  int
	cap  int
}

// Encoder is one marshal conversion in flight. A Marshaler uses it to reach the
// interpreter and to convert the dependencies of the value it owns, so nested
// conversion always runs through the codec that started the call.
type Encoder struct {
	interp   *Interpreter
	registry *Registry
	seen     map[unsafe.Pointer]bool
	owned    []int
}

func (f MarshalerFunc) Marshal(e *Encoder, p unsafe.Pointer) (types.Value, error) {
	return f(e, p)
}

// Interp returns the interpreter this conversion runs against.
func (e *Encoder) Interp() *Interpreter { return e.interp }

// Marshal converts the Go value of type t at p, resolving t through the same
// codec that started the conversion.
func (e *Encoder) Encode(t reflect.Type, p unsafe.Pointer) (types.Value, error) {
	c, err := e.registry.conversion(t)
	if err != nil {
		return nil, err
	}
	return c.value(e, p)
}

// enter records ptr as being converted and reports whether it already was, so a
// pointer that reaches itself fails instead of recursing forever.
func (e *Encoder) enter(ptr unsafe.Pointer) bool {
	if e.seen[ptr] {
		return false
	}
	if e.seen == nil {
		e.seen = make(map[unsafe.Pointer]bool)
	}
	e.seen[ptr] = true
	return true
}

func (e *Encoder) leave(ptr unsafe.Pointer) { delete(e.seen, ptr) }

// box narrows a standalone value into a slot of typ, publishing a heap ref when
// the slot holds one.
func (e *Encoder) boxAs(val types.Value, typ types.Type) (types.Boxed, error) {
	switch typ.Kind() {
	case types.KindI1:
		n, ok := asInt(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i1", ErrTypeMismatch)
		}
		return types.BoxI1(n != 0), nil
	case types.KindI8:
		n, ok := asInt(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i8", ErrTypeMismatch)
		}
		return types.BoxI8(int8(n)), nil
	case types.KindI32:
		n, ok := asInt(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i32", ErrTypeMismatch)
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return 0, fmt.Errorf("%w: %d overflows i32", ErrValueOverflow, n)
		}
		return types.BoxI32(int32(n)), nil
	case types.KindI64:
		n, ok := asInt(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i64", ErrTypeMismatch)
		}
		if types.IsBoxable(n) {
			return types.BoxI64(n), nil
		}
		return e.alloc(types.I64(n))
	case types.KindF32:
		f, ok := asFloat(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=f32", ErrTypeMismatch)
		}
		return types.BoxF32(float32(f)), nil
	case types.KindF64:
		f, ok := asFloat(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=f64", ErrTypeMismatch)
		}
		return types.BoxF64(f), nil
	case types.KindRef:
		if err := verify(val, typ); err != nil {
			return 0, err
		}
		return e.ref(val)
	default:
		return 0, fmt.Errorf("%w: target=%s", ErrTypeMismatch, typ)
	}
}

// boxed holds val the way the VM holds a value on its stack, so a key or an
// element reaches (*Interpreter).mapKey in the same form an opcode would hand
// it over. It is the error-returning counterpart of (*Interpreter).box, which
// panics on heap exhaustion where conversion must report it.
func (e *Encoder) box(val types.Value) (types.Boxed, error) {
	switch v := val.(type) {
	case types.Boxed:
		return v, nil
	case types.I64:
		return e.boxI64(int64(v))
	}
	if _, _, scalar := bitsOf(val); scalar {
		return e.interp.box(val), nil
	}
	return e.ref(val)
}

// ref publishes val as a heap reference, reusing one it already carries.
func (e *Encoder) ref(val types.Value) (types.Boxed, error) {
	if val == nil || types.IsNull(val) {
		return types.BoxedNull, nil
	}
	switch v := val.(type) {
	case types.Boxed:
		if v.Kind() == types.KindRef {
			return v, nil
		}
		return e.alloc(types.Unbox(v))
	case types.Ref:
		return e.retain(int(v))
	default:
		return e.alloc(val)
	}
}

func (e *Encoder) alloc(val types.Value) (types.Boxed, error) {
	addr, err := e.interp.Alloc(val)
	if err != nil {
		return 0, err
	}
	e.owned = append(e.owned, addr)
	return types.BoxRef(addr), nil
}

// retain takes ownership of a heap value the Go side only named, so a marshaled
// alias keeps its target alive for as long as the value holding it.
func (e *Encoder) retain(addr int) (types.Boxed, error) {
	if _, err := e.interp.Retain(addr); err != nil {
		return 0, err
	}
	e.owned = append(e.owned, addr)
	return types.BoxRef(addr), nil
}

// discard releases every reference the conversion published. A conversion that
// fails partway has no result to own them, so Marshal calls it to leave the
// heap as it found it rather than stranding what it already allocated.
func (e *Encoder) discard() {
	for _, addr := range e.owned {
		e.interp.release(addr)
	}
	e.owned = nil
}

// boxI64 stores a 64-bit integer inline when it fits the boxed payload, and on
// the heap when it does not.
func (e *Encoder) boxI64(n int64) (types.Boxed, error) {
	if types.IsBoxable(n) {
		return types.BoxI64(n), nil
	}
	return e.alloc(types.I64(n))
}

// wrap exposes a bound Go function to the VM. Scratch is allocated inside the
// returned closure on every call: the *HostFunction may be reachable from
// program constants, so pooled interpreters built from the same program share
// it and may call it concurrently from separate goroutines.
func (e *Encoder) wrap(fn reflect.Value, typ *types.FunctionType) *HostFunction {
	registry := e.registry
	sig := fn.Type()

	return NewHostFunction(typ, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
		if len(params) != len(typ.Params) {
			return nil, fmt.Errorf("%w: got %d params, want %d", ErrTypeMismatch, len(params), len(typ.Params))
		}
		in := make([]reflect.Value, sig.NumIn())
		dec := &Decoder{interp: i, registry: registry}
		at := 0
		for idx := range in {
			param := sig.In(idx)
			if param == typeContext {
				ctx := i.Context()
				if ctx == nil {
					ctx = context.Background()
				}
				in[idx] = reflect.ValueOf(ctx)
				continue
			}
			arg := reflect.New(param)
			value, err := registry.resolve(i, params[at])
			if err != nil {
				return nil, fmt.Errorf("function param %d: %w", at, err)
			}
			c, err := registry.conversion(param)
			if err != nil {
				return nil, err
			}
			if err := c.set(dec, value, arg.UnsafePointer()); err != nil {
				return nil, fmt.Errorf("function param %d: %w", at, err)
			}
			in[idx] = arg.Elem()
			at++
		}

		out := fn.Call(in)
		if len(out) > 0 && out[len(out)-1].Type().Implements(typeError) {
			last := out[len(out)-1]
			if err := hostError(last); err != nil {
				return nil, err
			}
			out = out[:len(out)-1]
		}

		enc := &Encoder{interp: i, registry: registry}
		returns := make([]types.Boxed, len(out))
		for idx := range out {
			holder := reflect.New(out[idx].Type())
			holder.Elem().Set(out[idx])
			c, err := registry.conversion(out[idx].Type())
			if err != nil {
				return nil, err
			}
			value, err := c.value(enc, holder.UnsafePointer())
			if err != nil {
				enc.discard()
				return nil, fmt.Errorf("function return %d: %w", idx, err)
			}
			boxed, err := enc.boxAs(value, typ.Returns[idx])
			if err != nil {
				enc.discard()
				return nil, fmt.Errorf("function return %d: %w", idx, err)
			}
			returns[idx] = boxed
		}
		return returns, nil
	})
}

// boxing derives a slot conversion from a standalone one, for every conversion whose
// kind cannot write a slot directly.
func boxing(p *conversion) func(*Encoder, unsafe.Pointer) (types.Boxed, error) {
	value, vm := p.value, p.vm
	return func(e *Encoder, ptr unsafe.Pointer) (types.Boxed, error) {
		val, err := value(e, ptr)
		if err != nil {
			return 0, err
		}
		return e.boxAs(val, vm)
	}
}

// marshalConverting calls the type's own MarshalVM through the pointer method
// set, so a value receiver is reached without copying the value out of its slot.
func marshalConverting(t reflect.Type) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		return reflect.NewAt(t, p).Interface().(VMMarshaler).MarshalVM(e)
	}
}

// marshalNative passes a Go value that already holds a VM value through,
// resolving a reference it carries so the caller sees the value itself.
func marshalNative(t reflect.Type) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		val, ok := reflect.NewAt(t, p).Elem().Interface().(types.Value)
		if !ok || val == nil {
			return types.Null, nil
		}
		return e.registry.resolve(e.interp, val)
	}
}

// marshalDynamic converts through the concrete type an interface slot holds.
func marshalDynamic(t reflect.Type) (
	func(*Encoder, unsafe.Pointer) (types.Value, error),
	func(*Encoder, unsafe.Pointer) (types.Boxed, error),
) {
	value := func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		rv := reflect.NewAt(t, p).Elem()
		if rv.IsNil() {
			return types.Null, nil
		}
		elem := rv.Elem()
		holder := reflect.New(elem.Type())
		holder.Elem().Set(elem)
		return e.Encode(elem.Type(), holder.UnsafePointer())
	}
	return value, func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) {
		val, err := value(e, p)
		if err != nil {
			return 0, err
		}
		return e.ref(val)
	}
}

// marshalPointer follows a pointer, refusing one that reaches itself.
//
// A pointer to a struct is the caller asking for a reference, so it produces a
// live view rather than a copy: copying would drop exactly the aliasing that
// made the caller pass a pointer. A view is shallow, so it cannot reach itself
// and needs no cycle check.
func marshalPointer(elem *conversion) (
	func(*Encoder, unsafe.Pointer) (types.Value, error),
	func(*Encoder, unsafe.Pointer) (types.Boxed, error),
) {
	if elem.view != nil {
		value := func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
			target := *(*unsafe.Pointer)(p)
			if target == nil {
				return types.Null, nil
			}
			return elem.view(e, target)
		}
		return value, func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			val, err := value(e, p)
			if err != nil {
				return 0, err
			}
			return e.ref(val)
		}
	}
	value := func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		target := *(*unsafe.Pointer)(p)
		if target == nil {
			return types.Null, nil
		}
		if !e.enter(target) {
			return nil, fmt.Errorf("%w: %s", ErrMarshalCycle, elem.typ)
		}
		defer e.leave(target)
		return elem.value(e, target)
	}
	return value, func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) {
		target := *(*unsafe.Pointer)(p)
		if target == nil {
			return types.BoxedNull, nil
		}
		if !e.enter(target) {
			return 0, fmt.Errorf("%w: %s", ErrMarshalCycle, elem.typ)
		}
		defer e.leave(target)
		return elem.box(e, target)
	}
}

// marshalFunc exposes a Go function value as a bound VM function.
func marshalFunc(t reflect.Type, typ *types.FunctionType) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		fn := reflect.NewAt(t, p).Elem()
		if fn.IsNil() {
			return types.Null, nil
		}
		return e.wrap(fn, typ), nil
	}
}

// marshalStruct writes exported fields into a native VM struct.
func marshalStruct(vm *types.StructType, fields []field) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		out := types.NewStruct(vm)
		for idx, f := range fields {
			boxed, err := f.conversion.box(e, unsafe.Add(p, f.offset))
			if err != nil {
				return nil, fmt.Errorf("struct field %s: %w", f.name, err)
			}
			if vm.Fields[idx].Kind == types.KindI64 {
				out.SetRaw(idx, uint64(e.interp.unboxI64(boxed)))
			} else {
				out.SetField(idx, boxed)
			}
		}
		return out, nil
	}
}

// marshalHostStruct exposes the Go struct at p as a live view instead of copying
// it. The view keeps p after the call returns, which is what makes it live: an
// unsafe.Pointer is pointer-shaped to the GC, so the Go struct stays alive for
// as long as the VM holds the view.
func marshalHostStruct(t reflect.Type, vm *types.StructType, fields []field) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		return &HostStruct{typ: vm, fields: fields, registry: e.registry, rtyp: t, ptr: p}, nil
	}
}

func marshalHostArray(t reflect.Type, vm *types.ArrayType, elem *conversion, copy func(*Encoder, unsafe.Pointer) (types.Value, error)) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	bounds, stride := span(t), t.Elem().Size()
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		return &HostArray{
			typ: vm, elem: elem, copy: copy, registry: e.registry,
			bounds: bounds, stride: stride, rtyp: t, ptr: p,
		}, nil
	}
}

func marshalHostMap(t reflect.Type, vm *types.MapType, index func(*Decoder, types.Value, unsafe.Pointer) error, elem *conversion, copy func(*Encoder, unsafe.Pointer) (types.Value, error)) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		return &HostMap{typ: vm, index: index, elem: elem, copy: copy, registry: e.registry, rtyp: t, ptr: p}, nil
	}
}

// hostError reports the error a Go call returned in its trailing result, or nil
// when that result is a nil error.
func hostError(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		if v.IsNil() {
			return nil
		}
	}
	return v.Interface().(error)
}

// verify reports whether a standalone value may occupy a ref slot of typ.
func verify(val types.Value, typ types.Type) error {
	if typ.Equals(types.TypeAny) {
		return nil
	}
	if val == nil {
		return fmt.Errorf("%w: source=<nil> target=%s", ErrTypeMismatch, typ)
	}
	if actual := val.Type(); actual == nil || !actual.Equals(typ) {
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, typ)
	}
	return nil
}

// span reports the base pointer and length of the Go array or slice at p.
func span(t reflect.Type) func(unsafe.Pointer) (unsafe.Pointer, int) {
	if t.Kind() == reflect.Array {
		n := t.Len()
		return func(p unsafe.Pointer) (unsafe.Pointer, int) { return p, n }
	}
	return func(p unsafe.Pointer) (unsafe.Pointer, int) {
		h := (*sliceHeader)(p)
		return h.data, h.len
	}
}

// marshalArray converts a Go array or slice. An element kind the VM stores
// unboxed selects a typed array at compile time; anything else boxes into the
// generic representation, where an element can reach the container again and
// the walk has to refuse the cycle.
func marshalArray(t reflect.Type, at *types.ArrayType, elem *conversion) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	bounds, stride := span(t), t.Elem().Size()
	switch elem.kind {
	case reflect.Bool:
		return typedArray(bounds, stride, func(p unsafe.Pointer) bool { return *(*bool)(p) })
	case reflect.Int8:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int8 { return *(*int8)(p) })
	case reflect.Uint8:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int8 { return int8(*(*uint8)(p)) })
	case reflect.Int16:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int32 { return int32(*(*int16)(p)) })
	case reflect.Int32:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int32 { return *(*int32)(p) })
	case reflect.Uint16:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int32 { return int32(*(*uint16)(p)) })
	case reflect.Uint32:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int32 { return int32(*(*uint32)(p)) })
	case reflect.Int:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int64 { return int64(*(*int)(p)) })
	case reflect.Int64:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int64 { return *(*int64)(p) })
	case reflect.Uint:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int64 { return int64(*(*uint)(p)) })
	case reflect.Uint64:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int64 { return int64(*(*uint64)(p)) })
	case reflect.Uintptr:
		return typedArray(bounds, stride, func(p unsafe.Pointer) int64 { return int64(*(*uintptr)(p)) })
	case reflect.Float32:
		return typedArray(bounds, stride, func(p unsafe.Pointer) float32 { return *(*float32)(p) })
	case reflect.Float64:
		return typedArray(bounds, stride, func(p unsafe.Pointer) float64 { return *(*float64)(p) })
	}
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		base, n := bounds(p)
		if base != nil {
			if !e.enter(base) {
				return nil, fmt.Errorf("%w: %s", ErrMarshalCycle, t)
			}
			defer e.leave(base)
		}
		elems := make([]types.Boxed, n)
		for idx := range elems {
			boxed, err := elem.box(e, unsafe.Add(base, uintptr(idx)*stride))
			if err != nil {
				return nil, fmt.Errorf("array element %d: %w", idx, err)
			}
			elems[idx] = boxed
		}
		return types.NewArray(at, elems...), nil
	}
}

func typedArray[T int8 | int32 | int64 | float32 | float64 | bool](
	bounds func(unsafe.Pointer) (unsafe.Pointer, int),
	stride uintptr,
	read func(unsafe.Pointer) T,
) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	return func(_ *Encoder, p unsafe.Pointer) (types.Value, error) {
		base, n := bounds(p)
		out := make(types.TypedArray[T], n)
		for idx := range out {
			out[idx] = read(unsafe.Add(base, uintptr(idx)*stride))
		}
		return out, nil
	}
}

// marshalMap converts a Go map. Go map memory is not addressable, so iteration
// is the one place the run step still uses reflection; the destination writer,
// which would otherwise switch over every map representation per call, is
// chosen once at compile time.
func marshalMap(t reflect.Type, mt *types.MapType, key, elem *conversion) func(*Encoder, unsafe.Pointer) (types.Value, error) {
	write := mapWriter(mt, key)
	// A hosted entry keeps the address it was marshaled from, so it needs
	// storage of its own rather than the scratch every other entry shares. Go
	// map memory is not addressable, so such a view stands for a copy either
	// way; what matters is that two entries are not the same copy.
	shared := !key.host && !elem.host
	return func(e *Encoder, p unsafe.Pointer) (types.Value, error) {
		if target := *(*unsafe.Pointer)(p); target != nil {
			if !e.enter(target) {
				return nil, fmt.Errorf("%w: %s", ErrMarshalCycle, t)
			}
			defer e.leave(target)
		}
		rv := reflect.NewAt(t, p).Elem()
		out := types.NewMapForType(mt, rv.Len())
		entryKey, entryValue := reflect.New(t.Key()).Elem(), reflect.New(t.Elem()).Elem()
		for iter := rv.MapRange(); iter.Next(); {
			if !shared {
				entryKey, entryValue = reflect.New(t.Key()).Elem(), reflect.New(t.Elem()).Elem()
			}
			entryKey.SetIterKey(iter)
			entryValue.SetIterValue(iter)
			value, err := elem.box(e, entryValue.Addr().UnsafePointer())
			if err != nil {
				return nil, fmt.Errorf("map value: %w", err)
			}
			if err := write(e, out, entryKey.Addr().UnsafePointer(), value); err != nil {
				return nil, fmt.Errorf("map key: %w", err)
			}
		}
		return out, nil
	}
}

// mapWriter selects how one entry is stored, from the key kind the map type
// already fixed.
func mapWriter(mt *types.MapType, key *conversion) func(*Encoder, types.Value, unsafe.Pointer, types.Boxed) error {
	switch mt.KeyKind {
	case types.KindI1:
		return typedMap(key, func(val types.Value) (bool, error) {
			n, err := keyInt(val, mt.Key)
			return n != 0, err
		})
	case types.KindI8:
		return typedMap(key, func(val types.Value) (int8, error) {
			n, err := keyInt(val, mt.Key)
			return int8(n), err
		})
	case types.KindI32:
		return typedMap(key, func(val types.Value) (int32, error) {
			n, err := keyInt(val, mt.Key)
			if err != nil {
				return 0, err
			}
			if n < math.MinInt32 || n > math.MaxInt32 {
				return 0, fmt.Errorf("%w: %d overflows i32", ErrValueOverflow, n)
			}
			return int32(n), nil
		})
	case types.KindI64:
		return typedMap(key, func(val types.Value) (int64, error) { return keyInt(val, mt.Key) })
	case types.KindF32:
		return typedMap(key, func(val types.Value) (float32, error) {
			f, err := keyFloat(val, mt.Key)
			return float32(f), err
		})
	case types.KindF64:
		return typedMap(key, func(val types.Value) (float64, error) { return keyFloat(val, mt.Key) })
	}
	if mt.Key.Equals(types.TypeString) {
		return func(e *Encoder, m types.Value, p unsafe.Pointer, value types.Boxed) error {
			val, err := key.value(e, p)
			if err != nil {
				return err
			}
			text, ok := val.(types.String)
			if !ok {
				return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, mt.Key)
			}
			m.(*types.TypedMap[string]).Set(string(text), value)
			return nil
		}
	}
	if mt.KeyKind != types.KindRef {
		return func(*Encoder, types.Value, unsafe.Pointer, types.Boxed) error {
			return fmt.Errorf("%w: map key type=%s", ErrUnsupportedMarshalType, mt.Key)
		}
	}
	return func(e *Encoder, m types.Value, p unsafe.Pointer, value types.Boxed) error {
		val, err := key.value(e, p)
		if err != nil {
			return err
		}
		boxed, err := e.box(val)
		if err != nil {
			return err
		}
		k, entryKey := e.interp.mapKey(boxed)
		if old, replaced := m.(*types.Map).Set(k, types.MapEntry{Key: entryKey, Value: value}); replaced {
			e.interp.releaseBox(old.Key)
			e.interp.releaseBox(old.Value)
		}
		return nil
	}
}

// typedMap stores one entry of a map that holds its keys as Go values. The key
// is read as a standalone value and narrowed straight to K: routing it through a
// slot first would allocate a heap reference for an i64 too large to box and then
// drop it, since such a map never stores a key as a reference.
func typedMap[K comparable](key *conversion, narrow func(types.Value) (K, error)) func(*Encoder, types.Value, unsafe.Pointer, types.Boxed) error {
	return func(e *Encoder, m types.Value, p unsafe.Pointer, value types.Boxed) error {
		val, err := key.value(e, p)
		if err != nil {
			return err
		}
		k, err := narrow(val)
		if err != nil {
			return err
		}
		m.(*types.TypedMap[K]).Set(k, value)
		return nil
	}
}

// keyInt and keyFloat read a map key as the VM scalar its declared key type
// fixed, naming that type when the compiled conversion produced another.
func keyInt(val types.Value, typ types.Type) (int64, error) {
	n, ok := asInt(val)
	if !ok {
		return 0, fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, typ)
	}
	return n, nil
}

func keyFloat(val types.Value, typ types.Type) (float64, error) {
	f, ok := asFloat(val)
	if !ok {
		return 0, fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, typ)
	}
	return f, nil
}
