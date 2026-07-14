package interp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

type Marshaler interface {
	Marshal(*Interpreter, any) (types.Value, error)
	Unmarshal(*Interpreter, types.Value, any) error
}

// ValueMarshaler lets a Go type define its own VM representation. A type that
// implements it is converted by its MarshalVM method instead of by reflection,
// taking precedence over struct and host-object routing. Implement
// ValueUnmarshaler as well for a full round-trip.
type ValueMarshaler interface {
	MarshalVM(*Interpreter) (types.Value, error)
}

// ValueUnmarshaler lets a Go type decode itself from a VM value. It is
// implemented on the pointer receiver so UnmarshalVM can mutate the
// destination.
type ValueUnmarshaler interface {
	UnmarshalVM(*Interpreter, types.Value) error
}

// Converter registers VM conversion for a Go type that cannot implement
// ValueMarshaler / ValueUnmarshaler (an external type). Register it with
// WithConverter; the default marshaler then applies it wherever that type
// appears, including nested in structs, slices, and maps. Marshal or Unmarshal
// may be nil to leave that direction unsupported. Unmarshal receives a pointer
// to the destination.
type Converter struct {
	VMType    types.Type
	Marshal   func(*Interpreter, any) (types.Value, error)
	Unmarshal func(*Interpreter, types.Value, any) error
}

type codec struct {
	plans      sync.Map
	converters map[reflect.Type]Converter
}

type marshalPlan struct {
	VMType    types.Type
	Type      reflect.Type
	marshal   marshaler
	unmarshal unmarshaler
}

type marshaler func(*marshalState, reflect.Value) (types.Value, error)

type unmarshaler func(*unmarshalState, types.Value, reflect.Value) error

type fieldPlan struct {
	Index  int
	Offset uintptr
	Kind   reflect.Kind
	Plan   *marshalPlan
}

type marshalState struct {
	m    *codec
	i    *Interpreter
	seen map[uintptr]bool
}

type unmarshalState struct {
	m *codec
	i *Interpreter
}

var (
	ErrMarshalCycle           = errors.New("marshal cycle")
	ErrUnsupportedMarshalType = errors.New("unsupported marshal type")
	ErrInvalidUnmarshalTarget = errors.New("invalid unmarshal target")
	ErrValueOverflow          = errors.New("value overflow")
)

var DefaultMarshaler Marshaler = &codec{}

var (
	_ Marshaler = (*codec)(nil)

	typeError   = reflect.TypeOf((*error)(nil)).Elem()
	typeContext = reflect.TypeOf((*context.Context)(nil)).Elem()
	typeValue   = reflect.TypeOf((*types.Value)(nil)).Elem()

	runtimeTypes = map[reflect.Type]types.Type{
		reflect.TypeOf(types.I32(0)):     types.TypeI32,
		reflect.TypeOf(types.I64(0)):     types.TypeI64,
		reflect.TypeOf(types.F32(0)):     types.TypeF32,
		reflect.TypeOf(types.F64(0)):     types.TypeF64,
		reflect.TypeOf(types.Ref(0)):     types.TypeRef,
		reflect.TypeOf(types.Boxed(0)):   types.TypeRef,
		reflect.TypeOf(types.String("")): types.TypeString,
	}

	valueMarshalerType   = reflect.TypeOf((*ValueMarshaler)(nil)).Elem()
	valueUnmarshalerType = reflect.TypeOf((*ValueUnmarshaler)(nil)).Elem()

	timeType       = reflect.TypeOf(time.Time{})
	complex64Type  = reflect.TypeOf(complex64(0))
	complex128Type = reflect.TypeOf(complex128(0))

	complex64VMType = types.NewStructType(
		types.NewStructField(types.TypeF32, types.FieldWithName("Real")),
		types.NewStructField(types.TypeF32, types.FieldWithName("Imag")),
	)
	complex128VMType = types.NewStructType(
		types.NewStructField(types.TypeF64, types.FieldWithName("Real")),
		types.NewStructField(types.TypeF64, types.FieldWithName("Imag")),
	)

	// builtinConverters are pre-registered Converters for stdlib types that have
	// no direct VM kind. They flow through the same path as WithConverter, and a
	// user registration for the same type overrides them. time.Time maps to I64
	// (UnixNano); complex maps to a {Real, Imag} struct.
	builtinConverters = map[reflect.Type]Converter{
		timeType: {
			VMType: types.TypeI64,
			Marshal: func(_ *Interpreter, v any) (types.Value, error) {
				return types.I64(v.(time.Time).UnixNano()), nil
			},
			Unmarshal: func(_ *Interpreter, val types.Value, dst any) error {
				n, ok := asInt(val)
				if !ok {
					return fmt.Errorf("%w: source=%T target=time.Time", ErrTypeMismatch, val)
				}
				*dst.(*time.Time) = time.Unix(0, n)
				return nil
			},
		},
		complex64Type: {
			VMType: complex64VMType,
			Marshal: func(_ *Interpreter, v any) (types.Value, error) {
				c := v.(complex64)
				return types.NewStruct(complex64VMType, types.BoxF32(real(c)), types.BoxF32(imag(c))), nil
			},
			Unmarshal: unmarshalComplex,
		},
		complex128Type: {
			VMType: complex128VMType,
			Marshal: func(_ *Interpreter, v any) (types.Value, error) {
				c := v.(complex128)
				return types.NewStruct(complex128VMType, types.BoxF64(real(c)), types.BoxF64(imag(c))), nil
			},
			Unmarshal: unmarshalComplex,
		},
	}
)

func (m *codec) Marshal(i *Interpreter, v any) (types.Value, error) {
	state := &marshalState{m: m, i: i, seen: make(map[uintptr]bool)}
	return state.value(reflect.ValueOf(v))
}

func (m *codec) Unmarshal(i *Interpreter, v types.Value, dst any) error {
	state := &unmarshalState{m: m, i: i}
	out := reflect.ValueOf(dst)
	if !out.IsValid() || out.Kind() != reflect.Pointer || out.IsNil() {
		return fmt.Errorf("%w: destination must be non-nil pointer", ErrInvalidUnmarshalTarget)
	}
	out = out.Elem()
	if err := state.value(v, out); err != nil {
		return fmt.Errorf("unmarshal %T into %s: %w", v, out.Type(), err)
	}
	return nil
}

func (m *codec) plan(t reflect.Type) (*marshalPlan, error) {
	if p, ok := m.plans.Load(t); ok {
		return p.(*marshalPlan), nil
	}
	p, err := m.compile(t, make(map[reflect.Type]bool))
	if err != nil {
		return nil, err
	}
	actual, _ := m.plans.LoadOrStore(t, p)
	return actual.(*marshalPlan), nil
}

func (m *codec) compile(t reflect.Type, seen map[reflect.Type]bool) (*marshalPlan, error) {
	if vmType, ok := runtimeTypes[t]; ok {
		return &marshalPlan{VMType: vmType, Type: t, marshal: (*marshalState).marshalRuntime, unmarshal: (*unmarshalState).unmarshalInterface}, nil
	}
	if seen[t] {
		return &marshalPlan{VMType: types.TypeRef, Type: t, marshal: (*marshalState).marshalCycle, unmarshal: (*unmarshalState).unmarshalCycle}, nil
	}
	seen[t] = true
	defer delete(seen, t)

	if p, ok := m.customPlan(t); ok {
		return p, nil
	}
	if p, ok := m.converterPlan(t); ok {
		return p, nil
	}

	if t.Kind() == reflect.Pointer {
		elem := t.Elem()
		if _, ok := runtimeTypes[elem]; !ok &&
			!elem.Implements(typeValue) &&
			!reflect.PointerTo(elem).Implements(typeValue) &&
			elem.Kind() != reflect.Interface &&
			m.typeOf(elem.Kind()) != nil &&
			t.NumMethod() > 0 {
			elemPlan, err := m.compile(elem, seen)
			if err != nil {
				return nil, err
			}
			vmStruct, slots, _, err := m.compileHostObject(elem, seen)
			if err != nil {
				return nil, err
			}
			return &marshalPlan{
				VMType:    vmStruct,
				Type:      t,
				marshal:   m.marshalHostPointer(slots, vmStruct),
				unmarshal: m.unmarshalPointer(elemPlan),
			}, nil
		}
		if seen[elem] {
			return &marshalPlan{VMType: types.TypeRef, Type: t, marshal: (*marshalState).marshalRuntime, unmarshal: (*unmarshalState).unmarshalInterface}, nil
		}
		elemPlan, err := m.compile(elem, seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    elemPlan.VMType,
			Type:      t,
			marshal:   m.marshalPointer(elemPlan),
			unmarshal: m.unmarshalPointer(elemPlan),
		}, nil
	}
	if t.Kind() == reflect.Struct {
		hostable := false
		if _, ok := runtimeTypes[t]; !ok &&
			!t.Implements(typeValue) &&
			!reflect.PointerTo(t).Implements(typeValue) {
			for idx := 0; idx < t.NumField(); idx++ {
				if t.Field(idx).PkgPath != "" {
					hostable = true
					break
				}
			}
			if !hostable {
				hostable = reflect.PointerTo(t).NumMethod() > 0
			}
		}
		if hostable {
			vmStruct, slots, data, err := m.compileHostObject(t, seen)
			if err != nil {
				return nil, err
			}
			return &marshalPlan{
				VMType:    vmStruct,
				Type:      t,
				marshal:   m.marshalHost(slots, vmStruct),
				unmarshal: m.unmarshalStruct(data),
			}, nil
		}
	}
	if t.Implements(typeValue) {
		return &marshalPlan{VMType: types.TypeRef, Type: t, marshal: (*marshalState).marshalRuntime, unmarshal: (*unmarshalState).unmarshalInterface}, nil
	}
	switch t.Kind() {
	case reflect.Bool:
		return &marshalPlan{VMType: types.TypeI1, Type: t, marshal: (*marshalState).marshalBool, unmarshal: (*unmarshalState).unmarshalBool}, nil
	case reflect.Int8:
		return &marshalPlan{VMType: types.TypeI8, Type: t, marshal: (*marshalState).marshalI32, unmarshal: (*unmarshalState).unmarshalInt}, nil
	case reflect.Int16, reflect.Int32:
		return &marshalPlan{VMType: types.TypeI32, Type: t, marshal: (*marshalState).marshalI32, unmarshal: (*unmarshalState).unmarshalInt}, nil
	case reflect.Int, reflect.Int64:
		return &marshalPlan{VMType: types.TypeI64, Type: t, marshal: (*marshalState).marshalI64, unmarshal: (*unmarshalState).unmarshalInt}, nil
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &marshalPlan{VMType: types.TypeI32, Type: t, marshal: (*marshalState).marshalU32, unmarshal: (*unmarshalState).unmarshalUint}, nil
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return &marshalPlan{VMType: types.TypeI64, Type: t, marshal: (*marshalState).marshalU64, unmarshal: (*unmarshalState).unmarshalUint}, nil
	case reflect.Float32:
		return &marshalPlan{VMType: types.TypeF32, Type: t, marshal: (*marshalState).marshalF32, unmarshal: (*unmarshalState).unmarshalFloat}, nil
	case reflect.Float64:
		return &marshalPlan{VMType: types.TypeF64, Type: t, marshal: (*marshalState).marshalF64, unmarshal: (*unmarshalState).unmarshalFloat}, nil
	case reflect.String:
		return &marshalPlan{VMType: types.TypeString, Type: t, marshal: (*marshalState).marshalString, unmarshal: (*unmarshalState).unmarshalString}, nil
	case reflect.Interface:
		return &marshalPlan{VMType: types.TypeRef, Type: t, marshal: (*marshalState).marshalAny, unmarshal: (*unmarshalState).unmarshalInterface}, nil
	case reflect.Func:
		fnType, err := m.compileFunctionType(t, 0, seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    fnType,
			Type:      t,
			marshal:   m.marshalFunc(fnType),
			unmarshal: m.unmarshalFunc(fnType),
		}, nil
	case reflect.Array:
		elemPlan, err := m.compile(t.Elem(), seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    types.NewArrayType(elemPlan.VMType),
			Type:      t,
			marshal:   m.marshalArray(elemPlan),
			unmarshal: m.unmarshalArray(elemPlan),
		}, nil
	case reflect.Slice:
		elemPlan, err := m.compile(t.Elem(), seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    types.NewArrayType(elemPlan.VMType),
			Type:      t,
			marshal:   m.marshalArray(elemPlan),
			unmarshal: m.unmarshalSlice(elemPlan),
		}, nil
	case reflect.Map:
		keyPlan, err := m.compile(t.Key(), seen)
		if err != nil {
			return nil, fmt.Errorf("map key type: %w", err)
		}
		valPlan, err := m.compile(t.Elem(), seen)
		if err != nil {
			return nil, fmt.Errorf("map value type: %w", err)
		}
		mt := types.NewMapType(keyPlan.VMType, valPlan.VMType)
		return &marshalPlan{
			VMType:    mt,
			Type:      t,
			marshal:   m.marshalMap(mt),
			unmarshal: m.unmarshalMap(keyPlan, valPlan),
		}, nil
	case reflect.Struct:
		vmStruct, fields, err := m.compileStructType(t, seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    vmStruct,
			Type:      t,
			marshal:   m.marshalStruct(fields, vmStruct),
			unmarshal: m.unmarshalStruct(fields),
		}, nil
	}
	return nil, fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, t)
}

func (m *codec) compileStructType(t reflect.Type, seen map[reflect.Type]bool) (*types.StructType, []fieldPlan, error) {
	fields := make([]types.StructField, 0, t.NumField())
	plans := make([]fieldPlan, 0, t.NumField())
	for idx := 0; idx < t.NumField(); idx++ {
		field := t.Field(idx)
		if field.PkgPath != "" {
			continue
		}
		p, err := m.compile(field.Type, seen)
		if err != nil {
			return nil, nil, fmt.Errorf("struct field %s: %w", field.Name, err)
		}
		fields = append(fields, types.NewStructField(p.VMType, types.FieldWithName(field.Name)))
		plans = append(plans, fieldPlan{
			Index:  idx,
			Offset: field.Offset,
			Kind:   field.Type.Kind(),
			Plan:   p,
		})
	}
	return types.NewStructType(fields...), plans, nil
}

func (m *codec) compileHostObject(t reflect.Type, seen map[reflect.Type]bool) (*types.StructType, []hostSlot, []fieldPlan, error) {
	var fields []types.StructField
	var slots []hostSlot
	var data []fieldPlan
	names := make(map[string]bool)
	if t.Kind() == reflect.Struct {
		for idx := 0; idx < t.NumField(); idx++ {
			f := t.Field(idx)
			if f.PkgPath != "" {
				continue
			}
			kind := f.Type.Kind()
			if kind == reflect.Interface && !f.Type.Implements(typeValue) {
				continue
			}
			typ := m.typeOf(kind)
			if typ == nil {
				continue
			}
			p, err := m.compile(f.Type, seen)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("host field %s: %w", f.Name, err)
			}
			fields = append(fields, types.NewStructField(typ, types.FieldWithName(f.Name)))
			slots = append(slots, hostSlot{field: idx, offset: f.Offset, kind: kind})
			data = append(data, fieldPlan{Index: idx, Offset: f.Offset, Kind: kind, Plan: p})
			names[f.Name] = true
		}
	} else if t.Kind() != reflect.Interface {
		typ := m.typeOf(t.Kind())
		if typ == nil {
			return nil, nil, nil, fmt.Errorf("%w: host value type=%s", ErrUnsupportedMarshalType, t)
		}
		fields = append(fields, types.NewStructField(typ, types.FieldWithName("Value")))
		slots = append(slots, hostSlot{field: 0, kind: t.Kind()})
		names["Value"] = true
	}
	methodType := reflect.PointerTo(t)
	for idx := 0; idx < methodType.NumMethod(); idx++ {
		method := methodType.Method(idx)
		if !method.IsExported() || names[method.Name] {
			continue
		}
		fnType, err := m.compileFunctionType(method.Func.Type(), 1, seen)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("method %s: %w", method.Name, err)
		}
		fields = append(fields, types.NewStructField(fnType, types.FieldWithName(method.Name)))
		slots = append(slots, hostSlot{field: -1, method: idx, fnType: fnType})
		names[method.Name] = true
	}
	return types.NewStructType(fields...), slots, data, nil
}

func (m *codec) compileFunctionType(t reflect.Type, skip int, seen map[reflect.Type]bool) (*types.FunctionType, error) {
	if skip < t.NumIn() && t.In(skip) == typeContext {
		skip++
	}
	params := make([]types.Type, t.NumIn()-skip)
	for idx := range params {
		p, err := m.compile(t.In(idx+skip), seen)
		if err != nil {
			return nil, fmt.Errorf("function param %d: %w", idx, err)
		}
		params[idx] = p.VMType
	}
	outs := t.NumOut()
	if outs > 0 && t.Out(outs-1).Implements(typeError) {
		outs--
	}
	returns := make([]types.Type, outs)
	for idx := range returns {
		p, err := m.compile(t.Out(idx), seen)
		if err != nil {
			return nil, fmt.Errorf("function return %d: %w", idx, err)
		}
		returns[idx] = p.VMType
	}
	return &types.FunctionType{Params: params, Returns: returns}, nil
}

// customPlan returns a plan for a Go type that opts into its own conversion via
// ValueMarshaler / ValueUnmarshaler. A direction the type does not implement
// surfaces ErrUnsupportedMarshalType, so round-trip use should implement both.
func (m *codec) customPlan(t reflect.Type) (*marshalPlan, bool) {
	ptr := reflect.PointerTo(t)
	marshalable := t.Implements(valueMarshalerType) || ptr.Implements(valueMarshalerType)
	unmarshalable := t.Implements(valueUnmarshalerType) || ptr.Implements(valueUnmarshalerType)
	if !marshalable && !unmarshalable {
		return nil, false
	}
	plan := &marshalPlan{VMType: types.TypeRef, Type: t, marshal: (*marshalState).marshalUnsupported, unmarshal: (*unmarshalState).unmarshalUnsupported}
	if marshalable {
		plan.marshal = m.marshalCustom(t)
	}
	if unmarshalable {
		plan.unmarshal = (*unmarshalState).unmarshalCustom
	}
	return plan, true
}

// converterPlan returns a plan for a type handled by a Converter, whether
// registered via WithConverter or built in (time.Time, complex). A user
// registration overrides the built-in for the same type. A nil direction stays
// unsupported.
func (m *codec) converterPlan(t reflect.Type) (*marshalPlan, bool) {
	c, ok := m.converters[t]
	if !ok {
		c, ok = builtinConverters[t]
	}
	if !ok {
		return nil, false
	}
	vmType := c.VMType
	if vmType == nil {
		vmType = types.TypeRef
	}
	plan := &marshalPlan{VMType: vmType, Type: t, marshal: (*marshalState).marshalUnsupported, unmarshal: (*unmarshalState).unmarshalUnsupported}
	if c.Marshal != nil {
		plan.marshal = m.marshalConverter(c)
	}
	if c.Unmarshal != nil {
		plan.unmarshal = m.unmarshalConverter(c)
	}
	return plan, true
}

func (m *codec) marshalCustom(t reflect.Type) marshaler {
	value := t.Implements(valueMarshalerType)
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		if !v.CanAddr() {
			p := reflect.New(v.Type())
			p.Elem().Set(v)
			v = p.Elem()
		}
		var vm ValueMarshaler
		if value {
			vm = v.Interface().(ValueMarshaler)
		} else {
			vm = v.Addr().Interface().(ValueMarshaler)
		}
		return vm.MarshalVM(s.i)
	}
}

func (m *codec) marshalConverter(c Converter) marshaler {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		if !v.CanInterface() {
			p := reflect.New(v.Type())
			p.Elem().Set(v)
			v = p.Elem()
		}
		return c.Marshal(s.i, v.Interface())
	}
}

func (m *codec) unmarshalConverter(c Converter) unmarshaler {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		return c.Unmarshal(s.i, val, dst.Addr().Interface())
	}
}

func (s *marshalState) hostObject(ptr reflect.Value, slots []hostSlot, vm *types.StructType) *HostObject {
	bound := make([]hostSlot, len(slots))
	copy(bound, slots)
	for idx := range bound {
		if !bound[idx].isMethod() {
			continue
		}
		fn := s.wrapFunc(ptr.Method(bound[idx].method), bound[idx].fnType)
		bound[idx].addr = s.alloc(fn)
	}
	return &HostObject{
		Typ:      vm,
		Receiver: ptr,
		data:     unsafe.Pointer(ptr.Pointer()),
		slots:    bound,
		interp:   s.i,
	}
}

func (s *marshalState) wrapFunc(fn reflect.Value, typ *types.FunctionType) *HostFunction {
	m := s.m
	fnType := fn.Type()
	hasContext := fnType.NumIn() > 0 && fnType.In(0) == typeContext
	// in and seen are allocated fresh inside the returned closure on every
	// call: the returned HostFunction is a types.Value that can be placed in
	// program.WithConstants, so pooled Interpreters built from the same
	// program share this exact *HostFunction (New keeps the constant's Go
	// value directly - see interp.go's BoxRef(i.keep(v)) path) and may call
	// it concurrently from separate goroutines. Call-scoped scratch would
	// race across such concurrent calls.
	return NewHostFunction(typ, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
		if len(params) != len(typ.Params) {
			return nil, fmt.Errorf("%w: got %d params, want %d", ErrTypeMismatch, len(params), len(typ.Params))
		}
		in := make([]reflect.Value, fnType.NumIn())
		offset := 0
		if hasContext {
			ctx := i.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			in[0] = reflect.ValueOf(ctx)
			offset = 1
		}
		unmarshal := &unmarshalState{m: m, i: i}
		for idx := range params {
			in[idx+offset] = reflect.New(fnType.In(idx + offset)).Elem()
			if err := unmarshal.value(params[idx], in[idx+offset]); err != nil {
				return nil, fmt.Errorf("function param %d: %w", idx, err)
			}
		}

		out := fn.Call(in)
		if len(out) > 0 && out[len(out)-1].Type().Implements(typeError) {
			err := out[len(out)-1]
			nilable := false
			switch err.Kind() {
			case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
				nilable = true
			}
			if !nilable || !err.IsNil() {
				return nil, err.Interface().(error)
			}
			out = out[:len(out)-1]
		}

		returns := make([]types.Boxed, len(out))
		marshal := &marshalState{m: m, i: i, seen: make(map[uintptr]bool)}
		for idx := range out {
			boxed, err := marshal.boxAs(out[idx], typ.Returns[idx])
			if err != nil {
				return nil, fmt.Errorf("function return %d: %w", idx, err)
			}
			returns[idx] = boxed
		}
		return returns, nil
	})
}

func (s *marshalState) boxFieldAt(base unsafe.Pointer, pf fieldPlan, typ types.Type) (types.Boxed, error) {
	fp := unsafe.Add(base, pf.Offset)
	if pf.Plan.Type.Implements(typeValue) {
		rv := reflect.NewAt(pf.Plan.Type, fp).Elem()
		return s.boxAs(rv, typ)
	}
	switch pf.Kind {
	case reflect.Bool:
		return s.boxed(types.Bool(*(*bool)(fp)), typ)
	case reflect.Int8:
		return s.boxed(types.I32(*(*int8)(fp)), typ)
	case reflect.Int16:
		return s.boxed(types.I32(*(*int16)(fp)), typ)
	case reflect.Int32:
		return s.boxed(types.I32(*(*int32)(fp)), typ)
	case reflect.Int:
		return s.boxed(types.I64(*(*int)(fp)), typ)
	case reflect.Int64:
		return s.boxed(types.I64(*(*int64)(fp)), typ)
	case reflect.Uint8:
		return s.boxed(types.I32(int32(*(*uint8)(fp))), typ)
	case reflect.Uint16:
		return s.boxed(types.I32(int32(*(*uint16)(fp))), typ)
	case reflect.Uint32:
		return s.boxed(types.I32(int32(*(*uint32)(fp))), typ)
	case reflect.Uint:
		return s.boxed(types.I64(int64(*(*uint)(fp))), typ)
	case reflect.Uint64:
		return s.boxed(types.I64(int64(*(*uint64)(fp))), typ)
	case reflect.Uintptr:
		return s.boxed(types.I64(int64(*(*uintptr)(fp))), typ)
	case reflect.Float32:
		return s.boxed(types.F32(*(*float32)(fp)), typ)
	case reflect.Float64:
		return s.boxed(types.F64(*(*float64)(fp)), typ)
	case reflect.String:
		return s.boxed(types.String(*(*string)(fp)), typ)
	default:
		rv := reflect.NewAt(pf.Plan.Type, fp).Elem()
		return s.boxAs(rv, typ)
	}
}

func (s *marshalState) boxAs(v reflect.Value, typ types.Type) (types.Boxed, error) {
	val, err := s.value(v)
	if err != nil {
		return 0, err
	}
	return s.boxed(val, typ)
}

func (s *marshalState) boxed(val types.Value, typ types.Type) (types.Boxed, error) {
	switch typ.Kind() {
	case types.KindI32:
		n, ok := asInt(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i32", ErrTypeMismatch)
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return 0, fmt.Errorf("%w: %d overflows i32", ErrValueOverflow, n)
		}
		return types.BoxI32(int32(n)), nil
	case types.KindI8:
		n, ok := asInt(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i8", ErrTypeMismatch)
		}
		return types.BoxI8(int8(n)), nil
	case types.KindI1:
		n, ok := asInt(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i1", ErrTypeMismatch)
		}
		return types.BoxI1(n != 0), nil
	case types.KindI64:
		n, ok := asInt(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i64", ErrTypeMismatch)
		}
		if types.IsBoxable(n) {
			return types.BoxI64(n), nil
		}
		return types.BoxRef(s.alloc(types.I64(n))), nil
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
		if typ.Equals(types.TypeString) {
			if _, ok := val.(types.String); !ok {
				return 0, fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, typ)
			}
			return s.boxRef(val), nil
		}
		if !typ.Equals(types.TypeRef) {
			valTyp := types.Type(nil)
			if val != nil {
				valTyp = val.Type()
			}
			if valTyp == nil || !valTyp.Equals(typ) {
				return 0, fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, typ)
			}
		}
		return s.boxRef(val), nil
	default:
		return 0, fmt.Errorf("%w: target=%s", ErrTypeMismatch, typ)
	}
}

func (s *marshalState) boxRef(val types.Value) types.Boxed {
	if val == nil || types.IsNull(val) {
		return types.BoxedNull
	}
	switch v := val.(type) {
	case types.Boxed:
		if v.Kind() == types.KindRef {
			return v
		}
		return types.BoxRef(s.alloc(types.Unbox(v)))
	case types.Ref:
		return types.BoxRef(int(v))
	case types.String:
		return types.BoxRef(int(s.i.intern(string(v))))
	default:
		return types.BoxRef(s.alloc(val))
	}
}

func (s *marshalState) alloc(val types.Value) int {
	addr, err := s.i.Alloc(val)
	if err != nil {
		panic(err)
	}
	return addr
}

func (s *unmarshalState) value(val types.Value, dst reflect.Value) error {
	if !dst.CanSet() {
		return fmt.Errorf("%w: destination is not settable", ErrInvalidUnmarshalTarget)
	}
	p, err := s.m.plan(dst.Type())
	if err != nil {
		return err
	}
	return p.unmarshal(s, val, dst)
}

func (s *unmarshalState) elems(value types.Value) ([]types.Value, error) {
	switch v := value.(type) {
	case types.TypedArray[bool]:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.I1(elem)
		}
		return out, nil
	case types.TypedArray[int8]:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.I32(elem)
		}
		return out, nil
	case types.TypedArray[int32]:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.I32(elem)
		}
		return out, nil
	case types.TypedArray[int64]:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.I64(elem)
		}
		return out, nil
	case types.TypedArray[float32]:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.F32(elem)
		}
		return out, nil
	case types.TypedArray[float64]:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.F64(elem)
		}
		return out, nil
	case *types.Array:
		out := make([]types.Value, len(v.Elems))
		for idx, elem := range v.Elems {
			val, err := s.m.resolve(s.i, elem)
			if err != nil {
				return nil, fmt.Errorf("array element %d: %w", idx, err)
			}
			out[idx] = val
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%w: source=%T", ErrTypeMismatch, value)
	}
}

func (s *marshalState) marshalBool(v reflect.Value) (types.Value, error) {
	return types.I1(v.Bool()), nil
}

func (s *marshalState) marshalI32(v reflect.Value) (types.Value, error) {
	return types.I32(v.Int()), nil
}

func (s *marshalState) marshalI64(v reflect.Value) (types.Value, error) {
	return types.I64(v.Int()), nil
}

func (s *marshalState) marshalU32(v reflect.Value) (types.Value, error) {
	return types.I32(int32(v.Uint())), nil
}

func (s *marshalState) marshalU64(v reflect.Value) (types.Value, error) {
	return types.I64(int64(v.Uint())), nil
}

func (s *marshalState) marshalF32(v reflect.Value) (types.Value, error) {
	return types.F32(float32(v.Float())), nil
}

func (s *marshalState) marshalF64(v reflect.Value) (types.Value, error) {
	return types.F64(v.Float()), nil
}

func (s *marshalState) marshalString(v reflect.Value) (types.Value, error) {
	return types.String(v.String()), nil
}

func (s *marshalState) marshalRuntime(v reflect.Value) (types.Value, error) {
	if !v.CanInterface() {
		return nil, fmt.Errorf("%w: cannot read %s", ErrTypeMismatch, v.Type())
	}
	val, ok := v.Interface().(types.Value)
	if !ok || val == nil {
		return types.Null, nil
	}
	return s.m.resolve(s.i, val)
}

func (s *marshalState) marshalAny(v reflect.Value) (types.Value, error) {
	if v.IsNil() {
		return types.Null, nil
	}
	return s.value(v.Elem())
}

func (s *marshalState) value(v reflect.Value) (types.Value, error) {
	if !v.IsValid() {
		return types.Null, nil
	}
	p, err := s.m.plan(v.Type())
	if err != nil {
		return nil, err
	}
	return p.marshal(s, v)
}

func (s *marshalState) marshalCycle(v reflect.Value) (types.Value, error) {
	return nil, fmt.Errorf("%w: %s", ErrMarshalCycle, v.Type())
}

func (s *marshalState) marshalUnsupported(v reflect.Value) (types.Value, error) {
	return nil, fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, v.Type())
}

func (m *codec) marshalPointer(elem *marshalPlan) marshaler {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		if v.IsNil() {
			return types.Null, nil
		}
		ptr := v.Pointer()
		if s.seen[ptr] {
			return nil, fmt.Errorf("%w: %s", ErrMarshalCycle, v.Type())
		}
		s.seen[ptr] = true
		defer delete(s.seen, ptr)
		return elem.marshal(s, v.Elem())
	}
}

func (m *codec) marshalStruct(fields []fieldPlan, vm *types.StructType) marshaler {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		var base unsafe.Pointer
		if v.CanAddr() {
			base = v.Addr().UnsafePointer()
		} else {
			holder := reflect.New(v.Type())
			holder.Elem().Set(v)
			base = holder.UnsafePointer()
		}
		out := types.NewStruct(vm)
		for slot, pf := range fields {
			field := vm.Fields[slot]
			boxed, err := s.boxFieldAt(base, pf, field.Type)
			if err != nil {
				return nil, fmt.Errorf("struct field %s: %w", field.Name, err)
			}
			if field.Kind == types.KindI64 {
				out.SetRaw(slot, uint64(s.i.unboxI64(boxed)))
			} else {
				out.SetField(slot, boxed)
			}
		}
		return out, nil
	}
}

func (m *codec) marshalArray(elem *marshalPlan) marshaler {
	elemVM := elem.VMType
	elemKind := elem.Type.Kind()
	arrayType := types.NewArrayType(elemVM)
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		switch elemKind {
		case reflect.Bool:
			out := make(types.TypedArray[bool], v.Len())
			for idx := range out {
				out[idx] = v.Index(idx).Bool()
			}
			return out, nil
		case reflect.Int8:
			out := make(types.TypedArray[int8], v.Len())
			for idx := range out {
				out[idx] = int8(v.Index(idx).Int())
			}
			return out, nil
		case reflect.Uint8:
			out := make(types.TypedArray[int8], v.Len())
			for idx := range out {
				out[idx] = int8(v.Index(idx).Uint())
			}
			return out, nil
		case reflect.Int16, reflect.Int32:
			out := make(types.TypedArray[int32], v.Len())
			for idx := range out {
				out[idx] = int32(v.Index(idx).Int())
			}
			return out, nil
		case reflect.Uint16, reflect.Uint32:
			out := make(types.TypedArray[int32], v.Len())
			for idx := range out {
				out[idx] = int32(v.Index(idx).Uint())
			}
			return out, nil
		case reflect.Int, reflect.Int64:
			out := make(types.TypedArray[int64], v.Len())
			for idx := range out {
				out[idx] = v.Index(idx).Int()
			}
			return out, nil
		case reflect.Uint, reflect.Uint64, reflect.Uintptr:
			out := make(types.TypedArray[int64], v.Len())
			for idx := range out {
				out[idx] = int64(v.Index(idx).Uint())
			}
			return out, nil
		case reflect.Float32:
			out := make(types.TypedArray[float32], v.Len())
			for idx := range out {
				out[idx] = float32(v.Index(idx).Float())
			}
			return out, nil
		case reflect.Float64:
			out := make(types.TypedArray[float64], v.Len())
			for idx := range out {
				out[idx] = v.Index(idx).Float()
			}
			return out, nil
		}
		elems := make([]types.Boxed, v.Len())
		for idx := range elems {
			boxed, err := s.boxAs(v.Index(idx), elemVM)
			if err != nil {
				return nil, fmt.Errorf("array element %d: %w", idx, err)
			}
			elems[idx] = boxed
		}
		return types.NewArray(arrayType, elems...), nil
	}
}

func (m *codec) marshalMap(mt *types.MapType) marshaler {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		out := types.NewMapForType(mt, v.Len())
		switch m := out.(type) {
		case *types.TypedMap[int8]:
			iter := v.MapRange()
			for iter.Next() {
				key, err := s.boxAs(iter.Key(), mt.Key)
				if err != nil {
					return nil, fmt.Errorf("map key: %w", err)
				}
				value, err := s.boxAs(iter.Value(), mt.Elem)
				if err != nil {
					return nil, fmt.Errorf("map value: %w", err)
				}
				m.Set(key.I8(), value)
			}
		case *types.TypedMap[bool]:
			iter := v.MapRange()
			for iter.Next() {
				key, err := s.boxAs(iter.Key(), mt.Key)
				if err != nil {
					return nil, fmt.Errorf("map key: %w", err)
				}
				value, err := s.boxAs(iter.Value(), mt.Elem)
				if err != nil {
					return nil, fmt.Errorf("map value: %w", err)
				}
				m.Set(key.Bool(), value)
			}
		case *types.TypedMap[int32]:
			iter := v.MapRange()
			for iter.Next() {
				key, err := s.boxAs(iter.Key(), mt.Key)
				if err != nil {
					return nil, fmt.Errorf("map key: %w", err)
				}
				value, err := s.boxAs(iter.Value(), mt.Elem)
				if err != nil {
					return nil, fmt.Errorf("map value: %w", err)
				}
				m.Set(key.I32(), value)
			}
		case *types.TypedMap[int64]:
			iter := v.MapRange()
			for iter.Next() {
				key, err := s.value(iter.Key())
				if err != nil {
					return nil, fmt.Errorf("map key: %w", err)
				}
				keyInt, ok := asInt(key)
				if !ok {
					return nil, fmt.Errorf("map key: %w: map key type=%s", ErrTypeMismatch, mt.Key)
				}
				value, err := s.boxAs(iter.Value(), mt.Elem)
				if err != nil {
					return nil, fmt.Errorf("map value: %w", err)
				}
				m.Set(keyInt, value)
			}
		case *types.TypedMap[float32]:
			iter := v.MapRange()
			for iter.Next() {
				key, err := s.value(iter.Key())
				if err != nil {
					return nil, fmt.Errorf("map key: %w", err)
				}
				keyFloat, ok := asFloat(key)
				if !ok {
					return nil, fmt.Errorf("map key: %w: map key type=%s", ErrTypeMismatch, mt.Key)
				}
				value, err := s.boxAs(iter.Value(), mt.Elem)
				if err != nil {
					return nil, fmt.Errorf("map value: %w", err)
				}
				m.Set(float32(keyFloat), value)
			}
		case *types.TypedMap[float64]:
			iter := v.MapRange()
			for iter.Next() {
				key, err := s.value(iter.Key())
				if err != nil {
					return nil, fmt.Errorf("map key: %w", err)
				}
				keyFloat, ok := asFloat(key)
				if !ok {
					return nil, fmt.Errorf("map key: %w: map key type=%s", ErrTypeMismatch, mt.Key)
				}
				value, err := s.boxAs(iter.Value(), mt.Elem)
				if err != nil {
					return nil, fmt.Errorf("map value: %w", err)
				}
				m.Set(keyFloat, value)
			}
		case *types.Map:
			iter := v.MapRange()
			for iter.Next() {
				keyValue, err := s.value(iter.Key())
				if err != nil {
					return nil, fmt.Errorf("map key: %w", err)
				}
				var mapKey types.MapKey
				var entryKey types.Boxed
				if mt.Key.Kind() == types.KindRef {
					entryKey = s.boxRef(keyValue)
					mapKey = types.MapKey{Kind: types.KindRef, Bits: uint64(entryKey.Ref())}
				} else {
					return nil, fmt.Errorf("map key: %w: map key type=%s", ErrUnsupportedMarshalType, mt.Key)
				}
				entryValue, err := s.boxAs(iter.Value(), mt.Elem)
				if err != nil {
					return nil, fmt.Errorf("map value: %w", err)
				}
				m.Set(mapKey, types.MapEntry{Key: entryKey, Value: entryValue})
			}
		}
		return out, nil
	}
}

func (m *codec) unmarshalFunc(fnType *types.FunctionType) unmarshaler {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		target, ok := s.i.callable(val)
		if !ok || target.Type() == nil || !target.Type().Equals(fnType) {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, dst.Type())
		}

		typ := dst.Type()
		hasContext := typ.NumIn() > 0 && typ.In(0) == typeContext
		hasError := typ.NumOut() > 0 && typ.Out(typ.NumOut()-1).Implements(typeError)
		dst.Set(reflect.MakeFunc(typ, func(in []reflect.Value) []reflect.Value {
			out := make([]reflect.Value, typ.NumOut())
			for idx := range out {
				out[idx] = reflect.Zero(typ.Out(idx))
			}
			fail := func(err error) []reflect.Value {
				if !hasError {
					panic(err)
				}
				out[len(out)-1] = reflect.ValueOf(err)
				return out
			}

			ctx := context.Background()
			offset := 0
			if hasContext {
				ctx, _ = in[0].Interface().(context.Context)
				if ctx == nil {
					ctx = context.Background()
				}
				offset = 1
			}
			boxed := make([]types.Boxed, len(in)-offset)
			marshal := &marshalState{m: m, i: s.i, seen: make(map[uintptr]bool)}
			for idx := range boxed {
				v, err := marshal.boxAs(in[idx+offset], fnType.Params[idx])
				if err != nil {
					return fail(fmt.Errorf("function param %d: %w", idx, err))
				}
				boxed[idx] = v
			}

			returns, err := s.i.invoke(ctx, val, boxed)
			if err != nil {
				return fail(err)
			}
			defer func() {
				for _, value := range returns {
					s.i.releaseBox(value)
				}
			}()
			for idx := range returns {
				value := reflect.New(typ.Out(idx))
				if err := m.Unmarshal(s.i, returns[idx], value.Interface()); err != nil {
					return fail(fmt.Errorf("function return %d: %w", idx, err))
				}
				out[idx] = value.Elem()
			}
			return out
		}))
		return nil
	}
}

func (m *codec) marshalFunc(fnType *types.FunctionType) marshaler {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		return s.wrapFunc(v, fnType), nil
	}
}

func (m *codec) marshalHost(slots []hostSlot, vm *types.StructType) marshaler {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		ptr := reflect.New(v.Type())
		ptr.Elem().Set(v)
		return s.hostObject(ptr, slots, vm), nil
	}
}

func (m *codec) marshalHostPointer(slots []hostSlot, vm *types.StructType) marshaler {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		if v.IsNil() {
			return types.Null, nil
		}
		ptr := reflect.New(v.Type().Elem())
		ptr.Elem().Set(v.Elem())
		return s.hostObject(ptr, slots, vm), nil
	}
}

func (s *unmarshalState) unmarshalBool(val types.Value, dst reflect.Value) error {
	n, ok := asInt(val)
	if !ok {
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, dst.Type())
	}
	dst.SetBool(n != 0)
	return nil
}

func (s *unmarshalState) unmarshalInt(val types.Value, dst reflect.Value) error {
	n, ok := asInt(val)
	if !ok {
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, dst.Type())
	}
	if dst.OverflowInt(n) {
		return fmt.Errorf("%w: %d overflows %s", ErrValueOverflow, n, dst.Type())
	}
	dst.SetInt(n)
	return nil
}

func (s *unmarshalState) unmarshalUint(val types.Value, dst reflect.Value) error {
	n, ok := asUint(val)
	if !ok {
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, dst.Type())
	}
	if dst.OverflowUint(n) {
		return fmt.Errorf("%w: %d overflows %s", ErrValueOverflow, n, dst.Type())
	}
	dst.SetUint(n)
	return nil
}

func (s *unmarshalState) unmarshalFloat(val types.Value, dst reflect.Value) error {
	f, ok := asFloat(val)
	if !ok {
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, dst.Type())
	}
	if dst.OverflowFloat(f) {
		return fmt.Errorf("%w: %g overflows %s", ErrValueOverflow, f, dst.Type())
	}
	dst.SetFloat(f)
	return nil
}

func (s *unmarshalState) unmarshalString(val types.Value, dst reflect.Value) error {
	str, ok := val.(types.String)
	if !ok {
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, val, dst.Type())
	}
	dst.SetString(string(str))
	return nil
}

func (s *unmarshalState) unmarshalInterface(val types.Value, dst reflect.Value) error {
	value, err := s.m.resolve(s.i, val)
	if err != nil {
		return err
	}
	if value == nil {
		dst.SetZero()
		return nil
	}
	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		dst.SetZero()
		return nil
	}
	if rv.Type().AssignableTo(dst.Type()) {
		dst.Set(rv)
		return nil
	}
	return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
}

func (s *unmarshalState) unmarshalCycle(_ types.Value, dst reflect.Value) error {
	return fmt.Errorf("%w: %s", ErrMarshalCycle, dst.Type())
}

func (s *unmarshalState) unmarshalUnsupported(_ types.Value, dst reflect.Value) error {
	return fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, dst.Type())
}

func (s *unmarshalState) unmarshalCustom(val types.Value, dst reflect.Value) error {
	return dst.Addr().Interface().(ValueUnmarshaler).UnmarshalVM(s.i, val)
}

func (m *codec) unmarshalPointer(elem *marshalPlan) unmarshaler {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		if types.IsNull(val) {
			dst.SetZero()
			return nil
		}
		if value, err := s.m.resolve(s.i, val); err == nil {
			if ho, ok := value.(*HostObject); ok && ho.Receiver.IsValid() {
				if ho.Receiver.Type() == dst.Type() {
					dst.Set(ho.Receiver)
					return nil
				}
			}
		}
		out := reflect.New(dst.Type().Elem())
		if err := elem.unmarshal(s, val, out.Elem()); err != nil {
			return err
		}
		dst.Set(out)
		return nil
	}
}

func (m *codec) unmarshalStruct(fields []fieldPlan) unmarshaler {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		value, err := s.m.resolve(s.i, val)
		if err != nil {
			return err
		}
		if ho, ok := value.(*HostObject); ok && ho.Receiver.IsValid() {
			rv := ho.Receiver
			if rv.Kind() == reflect.Pointer && !rv.IsNil() && rv.Elem().Type() == dst.Type() {
				dst.Set(rv.Elem())
				return nil
			}
		}
		var typ *types.StructType
		var fieldBox func(int) types.Boxed
		var rawBits func(int) uint64
		switch v := value.(type) {
		case *types.Struct:
			typ, fieldBox, rawBits = v.Typ, v.Field, v.Raw
		case *HostObject:
			typ, fieldBox, rawBits = v.Typ, v.Field, v.Raw
		default:
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
		}
		used := make([]bool, len(typ.Fields))
		for _, pf := range fields {
			name := dst.Type().Field(pf.Index).Name
			src, ok := 0, false
			for i, vmField := range typ.Fields {
				if vmField.Name == name {
					src, ok = i, true
					break
				}
			}
			if !ok {
				for i := range typ.Fields {
					if !used[i] {
						src, ok = i, true
						break
					}
				}
			}
			if !ok {
				continue
			}
			used[src] = true
			var fv types.Value
			if typ.Fields[src].Kind == types.KindI64 {
				fv = types.I64(int64(rawBits(src)))
			} else {
				fv, err = s.m.resolve(s.i, fieldBox(src))
				if err != nil {
					return fmt.Errorf("struct field %s: %w", name, err)
				}
			}
			if err := pf.Plan.unmarshal(s, fv, dst.Field(pf.Index)); err != nil {
				return fmt.Errorf("struct field %s: %w", name, err)
			}
		}
		return nil
	}
}

func (m *codec) unmarshalSlice(elem *marshalPlan) unmarshaler {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		if src, ok := val.(types.TypedArray[int8]); ok {
			if k := dst.Type().Elem().Kind(); k == reflect.Int8 || k == reflect.Uint8 {
				out := reflect.MakeSlice(dst.Type(), len(src), len(src))
				for idx, b := range src {
					if k == reflect.Int8 {
						out.Index(idx).SetInt(int64(b))
					} else {
						out.Index(idx).SetUint(uint64(uint8(b)))
					}
				}
				dst.Set(out)
				return nil
			}
		}
		elems, err := s.elems(val)
		if err != nil {
			return err
		}
		out := reflect.MakeSlice(dst.Type(), len(elems), len(elems))
		for idx, e := range elems {
			if err := elem.unmarshal(s, e, out.Index(idx)); err != nil {
				return fmt.Errorf("slice element %d: %w", idx, err)
			}
		}
		dst.Set(out)
		return nil
	}
}

func (m *codec) unmarshalArray(elem *marshalPlan) unmarshaler {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		if src, ok := val.(types.TypedArray[int8]); ok {
			if k := dst.Type().Elem().Kind(); k == reflect.Int8 || k == reflect.Uint8 {
				if len(src) != dst.Len() {
					return fmt.Errorf("%w: array length %d does not match %d", ErrValueOverflow, len(src), dst.Len())
				}
				for idx, b := range src {
					if k == reflect.Int8 {
						dst.Index(idx).SetInt(int64(b))
					} else {
						dst.Index(idx).SetUint(uint64(uint8(b)))
					}
				}
				return nil
			}
		}
		elems, err := s.elems(val)
		if err != nil {
			return err
		}
		if len(elems) != dst.Len() {
			return fmt.Errorf("%w: array length %d does not match %d", ErrValueOverflow, len(elems), dst.Len())
		}
		for idx, e := range elems {
			if err := elem.unmarshal(s, e, dst.Index(idx)); err != nil {
				return fmt.Errorf("array element %d: %w", idx, err)
			}
		}
		return nil
	}
}

func (m *codec) unmarshalMap(keyPlan, valPlan *marshalPlan) unmarshaler {
	return func(s *unmarshalState, src types.Value, dst reflect.Value) error {
		size := 0
		switch m := src.(type) {
		case *types.TypedMap[int8]:
			size = m.Len()
		case *types.TypedMap[bool]:
			size = m.Len()
		case *types.TypedMap[int32]:
			size = m.Len()
		case *types.TypedMap[int64]:
			size = m.Len()
		case *types.TypedMap[float32]:
			size = m.Len()
		case *types.TypedMap[float64]:
			size = m.Len()
		case *types.Map:
			size = m.Len()
		default:
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, src, dst.Type())
		}
		out := reflect.MakeMapWithSize(dst.Type(), size)
		var mapErr error
		set := func(keyValue types.Value, elemValue types.Value) {
			if mapErr != nil {
				return
			}
			k := reflect.New(dst.Type().Key()).Elem()
			if err := keyPlan.unmarshal(s, keyValue, k); err != nil {
				mapErr = fmt.Errorf("map key: %w", err)
				return
			}
			v := reflect.New(dst.Type().Elem()).Elem()
			if err := valPlan.unmarshal(s, elemValue, v); err != nil {
				mapErr = fmt.Errorf("map value: %w", err)
				return
			}
			out.SetMapIndex(k, v)
		}
		switch m := src.(type) {
		case *types.TypedMap[int8]:
			m.Range(func(key int8, value types.Boxed) {
				elemValue, err := s.m.resolve(s.i, value)
				if err != nil {
					mapErr = fmt.Errorf("map value: %w", err)
					return
				}
				set(types.I8(key), elemValue)
			})
		case *types.TypedMap[bool]:
			m.Range(func(key bool, value types.Boxed) {
				elemValue, err := s.m.resolve(s.i, value)
				if err != nil {
					mapErr = fmt.Errorf("map value: %w", err)
					return
				}
				set(types.I1(key), elemValue)
			})
		case *types.TypedMap[int32]:
			m.Range(func(key int32, value types.Boxed) {
				elemValue, err := s.m.resolve(s.i, value)
				if err != nil {
					mapErr = fmt.Errorf("map value: %w", err)
					return
				}
				set(types.I32(key), elemValue)
			})
		case *types.TypedMap[int64]:
			m.Range(func(key int64, value types.Boxed) {
				elemValue, err := s.m.resolve(s.i, value)
				if err != nil {
					mapErr = fmt.Errorf("map value: %w", err)
					return
				}
				set(types.I64(key), elemValue)
			})
		case *types.TypedMap[float32]:
			m.Range(func(key float32, value types.Boxed) {
				elemValue, err := s.m.resolve(s.i, value)
				if err != nil {
					mapErr = fmt.Errorf("map value: %w", err)
					return
				}
				set(types.F32(key), elemValue)
			})
		case *types.TypedMap[float64]:
			m.Range(func(key float64, value types.Boxed) {
				elemValue, err := s.m.resolve(s.i, value)
				if err != nil {
					mapErr = fmt.Errorf("map value: %w", err)
					return
				}
				set(types.F64(key), elemValue)
			})
		case *types.Map:
			m.Range(func(mapKey types.MapKey, entry types.MapEntry) {
				var keyValue types.Value
				var err error
				switch mapKey.Kind {
				case types.KindI32:
					keyValue = types.I32(int32(mapKey.Bits))
				case types.KindI64:
					keyValue = types.I64(int64(mapKey.Bits))
				case types.KindF32:
					keyValue = types.F32(math.Float32frombits(uint32(mapKey.Bits)))
				case types.KindF64:
					keyValue = types.F64(math.Float64frombits(mapKey.Bits))
				default:
					keyValue, err = s.m.resolve(s.i, entry.Key)
				}
				if err != nil {
					mapErr = fmt.Errorf("map key: %w", err)
					return
				}
				elemValue, err := s.m.resolve(s.i, entry.Value)
				if err != nil {
					mapErr = fmt.Errorf("map value: %w", err)
					return
				}
				set(keyValue, elemValue)
			})
		}
		if mapErr != nil {
			return mapErr
		}
		dst.Set(out)
		return nil
	}
}

func (m *codec) resolve(i *Interpreter, val types.Value) (types.Value, error) {
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

// typeOf maps a Go reflect.Kind to its VM-side types.Type.
// Returns nil for kinds that HostObject cannot represent directly.
// reflect.Interface is only valid for types implementing types.Value;
// callers must filter non-Value interfaces before reaching this point.
func (m *codec) typeOf(k reflect.Kind) types.Type {
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

// unmarshalComplex decodes a {Real, Imag} struct into a *complex64 or
// *complex128 destination. Shared by both complex builtin Converters.
func unmarshalComplex(i *Interpreter, val types.Value, dst any) error {
	st, ok := structOf(i, val)
	if !ok {
		return fmt.Errorf("%w: source=%T", ErrTypeMismatch, val)
	}
	re, reOK := asFloat(st.FieldByName("Real"))
	im, imOK := asFloat(st.FieldByName("Imag"))
	if !reOK || !imOK {
		return fmt.Errorf("%w: source=%s", ErrTypeMismatch, st.Typ)
	}
	out := reflect.ValueOf(dst).Elem()
	c := complex(re, im)
	if out.OverflowComplex(c) {
		return fmt.Errorf("%w: %v overflows %s", ErrValueOverflow, c, out.Type())
	}
	out.SetComplex(c)
	return nil
}

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

// structOf resolves val to a *types.Struct, following a heap ref when needed.
func structOf(i *Interpreter, val types.Value) (*types.Struct, bool) {
	if b, ok := val.(types.Boxed); ok && b.Kind() == types.KindRef {
		v, err := i.Load(b.Ref())
		if err != nil {
			return nil, false
		}
		val = v
	}
	st, ok := val.(*types.Struct)
	return st, ok
}
