package interp

import (
	"context"
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

// ValueUnmarshaler lets a Go type decode itself. The registry compiles an
// implementing type into an Unmarshaler and resolves it ahead of the structural
// mapping.
type ValueUnmarshaler interface {
	UnmarshalVM(*Decoder, types.Value) error
}

// Decoder is one unmarshal conversion in flight. An Unmarshaler uses it to
// reach the interpreter and to decode the dependencies of the value it owns.
type Decoder struct {
	interp   *Interpreter
	registry *Registry
}

func (f UnmarshalerFunc) Unmarshal(d *Decoder, val types.Value, p unsafe.Pointer) error {
	return f(d, val, p)
}

// Interp returns the interpreter this conversion runs against.
func (d *Decoder) Interp() *Interpreter { return d.interp }

// Unmarshal writes val into the Go value of type t at p, resolving t through
// the same codec that started the conversion.
func (d *Decoder) Unmarshal(val types.Value, t reflect.Type, p unsafe.Pointer) error {
	c, err := d.registry.conversion(t)
	if err != nil {
		return err
	}
	return c.set(d, val, p)
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
			return d.registry.resolve(d.interp, v.Elems[i])
		}, nil
	default:
		return 0, nil, fmt.Errorf("%w: source=%T", ErrTypeMismatch, val)
	}
}

// unmarshalConverting calls the type's own UnmarshalVM.
func unmarshalConverting(t reflect.Type) func(*Decoder, types.Value, unsafe.Pointer) error {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		return reflect.NewAt(t, p).Interface().(ValueUnmarshaler).UnmarshalVM(d, val)
	}
}

// unmarshalValue stores a VM value into a Go slot that holds one directly,
// covering both a types.Value-implementing type and an interface slot.
func unmarshalValue(t reflect.Type) func(*Decoder, types.Value, unsafe.Pointer) error {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		value, err := d.registry.resolve(d.interp, val)
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

// unmarshalPointer allocates the pointee and decodes into it, leaving a nil
// pointer for a null value.
func unmarshalPointer(elem *conversion) func(*Decoder, types.Value, unsafe.Pointer) error {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		if types.IsNull(val) {
			*(*unsafe.Pointer)(p) = nil
			return nil
		}
		out := reflect.New(elem.typ)
		if err := elem.set(d, val, out.UnsafePointer()); err != nil {
			return err
		}
		*(*unsafe.Pointer)(p) = out.UnsafePointer()
		return nil
	}
}

// unmarshalFunc wraps a VM function as a Go function that marshals arguments,
// runs the VM function on the same interpreter, then decodes the results.
func unmarshalFunc(t reflect.Type, typ *types.FunctionType) func(*Decoder, types.Value, unsafe.Pointer) error {
	ctxParam := t.NumIn() > 0 && t.In(0) == typeContext
	failing := t.NumOut() > 0 && t.Out(t.NumOut()-1).Implements(typeError)

	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		target, ok := d.interp.callable(val)
		if !ok || target.Type() == nil || !target.Type().Equals(typ) {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, t)
		}
		registry := d.registry
		fn := reflect.MakeFunc(t, func(in []reflect.Value) []reflect.Value {
			out := make([]reflect.Value, t.NumOut())
			for idx := range out {
				out[idx] = reflect.Zero(t.Out(idx))
			}
			fail := func(err error) []reflect.Value {
				if !failing {
					panic(err)
				}
				out[len(out)-1] = reflect.ValueOf(err)
				return out
			}

			ctx := context.Background()
			offset := 0
			if ctxParam {
				if caller, _ := in[0].Interface().(context.Context); caller != nil {
					ctx = caller
				}
				offset = 1
			}

			enc := &Encoder{interp: d.interp, registry: registry}
			params := make([]types.Boxed, len(in)-offset)
			for idx := range params {
				arg := in[idx+offset]
				holder := reflect.New(arg.Type())
				holder.Elem().Set(arg)
				c, err := registry.conversion(arg.Type())
				if err != nil {
					return fail(err)
				}
				value, err := c.value(enc, holder.UnsafePointer())
				if err != nil {
					return fail(fmt.Errorf("function param %d: %w", idx, err))
				}
				boxed, err := enc.box(value, typ.Params[idx])
				if err != nil {
					return fail(fmt.Errorf("function param %d: %w", idx, err))
				}
				params[idx] = boxed
			}

			returns, err := d.interp.invoke(ctx, val, params)
			if err != nil {
				return fail(err)
			}
			defer func() {
				for _, value := range returns {
					d.interp.releaseBox(value)
				}
			}()
			for idx := range returns {
				result := reflect.New(t.Out(idx))
				if err := registry.Unmarshal(d.interp, returns[idx], result.Interface()); err != nil {
					return fail(fmt.Errorf("function return %d: %w", idx, err))
				}
				out[idx] = result.Elem()
			}
			return out
		})
		reflect.NewAt(t, p).Elem().Set(fn)
		return nil
	}
}

// unmarshalArray decodes a VM array into a Go array or slice.
func unmarshalArray(t reflect.Type, elem *conversion) func(*Decoder, types.Value, unsafe.Pointer) error {
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

// unmarshalMap decodes a VM map into a Go map.
func unmarshalMap(t reflect.Type, key, elem *conversion) func(*Decoder, types.Value, unsafe.Pointer) error {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		var out reflect.Value
		entryKey, entryValue := reflect.New(t.Key()).Elem(), reflect.New(t.Elem()).Elem()
		reserve := func(n int) { out = reflect.MakeMapWithSize(t, n) }
		set := func(k, v types.Value) error {
			if err := key.set(d, k, entryKey.Addr().UnsafePointer()); err != nil {
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

// entries walks a VM map, resolving one representation switch per map and
// reporting the entry count first so the destination is sized once.
func (d *Decoder) entries(val types.Value, reserve func(int), set func(key, value types.Value) error) error {
	var err error
	visit := func(key types.Value, value types.Boxed) {
		if err != nil {
			return
		}
		elem, resolveErr := d.registry.resolve(d.interp, value)
		if resolveErr != nil {
			err = fmt.Errorf("map value: %w", resolveErr)
			return
		}
		err = set(key, elem)
	}
	switch m := val.(type) {
	case *types.TypedMap[bool]:
		reserve(m.Len())
		m.Range(func(k bool, v types.Boxed) { visit(types.I1(k), v) })
	case *types.TypedMap[int8]:
		reserve(m.Len())
		m.Range(func(k int8, v types.Boxed) { visit(types.I8(k), v) })
	case *types.TypedMap[int32]:
		reserve(m.Len())
		m.Range(func(k int32, v types.Boxed) { visit(types.I32(k), v) })
	case *types.TypedMap[int64]:
		reserve(m.Len())
		m.Range(func(k int64, v types.Boxed) { visit(types.I64(k), v) })
	case *types.TypedMap[float32]:
		reserve(m.Len())
		m.Range(func(k float32, v types.Boxed) { visit(types.F32(k), v) })
	case *types.TypedMap[float64]:
		reserve(m.Len())
		m.Range(func(k float64, v types.Boxed) { visit(types.F64(k), v) })
	case *types.TypedMap[string]:
		reserve(m.Len())
		m.Range(func(k string, v types.Boxed) { visit(types.String(k), v) })
	case *types.Map:
		reserve(m.Len())
		m.Range(func(k types.MapKey, entry types.MapEntry) {
			key, keyErr := d.key(k, entry)
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

// key recovers the VM value a generic map entry is keyed by.
func (d *Decoder) key(k types.MapKey, entry types.MapEntry) (types.Value, error) {
	switch k.Kind {
	case types.KindI32:
		return types.I32(int32(k.Bits)), nil
	case types.KindI64:
		return types.I64(int64(k.Bits)), nil
	case types.KindF32:
		return types.F32(math.Float32frombits(uint32(k.Bits))), nil
	case types.KindF64:
		return types.F64(math.Float64frombits(k.Bits)), nil
	case types.KindText:
		return types.String(k.Text), nil
	default:
		return d.registry.resolve(d.interp, entry.Key)
	}
}

// unmarshalStruct decodes a VM struct into a Go struct, matching each Go field
// to the VM field of the same name and falling back to the next unused data
// slot when no name matches.
func unmarshalStruct(fields []field) func(*Decoder, types.Value, unsafe.Pointer) error {
	return func(d *Decoder, val types.Value, p unsafe.Pointer) error {
		value, err := d.registry.resolve(d.interp, val)
		if err != nil {
			return err
		}
		src, ok := value.(*types.Struct)
		if !ok {
			return fmt.Errorf("%w: source=%T", ErrTypeMismatch, value)
		}
		used := make([]bool, len(src.Typ.Fields))
		for _, f := range fields {
			at, ok := source(src.Typ, f.name, used)
			if !ok {
				continue
			}
			used[at] = true
			var field types.Value
			if src.Typ.Fields[at].Kind == types.KindI64 {
				field = types.I64(int64(src.Raw(at)))
			} else if field, err = d.registry.resolve(d.interp, src.Field(at)); err != nil {
				return fmt.Errorf("struct field %s: %w", f.name, err)
			}
			if err := f.conversion.set(d, field, unsafe.Add(p, f.offset)); err != nil {
				return fmt.Errorf("struct field %s: %w", f.name, err)
			}
		}
		return nil
	}
}

// source locates the field a Go field reads from: the one sharing its name,
// or the next unused slot that is not a bound function.
func source(typ *types.StructType, name string, used []bool) (int, bool) {
	for idx, f := range typ.Fields {
		if f.Name == name {
			return idx, true
		}
	}
	for idx, f := range typ.Fields {
		if used[idx] {
			continue
		}
		if _, bound := f.Type.(*types.FunctionType); bound {
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

func setSigned[T ~int | ~int8 | ~int16 | ~int32 | ~int64]() func(*Decoder, types.Value, unsafe.Pointer) error {
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

func setUnsigned[T ~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr]() func(*Decoder, types.Value, unsafe.Pointer) error {
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

func setFloat[T ~float32 | ~float64]() func(*Decoder, types.Value, unsafe.Pointer) error {
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
