package interp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

// Codec converts Go values to and from VM values. WithCodec installs one on an
// interpreter; Marshal and Unmarshal delegate to it unchanged.
type Codec interface {
	Marshal(*Interpreter, any) (types.Value, error)
	Unmarshal(*Interpreter, types.Value, any) error
}

// Registry is the built-in Codec. It resolves one conversion per Go type,
// compiles reflection away on first use, and caches the result, so execution
// reads and writes Go memory through stored offsets rather than reflection.
//
// Registration happens during construction, so a Registry never changes after
// NewRegistry returns and may be shared by pooled interpreters.
type Registry struct {
	entries     map[reflect.Type]*conversion
	conversions sync.Map
}

// registry collects registrations until NewRegistry freezes them.
type registry struct {
	entries map[reflect.Type]*conversion
}

// conversion is what one Go type compiles to. Every function it holds takes an
// unsafe.Pointer to a live Go value of that type, valid only for the duration
// of the call.
//
// value and box differ by position: value produces a standalone VM value, the
// form Marshal returns and an interface slot stores, while box produces a slot
// of vm, allocating a heap ref when the slot needs one. set is the reverse of
// both, because a slot resolves to a value before it is written back.
type conversion struct {
	typ  reflect.Type
	kind reflect.Kind
	vm   types.Type

	value func(*Encoder, unsafe.Pointer) (types.Value, error)
	box   func(*Encoder, unsafe.Pointer) (types.Boxed, error)
	set   func(*Decoder, types.Value, unsafe.Pointer) error
}

// field addresses one exported Go field through a compiled offset.
type field struct {
	name       string
	offset     uintptr
	conversion *conversion
}

// leaf is the compiled conversion of one primitive Go kind, held in a table so
// a field selects its access once at compile time instead of switching on the
// kind at every read.
type leaf struct {
	vm    types.Type
	value func(*Encoder, unsafe.Pointer) (types.Value, error)
	box   func(*Encoder, unsafe.Pointer) (types.Boxed, error)
	set   func(*Decoder, types.Value, unsafe.Pointer) error
}

// method binds one exported method of the receiver type as a VM function field.
type method struct {
	name  string
	index int
	typ   *types.FunctionType
}

var (
	ErrMarshalCycle           = errors.New("marshal cycle")
	ErrUnsupportedMarshalType = errors.New("unsupported marshal type")
	ErrInvalidUnmarshalTarget = errors.New("invalid unmarshal target")
	ErrValueOverflow          = errors.New("value overflow")
)

var (
	_ Codec = (*Registry)(nil)

	typeError   = reflect.TypeFor[error]()
	typeContext = reflect.TypeFor[context.Context]()
	typeValue   = reflect.TypeFor[types.Value]()

	typeValueMarshaler   = reflect.TypeFor[ValueMarshaler]()
	typeValueUnmarshaler = reflect.TypeFor[ValueUnmarshaler]()

	// natives are the VM runtime types a Go value may hold directly. They
	// bypass structural compilation and pass through as themselves.
	natives = map[reflect.Type]types.Type{
		reflect.TypeFor[types.I32]():    types.TypeI32,
		reflect.TypeFor[types.I64]():    types.TypeI64,
		reflect.TypeFor[types.F32]():    types.TypeF32,
		reflect.TypeFor[types.F64]():    types.TypeF64,
		reflect.TypeFor[types.Ref]():    types.TypeAny,
		reflect.TypeFor[types.Boxed]():  types.TypeAny,
		reflect.TypeFor[types.String](): types.TypeString,
	}
)

// NewRegistry builds the built-in codec. Defaults for standard-library types
// install first, so an option naming the same Go type replaces one.
func NewRegistry(opts ...func(*registry)) *Registry {
	r := &registry{entries: make(map[reflect.Type]*conversion)}
	for _, opt := range defaults() {
		opt(r)
	}
	for _, opt := range opts {
		opt(r)
	}
	return &Registry{entries: r.entries}
}

// WithMarshaler registers m as the conversion of Go type t into a VM value. vm
// is the VM type m produces; enclosing struct, array, and map layouts compile
// from it, so it must not vary per value.
func WithMarshaler(t reflect.Type, vm types.Type, m Marshaler) func(*registry) {
	return func(r *registry) {
		e := r.entry(t)
		e.vm = vm
		e.value = m.Marshal
	}
}

// WithUnmarshaler registers u as the conversion of a VM value into Go type t.
// It needs no VM type: the source value carries its own.
func WithUnmarshaler(t reflect.Type, u Unmarshaler) func(*registry) {
	return func(r *registry) { r.entry(t).set = u.Unmarshal }
}

func (r *registry) entry(t reflect.Type) *conversion {
	e, ok := r.entries[t]
	if !ok {
		e = &conversion{typ: t, vm: types.TypeAny}
		r.entries[t] = e
	}
	return e
}

func (r *Registry) Marshal(i *Interpreter, v any) (types.Value, error) {
	rv := reflect.ValueOf(v)
	if !rv.IsValid() {
		return types.Null, nil
	}
	p, err := r.conversion(rv.Type())
	if err != nil {
		return nil, err
	}
	holder := reflect.New(rv.Type())
	holder.Elem().Set(rv)
	defer runtime.KeepAlive(holder)
	return p.value(&Encoder{interp: i, registry: r}, holder.UnsafePointer())
}

func (r *Registry) Unmarshal(i *Interpreter, val types.Value, dst any) error {
	rv := reflect.ValueOf(dst)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return fmt.Errorf("%w: destination must be non-nil pointer", ErrInvalidUnmarshalTarget)
	}
	elem := rv.Type().Elem()
	p, err := r.conversion(elem)
	if err != nil {
		return err
	}
	if err := p.set(&Decoder{interp: i, registry: r}, val, rv.UnsafePointer()); err != nil {
		return fmt.Errorf("unmarshal %T into %s: %w", val, elem, err)
	}
	return nil
}

// conversion returns t's compiled form, building and caching it on first use.
func (r *Registry) conversion(t reflect.Type) (*conversion, error) {
	if p, ok := r.conversions.Load(t); ok {
		return p.(*conversion), nil
	}
	p, err := r.compile(t, make(map[reflect.Type]*conversion))
	if err != nil {
		return nil, err
	}
	actual, _ := r.conversions.LoadOrStore(t, p)
	return actual.(*conversion), nil
}

// compile resolves t by the first rule that matches: a registered entry, a type
// that converts itself, a VM runtime type, then the structural mapping. seen
// holds the compiled form of every type on the current path, so a recursive
// type reaches its own instead of compiling forever.
func (r *Registry) compile(t reflect.Type, seen map[reflect.Type]*conversion) (*conversion, error) {
	if p, ok := seen[t]; ok {
		return p, nil
	}
	if p, ok := r.conversions.Load(t); ok {
		return p.(*conversion), nil
	}

	p := &conversion{typ: t, vm: types.TypeAny}
	seen[t] = p

	switch {
	case r.registered(p), r.converting(p), r.native(p):
	default:
		if err := r.structure(p, seen); err != nil {
			delete(seen, t)
			return nil, err
		}
	}
	r.complete(p)
	return p, nil
}

// registered copies a construction-time registration into p.
func (r *Registry) registered(p *conversion) bool {
	e, ok := r.entries[p.typ]
	if !ok {
		return false
	}
	p.vm, p.value, p.set = e.vm, e.value, e.set
	if p.vm == nil {
		p.vm = types.TypeAny
	}
	return true
}

// converting resolves a type that implements the conversion on itself. The
// pointer method set is used in both directions, so a value-receiver
// MarshalVM is reached without copying the value out of its slot.
func (r *Registry) converting(p *conversion) bool {
	ptr := reflect.PointerTo(p.typ)
	marshals := ptr.Implements(typeValueMarshaler)
	unmarshals := ptr.Implements(typeValueUnmarshaler)
	if !marshals && !unmarshals {
		return false
	}
	if marshals {
		p.value = marshalConverting(p.typ)
	}
	if unmarshals {
		p.set = unmarshalConverting(p.typ)
	}
	return true
}

// native resolves a Go type that already holds a VM value.
func (r *Registry) native(p *conversion) bool {
	vm, ok := natives[p.typ]
	if !ok {
		if !p.typ.Implements(typeValue) {
			return false
		}
		vm = types.TypeAny
	}
	p.vm = vm
	p.value = marshalNative(p.typ)
	p.set = unmarshalValue(p.typ)
	return true
}

// structure compiles the reflection mapping for t's kind.
func (r *Registry) structure(p *conversion, seen map[reflect.Type]*conversion) error {
	t := p.typ
	switch t.Kind() {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.String:
		l := leaves[t.Kind()]
		p.kind = t.Kind()
		p.vm, p.value, p.box, p.set = l.vm, l.value, l.box, l.set
		return nil

	case reflect.Interface:
		p.vm = types.TypeAny
		p.value, p.box = marshalDynamic(t)
		p.set = unmarshalValue(t)
		return nil

	case reflect.Pointer:
		elem, err := r.compile(t.Elem(), seen)
		if err != nil {
			return err
		}
		p.vm = elem.vm
		p.value, p.box = marshalPointer(elem)
		p.set = unmarshalPointer(elem)
		return nil

	case reflect.Func:
		fn, err := r.function(t, 0, seen)
		if err != nil {
			return err
		}
		p.vm = fn
		p.value = marshalFunc(t, fn)
		p.set = unmarshalFunc(t, fn)
		return nil

	case reflect.Array, reflect.Slice:
		elem, err := r.compile(t.Elem(), seen)
		if err != nil {
			return err
		}
		p.vm = types.NewArrayType(elem.vm)
		p.value = marshalArray(t, p.vm.(*types.ArrayType), elem)
		p.set = unmarshalArray(t, elem)
		return nil

	case reflect.Map:
		key, err := r.compile(t.Key(), seen)
		if err != nil {
			return fmt.Errorf("map key type: %w", err)
		}
		elem, err := r.compile(t.Elem(), seen)
		if err != nil {
			return fmt.Errorf("map value type: %w", err)
		}
		mt := types.NewMapType(key.vm, elem.vm)
		p.vm = mt
		p.value = marshalMap(t, mt, key, elem)
		p.set = unmarshalMap(t, key, elem)
		return nil

	case reflect.Struct:
		vm, fields, methods, err := r.layout(t, seen)
		if err != nil {
			return err
		}
		p.vm = vm
		p.value = marshalStruct(t, vm, fields, methods)
		p.set = unmarshalStruct(fields)
		return nil
	}
	return fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, t)
}

// layout compiles a Go struct into a VM struct type: exported data fields in
// declaration order, then the exported methods of the pointer receiver. A field
// whose type has no VM representation is skipped, and a method name already
// taken by a field is dropped.
func (r *Registry) layout(t reflect.Type, seen map[reflect.Type]*conversion) (*types.StructType, []field, []method, error) {
	var slots []types.StructField
	var fields []field
	names := make(map[string]bool)

	for idx := range t.NumField() {
		f := t.Field(idx)
		if f.PkgPath != "" {
			continue
		}
		child, err := r.compile(f.Type, seen)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("struct field %s: %w", f.Name, err)
		}
		slots = append(slots, types.NewStructField(child.vm, types.FieldWithName(f.Name)))
		fields = append(fields, field{name: f.Name, offset: f.Offset, conversion: child})
		names[f.Name] = true
	}

	methods, err := r.methods(t, names, seen)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, m := range methods {
		slots = append(slots, types.NewStructField(m.typ, types.FieldWithName(m.name)))
	}
	return types.NewStructType(slots...), fields, methods, nil
}

// methods compiles the exported methods of *t, skipping any name already used
// and any signature with no VM mapping.
func (r *Registry) methods(t reflect.Type, names map[string]bool, seen map[reflect.Type]*conversion) ([]method, error) {
	ptr := reflect.PointerTo(t)
	var out []method
	for idx := range ptr.NumMethod() {
		m := ptr.Method(idx)
		if !m.IsExported() || names[m.Name] {
			continue
		}
		fn, err := r.function(m.Func.Type(), 1, seen)
		if err != nil {
			return nil, fmt.Errorf("method %s: %w", m.Name, err)
		}
		out = append(out, method{name: m.Name, index: idx, typ: fn})
		names[m.Name] = true
	}
	return out, nil
}

// function compiles a Go function signature into a VM function type, skipping
// the first skip parameters, an exact leading context.Context, and a trailing
// error return, all of which stay on the host side.
func (r *Registry) function(t reflect.Type, skip int, seen map[reflect.Type]*conversion) (*types.FunctionType, error) {
	if skip < t.NumIn() && t.In(skip) == typeContext {
		skip++
	}
	params := make([]types.Type, t.NumIn()-skip)
	for idx := range params {
		p, err := r.compile(t.In(idx+skip), seen)
		if err != nil {
			return nil, fmt.Errorf("function param %d: %w", idx, err)
		}
		params[idx] = p.vm
	}
	outs := t.NumOut()
	if outs > 0 && t.Out(outs-1).Implements(typeError) {
		outs--
	}
	returns := make([]types.Type, outs)
	for idx := range returns {
		p, err := r.compile(t.Out(idx), seen)
		if err != nil {
			return nil, fmt.Errorf("function return %d: %w", idx, err)
		}
		returns[idx] = p.vm
	}
	return &types.FunctionType{Params: params, Returns: returns}, nil
}

// complete fills the directions a resolution rule left open, so every compiled
// form is callable and an unsupported direction reports itself instead of
// panicking.
func (r *Registry) complete(p *conversion) {
	if p.value == nil {
		p.value = func(*Encoder, unsafe.Pointer) (types.Value, error) {
			return nil, fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, p.typ)
		}
	}
	if p.box == nil {
		p.box = boxing(p)
	}
	if p.set == nil {
		p.set = func(*Decoder, types.Value, unsafe.Pointer) error {
			return fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, p.typ)
		}
	}
}

// resolve follows a boxed value to the value it stands for, so every compiled
// conversion sees a standalone VM value however the source stored it.
func (r *Registry) resolve(i *Interpreter, val types.Value) (types.Value, error) {
	boxed, ok := val.(types.Boxed)
	if !ok {
		return val, nil
	}
	if boxed.Kind() != types.KindRef {
		return types.Unbox(boxed), nil
	}
	out, err := i.Load(boxed.Ref())
	if err != nil {
		return nil, fmt.Errorf("load ref %d: %w", boxed.Ref(), err)
	}
	return out, nil
}

// leaves maps each primitive Go kind to its VM type and the three conversions
// a conversion needs. Selecting an entry once at compile time is what keeps field
// access free of a per-read kind switch.
var leaves = [...]leaf{
	reflect.Bool: {
		vm:    types.TypeI1,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.I1(*(*bool)(p)), nil },
		box:   func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) { return types.BoxI1(*(*bool)(p)), nil },
		set:   setBool,
	},
	reflect.Int8: {
		vm:    types.TypeI8,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.I32(*(*int8)(p)), nil },
		box:   func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) { return types.BoxI8(*(*int8)(p)), nil },
		set:   setSigned[int8](),
	},
	reflect.Int16: {
		vm:    types.TypeI32,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.I32(*(*int16)(p)), nil },
		box: func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return types.BoxI32(int32(*(*int16)(p))), nil
		},
		set: setSigned[int16](),
	},
	reflect.Int32: {
		vm:    types.TypeI32,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.I32(*(*int32)(p)), nil },
		box:   func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) { return types.BoxI32(*(*int32)(p)), nil },
		set:   setSigned[int32](),
	},
	reflect.Int: {
		vm:    types.TypeI64,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.I64(*(*int)(p)), nil },
		box: func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return e.boxI64(int64(*(*int)(p)))
		},
		set: setSigned[int](),
	},
	reflect.Int64: {
		vm:    types.TypeI64,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.I64(*(*int64)(p)), nil },
		box:   func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) { return e.boxI64(*(*int64)(p)) },
		set:   setSigned[int64](),
	},
	reflect.Uint8: {
		vm:    types.TypeI32,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.I32(*(*uint8)(p)), nil },
		box: func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return types.BoxI32(int32(*(*uint8)(p))), nil
		},
		set: setUnsigned[uint8](),
	},
	reflect.Uint16: {
		vm:    types.TypeI32,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.I32(*(*uint16)(p)), nil },
		box: func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return types.BoxI32(int32(*(*uint16)(p))), nil
		},
		set: setUnsigned[uint16](),
	},
	reflect.Uint32: {
		vm: types.TypeI32,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) {
			return types.I32(int32(*(*uint32)(p))), nil
		},
		box: func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return types.BoxI32(int32(*(*uint32)(p))), nil
		},
		set: setUnsigned[uint32](),
	},
	reflect.Uint: {
		vm: types.TypeI64,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) {
			return types.I64(int64(*(*uint)(p))), nil
		},
		box: func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return e.boxI64(int64(*(*uint)(p)))
		},
		set: setUnsigned[uint](),
	},
	reflect.Uint64: {
		vm: types.TypeI64,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) {
			return types.I64(int64(*(*uint64)(p))), nil
		},
		box: func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return e.boxI64(int64(*(*uint64)(p)))
		},
		set: setUnsigned[uint64](),
	},
	reflect.Uintptr: {
		vm: types.TypeI64,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) {
			return types.I64(int64(*(*uintptr)(p))), nil
		},
		box: func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return e.boxI64(int64(*(*uintptr)(p)))
		},
		set: setUnsigned[uintptr](),
	},
	reflect.Float32: {
		vm:    types.TypeF32,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.F32(*(*float32)(p)), nil },
		box:   func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) { return types.BoxF32(*(*float32)(p)), nil },
		set:   setFloat[float32](),
	},
	reflect.Float64: {
		vm:    types.TypeF64,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.F64(*(*float64)(p)), nil },
		box:   func(_ *Encoder, p unsafe.Pointer) (types.Boxed, error) { return types.BoxF64(*(*float64)(p)), nil },
		set:   setFloat[float64](),
	},
	reflect.String: {
		vm:    types.TypeString,
		value: func(_ *Encoder, p unsafe.Pointer) (types.Value, error) { return types.String(*(*string)(p)), nil },
		box: func(e *Encoder, p unsafe.Pointer) (types.Boxed, error) {
			return e.alloc(types.String(*(*string)(p)))
		},
		set: setString,
	},
}

// asInt reads val as a VM integer.
func asInt(val types.Value) (int64, bool) {
	kind, bits, ok := bitsOf(val)
	if !ok {
		return 0, false
	}
	switch kind {
	case types.KindI32:
		return int64(int32(bits)), true
	case types.KindI64:
		return int64(bits), true
	default:
		return 0, false
	}
}

// asUint reads val as a VM integer, preserving the raw bits an unsigned Go
// value was stored as.
func asUint(val types.Value) (uint64, bool) {
	kind, bits, ok := bitsOf(val)
	if !ok {
		return 0, false
	}
	switch kind {
	case types.KindI32:
		return uint64(uint32(bits)), true
	case types.KindI64:
		return bits, true
	default:
		return 0, false
	}
}

// asFloat reads val as a VM float.
func asFloat(val types.Value) (float64, bool) {
	kind, bits, ok := bitsOf(val)
	if !ok {
		return 0, false
	}
	switch kind {
	case types.KindF32:
		return float64(math.Float32frombits(uint32(bits))), true
	case types.KindF64:
		return math.Float64frombits(bits), true
	default:
		return 0, false
	}
}

// bitsOf reduces a scalar VM value to the kind and raw bits it carries.
func bitsOf(val types.Value) (types.Kind, uint64, bool) {
	switch v := val.(type) {
	case types.I1:
		if v {
			return types.KindI32, 1, true
		}
		return types.KindI32, 0, true
	case types.I8:
		return types.KindI32, uint64(uint32(int32(v))), true
	case types.I32:
		return types.KindI32, uint64(uint32(v)), true
	case types.I64:
		return types.KindI64, uint64(v), true
	case types.F32:
		return types.KindF32, uint64(math.Float32bits(float32(v))), true
	case types.F64:
		return types.KindF64, math.Float64bits(float64(v)), true
	case types.Boxed:
		switch v.Kind() {
		case types.KindI32, types.KindI8, types.KindI1:
			return types.KindI32, uint64(uint32(v.I32())), true
		case types.KindI64:
			return types.KindI64, uint64(v.I64()), true
		case types.KindF32:
			return types.KindF32, uint64(math.Float32bits(v.F32())), true
		case types.KindF64:
			return types.KindF64, math.Float64bits(v.F64()), true
		}
	}
	return 0, 0, false
}

var (
	complexF32 = types.NewStructType(
		types.NewStructField(types.TypeF32, types.FieldWithName("Real")),
		types.NewStructField(types.TypeF32, types.FieldWithName("Imag")),
	)
	complexF64 = types.NewStructType(
		types.NewStructField(types.TypeF64, types.FieldWithName("Real")),
		types.NewStructField(types.TypeF64, types.FieldWithName("Imag")),
	)
)

// defaults registers the standard-library types that have no direct VM kind.
// They take the same path as a user registration, so naming one of them in
// NewRegistry replaces it rather than layering on it.
func defaults() []func(*registry) {
	return []func(*registry){
		WithMarshaler(reflect.TypeFor[time.Time](), types.TypeI64, MarshalerFunc(
			func(_ *Encoder, p unsafe.Pointer) (types.Value, error) {
				return types.I64((*(*time.Time)(p)).UnixNano()), nil
			})),
		WithUnmarshaler(reflect.TypeFor[time.Time](), UnmarshalerFunc(
			func(_ *Decoder, val types.Value, p unsafe.Pointer) error {
				n, ok := asInt(val)
				if !ok {
					return fmt.Errorf("%w: source=%T target=time.Time", ErrTypeMismatch, val)
				}
				*(*time.Time)(p) = time.Unix(0, n)
				return nil
			})),
		WithMarshaler(reflect.TypeFor[complex64](), complexF32, MarshalerFunc(
			func(_ *Encoder, p unsafe.Pointer) (types.Value, error) {
				c := *(*complex64)(p)
				return types.NewStruct(complexF32, types.BoxF32(real(c)), types.BoxF32(imag(c))), nil
			})),
		WithUnmarshaler(reflect.TypeFor[complex64](), UnmarshalerFunc(
			func(d *Decoder, val types.Value, p unsafe.Pointer) error {
				c, err := d.complex(val)
				if err != nil {
					return err
				}
				if math.IsInf(float64(float32(real(c))), 0) || math.IsInf(float64(float32(imag(c))), 0) {
					return fmt.Errorf("%w: %v overflows complex64", ErrValueOverflow, c)
				}
				*(*complex64)(p) = complex64(c)
				return nil
			})),
		WithMarshaler(reflect.TypeFor[complex128](), complexF64, MarshalerFunc(
			func(_ *Encoder, p unsafe.Pointer) (types.Value, error) {
				c := *(*complex128)(p)
				return types.NewStruct(complexF64, types.BoxF64(real(c)), types.BoxF64(imag(c))), nil
			})),
		WithUnmarshaler(reflect.TypeFor[complex128](), UnmarshalerFunc(
			func(d *Decoder, val types.Value, p unsafe.Pointer) error {
				c, err := d.complex(val)
				if err != nil {
					return err
				}
				*(*complex128)(p) = c
				return nil
			})),
	}
}

// complex reads the {Real, Imag} struct both complex registrations produce.
func (d *Decoder) complex(val types.Value) (complex128, error) {
	value, err := d.registry.resolve(d.interp, val)
	if err != nil {
		return 0, err
	}
	st, ok := value.(*types.Struct)
	if !ok {
		return 0, fmt.Errorf("%w: source=%T", ErrTypeMismatch, value)
	}
	re, reOK := asFloat(st.FieldByName("Real"))
	im, imOK := asFloat(st.FieldByName("Imag"))
	if !reOK || !imOK {
		return 0, fmt.Errorf("%w: source=%s", ErrTypeMismatch, st.Typ)
	}
	return complex(re, im), nil
}
