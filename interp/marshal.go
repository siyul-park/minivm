package interp

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"sync"
	"unsafe"

	"github.com/siyul-park/minivm/types"
)

type Marshaler interface {
	Marshal(*Interpreter, any) (types.Value, error)
	Unmarshal(*Interpreter, types.Value, any) error
}

type marshaler struct {
	plans sync.Map
}

type marshalPlan struct {
	VMType    types.Type
	Type      reflect.Type
	marshal   marshalFn
	unmarshal unmarshalFn
}

type marshalFn func(*marshalState, reflect.Value) (types.Value, error)

type unmarshalFn func(*unmarshalState, types.Value, reflect.Value) error

type planField struct {
	Index  int
	Offset uintptr
	Kind   reflect.Kind
	Plan   *marshalPlan
}

type marshalState struct {
	m    *marshaler
	i    *Interpreter
	root int
	seen map[uintptr]bool
}

type unmarshalState struct {
	m *marshaler
	i *Interpreter
}

var (
	ErrMarshalCycle           = errors.New("marshal cycle")
	ErrUnsupportedMarshalType = errors.New("unsupported marshal type")
	ErrInvalidUnmarshalTarget = errors.New("invalid unmarshal target")
	ErrValueOverflow          = errors.New("value overflow")
)

var DefaultMarshaler Marshaler = newMarshaler()

var (
	_ Marshaler = (*marshaler)(nil)

	typeError = reflect.TypeOf((*error)(nil)).Elem()
	typeValue = reflect.TypeOf((*types.Value)(nil)).Elem()

	runtimeTypes = map[reflect.Type]types.Type{
		reflect.TypeOf(types.I32(0)):     types.TypeI32,
		reflect.TypeOf(types.I64(0)):     types.TypeI64,
		reflect.TypeOf(types.F32(0)):     types.TypeF32,
		reflect.TypeOf(types.F64(0)):     types.TypeF64,
		reflect.TypeOf(types.Ref(0)):     types.TypeRef,
		reflect.TypeOf(types.Boxed(0)):   types.TypeRef,
		reflect.TypeOf(types.String("")): types.TypeString,
	}
)

func (i *Interpreter) Marshal(v any) (types.Value, error) {
	return i.marshaler.Marshal(i, v)
}

func (i *Interpreter) Unmarshal(v types.Value, dst any) error {
	return i.marshaler.Unmarshal(i, v, dst)
}

func (m *marshaler) Marshal(i *Interpreter, v any) (types.Value, error) {
	state := newMarshalState(i, m)
	defer state.close()
	return state.value(reflect.ValueOf(v))
}

func (m *marshaler) Unmarshal(i *Interpreter, v types.Value, dst any) error {
	state := newUnmarshalState(i, m)
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

func (m *marshaler) plan(t reflect.Type) (*marshalPlan, error) {
	if p, ok := m.plans.Load(t); ok {
		return p.(*marshalPlan), nil
	}
	p, err := m.compilePlan(t, make(map[reflect.Type]bool))
	if err != nil {
		return nil, err
	}
	actual, _ := m.plans.LoadOrStore(t, p)
	return actual.(*marshalPlan), nil
}

func (m *marshaler) compilePlan(t reflect.Type, seen map[reflect.Type]bool) (*marshalPlan, error) {
	if vmType, ok := runtimeTypes[t]; ok {
		return &marshalPlan{VMType: vmType, Type: t, marshal: marshalRuntime, unmarshal: unmarshalInterface}, nil
	}
	if seen[t] {
		return &marshalPlan{VMType: types.TypeRef, Type: t, marshal: marshalCycle, unmarshal: unmarshalCycle}, nil
	}
	seen[t] = true
	defer delete(seen, t)

	if m.isHostable(t) {
		fields, slots, data, err := m.compileHostObject(t, seen)
		if err != nil {
			return nil, err
		}
		vmStruct := types.NewStructType(fields...)
		return &marshalPlan{
			VMType:    vmStruct,
			Type:      t,
			marshal:   hostMarshaler(slots, vmStruct),
			unmarshal: structUnmarshaler(data),
		}, nil
	}
	if t.Kind() == reflect.Pointer {
		elem := t.Elem()
		if seen[elem] {
			return &marshalPlan{VMType: types.TypeRef, Type: t, marshal: marshalRuntime, unmarshal: unmarshalInterface}, nil
		}
		elemPlan, err := m.compilePlan(elem, seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    elemPlan.VMType,
			Type:      t,
			marshal:   pointerMarshaler(elemPlan),
			unmarshal: pointerUnmarshaler(elemPlan),
		}, nil
	}
	if t.Implements(typeValue) {
		return &marshalPlan{VMType: types.TypeRef, Type: t, marshal: marshalRuntime, unmarshal: unmarshalInterface}, nil
	}
	switch t.Kind() {
	case reflect.Bool:
		return &marshalPlan{VMType: types.TypeI32, Type: t, marshal: marshalBool, unmarshal: unmarshalBool}, nil
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return &marshalPlan{VMType: types.TypeI32, Type: t, marshal: marshalI32, unmarshal: unmarshalInt}, nil
	case reflect.Int, reflect.Int64:
		return &marshalPlan{VMType: types.TypeI64, Type: t, marshal: marshalI64, unmarshal: unmarshalInt}, nil
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &marshalPlan{VMType: types.TypeI32, Type: t, marshal: marshalU32, unmarshal: unmarshalUint}, nil
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return &marshalPlan{VMType: types.TypeI64, Type: t, marshal: marshalU64, unmarshal: unmarshalUint}, nil
	case reflect.Float32:
		return &marshalPlan{VMType: types.TypeF32, Type: t, marshal: marshalF32, unmarshal: unmarshalFloat}, nil
	case reflect.Float64:
		return &marshalPlan{VMType: types.TypeF64, Type: t, marshal: marshalF64, unmarshal: unmarshalFloat}, nil
	case reflect.String:
		return &marshalPlan{VMType: types.TypeString, Type: t, marshal: marshalString, unmarshal: unmarshalString}, nil
	case reflect.Interface:
		return &marshalPlan{VMType: types.TypeRef, Type: t, marshal: marshalAny, unmarshal: unmarshalInterface}, nil
	case reflect.Func:
		fnType, err := m.compileFunctionType(t, 0, seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    fnType,
			Type:      t,
			marshal:   funcMarshaler(fnType),
			unmarshal: unmarshalUnsupported,
		}, nil
	case reflect.Array:
		elemPlan, err := m.compilePlan(t.Elem(), seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    types.NewArrayType(elemPlan.VMType),
			Type:      t,
			marshal:   arrayMarshaler(elemPlan),
			unmarshal: arrayUnmarshaler(elemPlan),
		}, nil
	case reflect.Slice:
		elemPlan, err := m.compilePlan(t.Elem(), seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    types.NewArrayType(elemPlan.VMType),
			Type:      t,
			marshal:   arrayMarshaler(elemPlan),
			unmarshal: sliceUnmarshaler(elemPlan),
		}, nil
	case reflect.Map:
		keyPlan, err := m.compilePlan(t.Key(), seen)
		if err != nil {
			return nil, fmt.Errorf("map key type: %w", err)
		}
		valPlan, err := m.compilePlan(t.Elem(), seen)
		if err != nil {
			return nil, fmt.Errorf("map value type: %w", err)
		}
		mt, err := mapType(keyPlan.VMType, valPlan.VMType)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    mt,
			Type:      t,
			marshal:   mapMarshaler(mt),
			unmarshal: mapUnmarshaler(keyPlan, valPlan),
		}, nil
	case reflect.Struct:
		vmStruct, fields, err := m.compileStructType(t, seen)
		if err != nil {
			return nil, err
		}
		return &marshalPlan{
			VMType:    vmStruct,
			Type:      t,
			marshal:   structMarshaler(fields, vmStruct),
			unmarshal: structUnmarshaler(fields),
		}, nil
	}
	return nil, fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, t)
}

func (m *marshaler) isHostable(t reflect.Type) bool {
	if _, ok := runtimeTypes[t]; ok {
		return false
	}
	if t.Implements(typeValue) || reflect.PointerTo(t).Implements(typeValue) {
		return false
	}
	switch t.Kind() {
	case reflect.Func, reflect.Chan, reflect.Map, reflect.Slice, reflect.Interface, reflect.Pointer:
		return false
	}
	if reflect.PointerTo(t).NumMethod() > 0 {
		return true
	}
	if t.Kind() == reflect.Struct {
		for idx := 0; idx < t.NumField(); idx++ {
			if t.Field(idx).PkgPath != "" {
				return true
			}
		}
	}
	return false
}

func (m *marshaler) compileStructType(t reflect.Type, seen map[reflect.Type]bool) (*types.StructType, []planField, error) {
	fields := make([]types.StructField, 0, t.NumField())
	plans := make([]planField, 0, t.NumField())
	for idx := 0; idx < t.NumField(); idx++ {
		field := t.Field(idx)
		if field.PkgPath != "" {
			continue
		}
		p, err := m.compilePlan(field.Type, seen)
		if err != nil {
			return nil, nil, fmt.Errorf("struct field %s: %w", field.Name, err)
		}
		fields = append(fields, types.NewStructField(p.VMType, types.FieldWithName(field.Name)))
		plans = append(plans, planField{
			Index:  idx,
			Offset: field.Offset,
			Kind:   field.Type.Kind(),
			Plan:   p,
		})
	}
	return types.NewStructType(fields...), plans, nil
}

func (m *marshaler) compileHostObject(t reflect.Type, seen map[reflect.Type]bool) ([]types.StructField, []hostSlot, []planField, error) {
	var fields []types.StructField
	var slots []hostSlot
	var data []planField
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
			vmType := hostFieldVMType(kind)
			if vmType == nil {
				continue
			}
			p, err := m.compilePlan(f.Type, seen)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("host field %s: %w", f.Name, err)
			}
			fields = append(fields, types.NewStructField(vmType, types.FieldWithName(f.Name)))
			slots = append(slots, hostSlot{field: idx, offset: f.Offset, kind: kind})
			data = append(data, planField{Index: idx, Offset: f.Offset, Kind: kind, Plan: p})
			names[f.Name] = true
		}
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
	return fields, slots, data, nil
}

func (m *marshaler) compileFunctionType(t reflect.Type, skip int, seen map[reflect.Type]bool) (*types.FunctionType, error) {
	params := make([]types.Type, t.NumIn()-skip)
	for idx := range params {
		p, err := m.compilePlan(t.In(idx+skip), seen)
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
		p, err := m.compilePlan(t.Out(idx), seen)
		if err != nil {
			return nil, fmt.Errorf("function return %d: %w", idx, err)
		}
		returns[idx] = p.VMType
	}
	return &types.FunctionType{Params: params, Returns: returns}, nil
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

func (s *marshalState) mapKey(typ types.Type, v reflect.Value) (types.MapKey, types.Boxed, error) {
	val, err := s.value(v)
	if err != nil {
		return types.MapKey{}, 0, err
	}
	switch {
	case typ.Equals(types.TypeI32):
		boxed, err := s.boxed(val, typ)
		if err != nil {
			return types.MapKey{}, 0, err
		}
		bits := uint64(uint32(boxed.I32()))
		return types.MapKey{Kind: types.KindI32, Bits: bits}, types.BoxI32(int32(bits)), nil
	case typ.Equals(types.TypeI64):
		n, ok := signedValue(val)
		if !ok {
			return types.MapKey{}, 0, fmt.Errorf("%w: map key type=%s", ErrTypeMismatch, typ)
		}
		return types.MapKey{Kind: types.KindI64, Bits: uint64(n)}, 0, nil
	case typ.Equals(types.TypeF32):
		f, ok := floatValue(val)
		if !ok {
			return types.MapKey{}, 0, fmt.Errorf("%w: map key type=%s", ErrTypeMismatch, typ)
		}
		bits := math.Float32bits(float32(f))
		if bits == 1<<31 {
			bits = 0
		}
		return types.MapKey{Kind: types.KindF32, Bits: uint64(bits)}, types.BoxF32(math.Float32frombits(bits)), nil
	case typ.Equals(types.TypeF64):
		f, ok := floatValue(val)
		if !ok {
			return types.MapKey{}, 0, fmt.Errorf("%w: map key type=%s", ErrTypeMismatch, typ)
		}
		bits := math.Float64bits(f)
		if bits == 1<<63 {
			bits = 0
		}
		return types.MapKey{Kind: types.KindF64, Bits: bits}, types.BoxF64(math.Float64frombits(bits)), nil
	case typ.Equals(types.TypeString):
		str, ok := val.(types.String)
		if !ok {
			return types.MapKey{}, 0, fmt.Errorf("%w: map key type=%s", ErrTypeMismatch, typ)
		}
		return types.MapKey{Kind: types.KindRef, Text: string(str)}, types.BoxedNull, nil
	case typ.Equals(types.TypeRef):
		boxed := s.boxRef(val)
		return types.MapKey{Kind: types.KindRef, Bits: uint64(boxed.Ref())}, boxed, nil
	default:
		return types.MapKey{}, 0, fmt.Errorf("%w: map key type=%s", ErrUnsupportedMarshalType, typ)
	}
}

func (s *marshalState) wrapFunc(fn reflect.Value, typ *types.FunctionType) *HostFunction {
	m := s.m
	return NewHostFunction(typ, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
		fnType := fn.Type()
		if len(params) != fnType.NumIn() {
			return nil, fmt.Errorf("%w: got %d params, want %d", ErrTypeMismatch, len(params), fnType.NumIn())
		}
		in := make([]reflect.Value, fnType.NumIn())
		unmarshal := newUnmarshalState(i, m)
		for idx := range in {
			arg := reflect.New(fnType.In(idx)).Elem()
			if err := unmarshal.value(params[idx], arg); err != nil {
				return nil, fmt.Errorf("function param %d: %w", idx, err)
			}
			in[idx] = arg
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
		marshal := newMarshalState(i, m)
		defer marshal.close()
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

func (s *marshalState) boxFieldAt(base unsafe.Pointer, pf planField, typ types.Type) (types.Boxed, error) {
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
		n, ok := signedValue(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i32", ErrTypeMismatch)
		}
		if n < math.MinInt32 || n > math.MaxInt32 {
			return 0, fmt.Errorf("%w: %d overflows i32", ErrValueOverflow, n)
		}
		return types.BoxI32(int32(n)), nil
	case types.KindI64:
		n, ok := signedValue(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=i64", ErrTypeMismatch)
		}
		if types.IsBoxable(n) {
			return types.BoxI64(n), nil
		}
		return types.BoxRef(s.alloc(types.I64(n))), nil
	case types.KindF32:
		f, ok := floatValue(val)
		if !ok {
			return 0, fmt.Errorf("%w: target=f32", ErrTypeMismatch)
		}
		return types.BoxF32(float32(f)), nil
	case types.KindF64:
		f, ok := floatValue(val)
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
	default:
		return types.BoxRef(s.alloc(val))
	}
}

func (s *marshalState) alloc(val types.Value) int {
	addr, _ := s.i.Alloc(val)
	s.root += s.i.root(types.BoxRef(addr))
	return addr
}

func (s *marshalState) close() {
	s.i.unroot(s.root)
	s.root = 0
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

func (s *unmarshalState) mapKey(typ types.Type, key types.MapKey, entry types.Boxed) (types.Value, error) {
	switch {
	case typ.Equals(types.TypeString):
		return types.String(key.Text), nil
	case typ.Equals(types.TypeI32):
		return types.I32(int32(key.Bits)), nil
	case typ.Equals(types.TypeI64):
		return types.I64(int64(key.Bits)), nil
	case typ.Equals(types.TypeF32):
		return types.F32(math.Float32frombits(uint32(key.Bits))), nil
	case typ.Equals(types.TypeF64):
		return types.F64(math.Float64frombits(key.Bits)), nil
	default:
		return loadValue(s.i, entry)
	}
}

func (s *unmarshalState) arrayElems(value types.Value) ([]types.Value, error) {
	switch v := value.(type) {
	case types.I32Array:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.I32(elem)
		}
		return out, nil
	case types.I64Array:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.I64(elem)
		}
		return out, nil
	case types.F32Array:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.F32(elem)
		}
		return out, nil
	case types.F64Array:
		out := make([]types.Value, len(v))
		for idx, elem := range v {
			out[idx] = types.F64(elem)
		}
		return out, nil
	case *types.Array:
		out := make([]types.Value, len(v.Elems))
		for idx, elem := range v.Elems {
			val, err := loadValue(s.i, elem)
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

func newMarshaler() *marshaler {
	return &marshaler{}
}

func newMarshalState(i *Interpreter, m *marshaler) *marshalState {
	return &marshalState{
		m:    m,
		i:    i,
		seen: make(map[uintptr]bool),
	}
}

func newUnmarshalState(i *Interpreter, m *marshaler) *unmarshalState {
	return &unmarshalState{m: m, i: i}
}

func marshalBool(_ *marshalState, v reflect.Value) (types.Value, error) {
	return types.Bool(v.Bool()), nil
}

func marshalI32(_ *marshalState, v reflect.Value) (types.Value, error) {
	return types.I32(v.Int()), nil
}

func marshalI64(_ *marshalState, v reflect.Value) (types.Value, error) {
	return types.I64(v.Int()), nil
}

func marshalU32(_ *marshalState, v reflect.Value) (types.Value, error) {
	return types.I32(int32(v.Uint())), nil
}

func marshalU64(_ *marshalState, v reflect.Value) (types.Value, error) {
	return types.I64(int64(v.Uint())), nil
}

func marshalF32(_ *marshalState, v reflect.Value) (types.Value, error) {
	return types.F32(float32(v.Float())), nil
}

func marshalF64(_ *marshalState, v reflect.Value) (types.Value, error) {
	return types.F64(v.Float()), nil
}

func marshalString(_ *marshalState, v reflect.Value) (types.Value, error) {
	return types.String(v.String()), nil
}

func marshalRuntime(s *marshalState, v reflect.Value) (types.Value, error) {
	if !v.CanInterface() {
		return nil, fmt.Errorf("%w: cannot read %s", ErrTypeMismatch, v.Type())
	}
	val, ok := v.Interface().(types.Value)
	if !ok || val == nil {
		return types.Null, nil
	}
	return loadValue(s.i, val)
}

func marshalAny(s *marshalState, v reflect.Value) (types.Value, error) {
	if v.IsNil() {
		return types.Null, nil
	}
	return s.value(v.Elem())
}

func marshalCycle(_ *marshalState, v reflect.Value) (types.Value, error) {
	return nil, fmt.Errorf("%w: %s", ErrMarshalCycle, v.Type())
}

func pointerMarshaler(elem *marshalPlan) marshalFn {
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

func structMarshaler(fields []planField, vm *types.StructType) marshalFn {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		base := addrPointer(v)
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

func arrayMarshaler(elem *marshalPlan) marshalFn {
	elemVM := elem.VMType
	elemKind := elem.Type.Kind()
	arrayType := types.NewArrayType(elemVM)
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		switch elemKind {
		case reflect.Int8, reflect.Int16, reflect.Int32:
			out := make(types.I32Array, v.Len())
			for idx := range out {
				out[idx] = int32(v.Index(idx).Int())
			}
			return out, nil
		case reflect.Uint8, reflect.Uint16, reflect.Uint32:
			out := make(types.I32Array, v.Len())
			for idx := range out {
				out[idx] = int32(v.Index(idx).Uint())
			}
			return out, nil
		case reflect.Int, reflect.Int64:
			out := make(types.I64Array, v.Len())
			for idx := range out {
				out[idx] = v.Index(idx).Int()
			}
			return out, nil
		case reflect.Uint, reflect.Uint64, reflect.Uintptr:
			out := make(types.I64Array, v.Len())
			for idx := range out {
				out[idx] = int64(v.Index(idx).Uint())
			}
			return out, nil
		case reflect.Float32:
			out := make(types.F32Array, v.Len())
			for idx := range out {
				out[idx] = float32(v.Index(idx).Float())
			}
			return out, nil
		case reflect.Float64:
			out := make(types.F64Array, v.Len())
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

func mapMarshaler(mt *types.MapType) marshalFn {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		out := types.NewMapWithCapacity(mt, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			mapKey, entryKey, err := s.mapKey(mt.Key, iter.Key())
			if err != nil {
				return nil, fmt.Errorf("map key: %w", err)
			}
			entryVal, err := s.boxAs(iter.Value(), mt.Elem)
			if err != nil {
				return nil, fmt.Errorf("map value: %w", err)
			}
			out.Set(mapKey, types.MapEntry{Key: entryKey, Value: entryVal})
		}
		return out, nil
	}
}

func funcMarshaler(fnType *types.FunctionType) marshalFn {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		return s.wrapFunc(v, fnType), nil
	}
}

func hostMarshaler(slots []hostSlot, vm *types.StructType) marshalFn {
	return func(s *marshalState, v reflect.Value) (types.Value, error) {
		ptr := reflect.New(v.Type())
		ptr.Elem().Set(v)
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
		}, nil
	}
}

func unmarshalBool(_ *unmarshalState, val types.Value, dst reflect.Value) error {
	n, ok := signedValue(val)
	if !ok {
		return mismatchErr(val, dst)
	}
	dst.SetBool(n != 0)
	return nil
}

func unmarshalInt(_ *unmarshalState, val types.Value, dst reflect.Value) error {
	n, ok := signedValue(val)
	if !ok {
		return mismatchErr(val, dst)
	}
	if dst.OverflowInt(n) {
		return fmt.Errorf("%w: %d overflows %s", ErrValueOverflow, n, dst.Type())
	}
	dst.SetInt(n)
	return nil
}

func unmarshalUint(_ *unmarshalState, val types.Value, dst reflect.Value) error {
	n, ok := unsignedValue(val)
	if !ok {
		return mismatchErr(val, dst)
	}
	if dst.OverflowUint(n) {
		return fmt.Errorf("%w: %d overflows %s", ErrValueOverflow, n, dst.Type())
	}
	dst.SetUint(n)
	return nil
}

func unmarshalFloat(_ *unmarshalState, val types.Value, dst reflect.Value) error {
	f, ok := floatValue(val)
	if !ok {
		return mismatchErr(val, dst)
	}
	if dst.OverflowFloat(f) {
		return fmt.Errorf("%w: %g overflows %s", ErrValueOverflow, f, dst.Type())
	}
	dst.SetFloat(f)
	return nil
}

func unmarshalString(_ *unmarshalState, val types.Value, dst reflect.Value) error {
	str, ok := val.(types.String)
	if !ok {
		return mismatchErr(val, dst)
	}
	dst.SetString(string(str))
	return nil
}

func unmarshalInterface(s *unmarshalState, val types.Value, dst reflect.Value) error {
	value, err := loadValue(s.i, val)
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
	return mismatchErr(value, dst)
}

func unmarshalCycle(_ *unmarshalState, _ types.Value, dst reflect.Value) error {
	return fmt.Errorf("%w: %s", ErrMarshalCycle, dst.Type())
}

func unmarshalUnsupported(_ *unmarshalState, _ types.Value, dst reflect.Value) error {
	return fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, dst.Type())
}

func pointerUnmarshaler(elem *marshalPlan) unmarshalFn {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		if types.IsNull(val) {
			dst.SetZero()
			return nil
		}
		if value, err := loadValue(s.i, val); err == nil {
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

func structUnmarshaler(fields []planField) unmarshalFn {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		value, err := loadValue(s.i, val)
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
			return mismatchErr(value, dst)
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
				fv, err = loadValue(s.i, fieldBox(src))
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

func sliceUnmarshaler(elem *marshalPlan) unmarshalFn {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		elems, err := s.arrayElems(val)
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

func arrayUnmarshaler(elem *marshalPlan) unmarshalFn {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		elems, err := s.arrayElems(val)
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

func mapUnmarshaler(keyPlan, valPlan *marshalPlan) unmarshalFn {
	return func(s *unmarshalState, val types.Value, dst reflect.Value) error {
		m, ok := val.(*types.Map)
		if !ok {
			return mismatchErr(val, dst)
		}
		out := reflect.MakeMapWithSize(dst.Type(), m.Len())
		var mapErr error
		m.Range(func(mk types.MapKey, entry types.MapEntry) {
			if mapErr != nil {
				return
			}
			kv, err := s.mapKey(m.Typ.Key, mk, entry.Key)
			if err != nil {
				mapErr = fmt.Errorf("map key: %w", err)
				return
			}
			k := reflect.New(dst.Type().Key()).Elem()
			if err := keyPlan.unmarshal(s, kv, k); err != nil {
				mapErr = fmt.Errorf("map key: %w", err)
				return
			}
			ev, err := loadValue(s.i, entry.Value)
			if err != nil {
				mapErr = fmt.Errorf("map value: %w", err)
				return
			}
			v := reflect.New(dst.Type().Elem()).Elem()
			if err := valPlan.unmarshal(s, ev, v); err != nil {
				mapErr = fmt.Errorf("map value: %w", err)
				return
			}
			out.SetMapIndex(k, v)
		})
		if mapErr != nil {
			return mapErr
		}
		dst.Set(out)
		return nil
	}
}

func addrPointer(v reflect.Value) unsafe.Pointer {
	if v.CanAddr() {
		return v.Addr().UnsafePointer()
	}
	holder := reflect.New(v.Type())
	holder.Elem().Set(v)
	return holder.UnsafePointer()
}

func mapType(key, elem types.Type) (*types.MapType, error) {
	if !types.IsComparableMapKeyType(key) {
		return nil, fmt.Errorf("%w: map key type=%s", ErrUnsupportedMarshalType, key)
	}
	return types.NewMapType(key, elem), nil
}

func mismatchErr(src any, dst reflect.Value) error {
	return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, src, dst.Type())
}

func loadValue(i *Interpreter, val types.Value) (types.Value, error) {
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

func signedValue(val types.Value) (int64, bool) {
	switch v := val.(type) {
	case types.I32:
		return int64(v), true
	case types.I64:
		return int64(v), true
	case types.Boxed:
		switch v.Kind() {
		case types.KindI32:
			return int64(v.I32()), true
		case types.KindI64:
			return v.I64(), true
		}
	}
	return 0, false
}

func unsignedValue(val types.Value) (uint64, bool) {
	switch v := val.(type) {
	case types.I32:
		return uint64(uint32(v)), true
	case types.I64:
		return uint64(v), true
	case types.Boxed:
		switch v.Kind() {
		case types.KindI32:
			return uint64(uint32(v.I32())), true
		case types.KindI64:
			return uint64(v.I64()), true
		}
	}
	return 0, false
}

func floatValue(val types.Value) (float64, bool) {
	switch v := val.(type) {
	case types.F32:
		return float64(v), true
	case types.F64:
		return float64(v), true
	case types.Boxed:
		switch v.Kind() {
		case types.KindF32:
			return float64(v.F32()), true
		case types.KindF64:
			return v.F64(), true
		}
	}
	return 0, false
}
