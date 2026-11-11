package interp

import (
	"fmt"
	"math"
	"reflect"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

// Unmarshaler converts a VM value into one Go type. Register one with
// WithUnmarshaler to override how the built-in codec handles that type.
type Unmarshaler interface {
	// Unmarshal writes val into the Go value at p. p addresses a live value of
	// the registered type and is valid only for the duration of the call.
	Unmarshal(*Decoder, types.Value, unsafe.Pointer) error
}

// UnmarshalerFunc adapts a function to Unmarshaler.
type UnmarshalerFunc func(*Decoder, types.Value, unsafe.Pointer) error

// VMUnmarshaler lets a Go type decode itself. The registry compiles an
// implementing type into an Unmarshaler and resolves it ahead of the structural
// mapping.
type VMUnmarshaler interface {
	UnmarshalVM(*Decoder, types.Value) error
}

// Decoder is one unmarshal conversion in flight. An Unmarshaler uses it to
// reach the interpreter and to decode the dependencies of the value it owns.
type Decoder struct {
	interp   *Interpreter
	registry *Registry
}

// Unmarshal decodes an encoded value.
func (f UnmarshalerFunc) Unmarshal(d *Decoder, val types.Value, p unsafe.Pointer) error {
	return f(d, val, p)
}

// Interp returns the interpreter this conversion runs against.
func (d *Decoder) Interp() *Interpreter {
	return d.interp
}

// Decode writes val into the Go value of type t at p, resolving t through the
// same codec that started the conversion. Like Unmarshal, it takes a slot or a
// standalone value: an Unmarshaler reading a container hands over what the
// container held, and the conversion below sees the value that slot names.
func (d *Decoder) Decode(val types.Value, t reflect.Type, p unsafe.Pointer) error {
	c, err := d.registry.conversion(t)
	if err != nil {
		return err
	}
	value, err := d.interp.deref(val)
	if err != nil {
		return err
	}
	return c.set(d, value, p)
}

// unmarshalConverting calls the type's own UnmarshalVM.
func unmarshalConverting(t reflect.Type) UnmarshalerFunc {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		return reflect.NewAt(t, p).Interface().(VMUnmarshaler).UnmarshalVM(d, val)
	}
}

// unmarshalValue stores a VM value into a Go slot that holds one directly,
// covering both a types.Value-implementing type and an interface slot.
func unmarshalValue(t reflect.Type) UnmarshalerFunc {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		value, err := d.interp.deref(val)
		if err != nil {
			return err
		}
		dst := reflect.NewAt(t, p).Elem()
		rv := reflect.ValueOf(value)
		if value == nil || !rv.IsValid() {
			dst.SetZero()
			return nil
		}
		if !rv.Type().AssignableTo(t) {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, t)
		}
		dst.Set(rv)
		return nil
	}
}

// unmarshalHost recovers the Go value a host value stands for, unexported state
// included, and decodes structurally from any other VM value. Both halves
// matter: a method reached through a host value mutates the caller's value,
// while the same conversion applied to a struct guest code built still runs.
func unmarshalHost(t reflect.Type, structural UnmarshalerFunc) UnmarshalerFunc {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		value, err := d.interp.deref(val)
		if err != nil {
			return err
		}
		if src, ok := hosting(value, t); ok {
			reflect.NewAt(t, p).Elem().Set(reflect.NewAt(t, src).Elem())
			return nil
		}
		return structural(d, value, p)
	}
}

// unmarshalPointer allocates the pointee and decodes into it, leaving a nil
// pointer for a null value.
func unmarshalPointer(elem *conversion) UnmarshalerFunc {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		if types.IsNull(val) {
			*(*unsafe.Pointer)(p) = nil
			return nil
		}
		if elem.view != nil {
			value, err := d.interp.deref(val)
			if err != nil {
				return err
			}

			if target, ok := hosting(value, elem.typ); ok {
				*(*unsafe.Pointer)(p) = target
				return nil
			}
		}
		out := reflect.New(elem.typ)
		if err := elem.set(d, val, out.UnsafePointer()); err != nil {
			return err
		}
		*(*unsafe.Pointer)(p) = out.UnsafePointer()
		return nil
	}
}

// unmarshalArray decodes a VM array into a Go array or slice.
func unmarshalArray(t reflect.Type, elem *conversion) UnmarshalerFunc {
	stride := t.Elem().Size()
	if t.Kind() == reflect.Array {
		size := t.Len()
		return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
			n, read, err := d.elements(val)
			if err != nil {
				return err
			}
			if n != size {
				return fmt.Errorf("%w: array length %d does not match %d", ErrValueOverflow, n, size)
			}
			return fill(d, elem, p, stride, n, read)
		}
	}
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		n, read, err := d.elements(val)
		if err != nil {
			return err
		}
		out := reflect.MakeSlice(t, n, n)
		if err := fill(d, elem, out.UnsafePointer(), stride, n, read); err != nil {
			return err
		}
		reflect.NewAt(t, p).Elem().Set(out)
		return nil
	}
}

// unmarshalKey reverses Interpreter.mapKey for dynamic host maps: numeric kinds
// use their normalized Go type, strings use content, and other refs use identity.
func unmarshalKey(d *Decoder, val types.Value, p unsafe.Pointer) error {
	value, err := d.interp.deref(val)
	if err != nil {
		return err
	}
	var key any = value
	switch v := value.(type) {
	case types.I1:
		key = int32(0)
		if v {
			key = int32(1)
		}
	case types.I8:
		key = int32(v)
	case types.I32:
		key = int32(v)
	case types.I64:
		key = int64(v)
	case types.F32:
		key = float32(v)
	case types.F64:
		key = float64(v)
	case types.String:
		key = string(v)
	}
	*(*any)(p) = key
	return nil
}

// unmarshalMap decodes a VM map into a Go map.
func unmarshalMap(t reflect.Type, index UnmarshalerFunc, elem *conversion) UnmarshalerFunc {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		var out reflect.Value
		entryKey, entryValue := reflect.New(t.Key()).Elem(), reflect.New(t.Elem()).Elem()
		reserve := func(n int) { out = reflect.MakeMapWithSize(t, n) }
		set := func(k, v types.Value) error {
			if err := index(d, k, entryKey.Addr().UnsafePointer()); err != nil {
				return fmt.Errorf("map key: %w", err)
			}
			if err := elem.set(d, v, entryValue.Addr().UnsafePointer()); err != nil {
				return fmt.Errorf("map value: %w", err)
			}
			out.SetMapIndex(entryKey, entryValue)
			return nil
		}
		if err := d.entries(val, reserve, set); err != nil {
			return err
		}
		reflect.NewAt(t, p).Elem().Set(out)
		return nil
	}
}

// elements reports the length of a VM array and a reader for its elements. The
// source representation is resolved once per array rather than per element.
func (d *Decoder) elements(val types.Value) (int, func(int) (types.Value, error), error) {
	switch v := val.(type) {
	case types.TypedArray[bool]:
		return len(v), func(i int) (types.Value, error) { return types.I1(v[i]), nil }, nil
	case types.TypedArray[int8]:
		return len(v), func(i int) (types.Value, error) { return types.I32(v[i]), nil }, nil
	case types.TypedArray[int32]:
		return len(v), func(i int) (types.Value, error) { return types.I32(v[i]), nil }, nil
	case types.TypedArray[int64]:
		return len(v), func(i int) (types.Value, error) { return types.I64(v[i]), nil }, nil
	case types.TypedArray[float32]:
		return len(v), func(i int) (types.Value, error) { return types.F32(v[i]), nil }, nil
	case types.TypedArray[float64]:
		return len(v), func(i int) (types.Value, error) { return types.F64(v[i]), nil }, nil
	case *types.Array:
		return len(v.Elems), func(i int) (types.Value, error) {
			return d.interp.deref(v.Elems[i])
		}, nil
	default:
		return 0, nil, fmt.Errorf("%w: source=%T", ErrTypeMismatch, val)
	}
}

// entries walks a VM map, resolving one representation switch per map and
// reporting the entry count first so the destination is sized once.
func (d *Decoder) entries(val types.Value, reserve func(int), set func(key, value types.Value) error) error {
	var err error
	visit := func(key types.Value, value types.Boxed) {
		if err != nil {
			return
		}
		elem, resolveErr := d.interp.deref(value)
		if resolveErr != nil {
			err = fmt.Errorf("map value: %w", resolveErr)
			return
		}
		err = set(key, elem)
	}
	switch m := val.(type) {
	case *types.TypedMap[bool]:
		rangeKeys(m.Len(), m.Range, func(k bool) types.Value { return types.I1(k) }, reserve, visit)
	case *types.TypedMap[int8]:
		rangeKeys(m.Len(), m.Range, func(k int8) types.Value { return types.I8(k) }, reserve, visit)
	case *types.TypedMap[int32]:
		rangeKeys(m.Len(), m.Range, func(k int32) types.Value { return types.I32(k) }, reserve, visit)
	case *types.TypedMap[int64]:
		rangeKeys(m.Len(), m.Range, func(k int64) types.Value { return types.I64(k) }, reserve, visit)
	case *types.TypedMap[float32]:
		rangeKeys(m.Len(), m.Range, func(k float32) types.Value { return types.F32(k) }, reserve, visit)
	case *types.TypedMap[float64]:
		rangeKeys(m.Len(), m.Range, func(k float64) types.Value { return types.F64(k) }, reserve, visit)
	case *types.StringMap:
		rangeKeys(m.Len(), m.Range, func(k string) types.Value { return types.String(k) }, reserve, visit)
	case *types.Map:
		reserve(m.Len())
		m.Range(func(k types.MapKey, entry types.MapEntry) {
			key, keyErr := d.interp.deref(k.Value(entry))
			if keyErr != nil {
				if err == nil {
					err = fmt.Errorf("map key: %w", keyErr)
				}
				return
			}
			visit(key, entry.Value)
		})
	default:
		return fmt.Errorf("%w: source=%T", ErrTypeMismatch, val)
	}
	return err
}

func fill(d *Decoder, elem *conversion, base unsafe.Pointer, stride uintptr, n int, read func(int) (types.Value, error)) error {
	for idx := range n {
		value, err := read(idx)
		if err != nil {
			return fmt.Errorf("array element %d: %w", idx, err)
		}
		if err := elem.set(d, value, unsafe.Add(base, uintptr(idx)*stride)); err != nil {
			return fmt.Errorf("array element %d: %w", idx, err)
		}
	}
	return nil
}

// rangeKeys reserves a map's size n, then visits each entry under its boxed key.
func rangeKeys[K any](n int, each func(func(K, types.Boxed)), key func(K) types.Value, reserve func(int), visit func(types.Value, types.Boxed)) {
	reserve(n)
	each(func(k K, v types.Boxed) { visit(key(k), v) })
}

// unmarshalStruct decodes a VM struct into a Go struct, matching each Go field
// to the VM field of the same name and falling back to the next unused data
// slot when no name matches.
func unmarshalStruct(fields []field) UnmarshalerFunc {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		value, err := d.interp.deref(val)
		if err != nil {
			return err
		}
		src, ok := value.(*types.Struct)
		if !ok {
			return fmt.Errorf("%w: source=%T", ErrTypeMismatch, value)
		}
		used := make([]bool, len(src.Typ.Fields))
		for _, f := range fields {
			at, ok := fieldIndex(src.Typ, f.name, used)
			if !ok {
				continue
			}
			used[at] = true
			var field types.Value
			if src.Typ.Fields[at].Kind == types.KindI64 {
				field = types.I64(int64(src.Raw(at)))
			} else if field, err = d.interp.deref(src.Field(at)); err != nil {
				return fmt.Errorf("struct field %s: %w", f.name, err)
			}
			if err := f.conversion.set(d, field, unsafe.Add(p, f.offset)); err != nil {
				return fmt.Errorf("struct field %s: %w", f.name, err)
			}
		}
		return nil
	}
}

// fieldIndex locates the field a Go field reads from: the one sharing its name,
// or the next unused slot that does not hold a function.
func fieldIndex(typ *types.StructType, name string, used []bool) (int, bool) {
	for idx, f := range typ.Fields {
		if f.Name == name {
			return idx, true
		}
	}
	for idx, f := range typ.Fields {
		if used[idx] {
			continue
		}
		if _, callable := f.Type.(*types.FunctionType); callable {
			continue
		}
		return idx, true
	}
	return 0, false
}

func setBool(_ *Decoder, val types.Value, p unsafe.Pointer) error {
	n, ok := asInt(val)
	if !ok {
		return fmt.Errorf("%w: source=%T target=bool", ErrTypeMismatch, val)
	}
	*(*bool)(p) = n != 0
	return nil
}

func setString(_ *Decoder, val types.Value, p unsafe.Pointer) error {
	text, ok := val.(types.String)
	if !ok {
		return fmt.Errorf("%w: source=%T target=string", ErrTypeMismatch, val)
	}
	*(*string)(p) = string(text)
	return nil
}

func setSigned[T ~int | ~int8 | ~int16 | ~int32 | ~int64]() UnmarshalerFunc {
	target := reflect.TypeFor[T]()
	return func(_ *Decoder, val types.Value, p unsafe.Pointer) error {
		n, ok := asInt(val)
		if !ok {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, target)
		}
		if int64(T(n)) != n {
			return fmt.Errorf("%w: %d overflows %s", ErrValueOverflow, n, target)
		}
		*(*T)(p) = T(n)
		return nil
	}
}

func setUnsigned[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr]() UnmarshalerFunc {
	target := reflect.TypeFor[T]()
	return func(_ *Decoder, val types.Value, p unsafe.Pointer) error {
		n, ok := asUint(val)
		if !ok {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, target)
		}
		if uint64(T(n)) != n {
			return fmt.Errorf("%w: %d overflows %s", ErrValueOverflow, n, target)
		}
		*(*T)(p) = T(n)
		return nil
	}
}

func setFloat[T ~float32 | ~float64]() UnmarshalerFunc {
	target := reflect.TypeFor[T]()
	return func(_ *Decoder, val types.Value, p unsafe.Pointer) error {
		f, ok := asFloat(val)
		if !ok {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, target)
		}
		if math.IsInf(float64(T(f)), 0) && !math.IsInf(f, 0) {
			return fmt.Errorf("%w: %g overflows %s", ErrValueOverflow, f, target)
		}
		*(*T)(p) = T(f)
		return nil
	}
}
