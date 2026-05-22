package interp

import (
	"errors"
	"fmt"
	"math"
	"reflect"

	"github.com/siyul-park/minivm/types"
)

type Marshaler interface {
	MarshalValue(*Interpreter, any) (types.Value, error)
	UnmarshalValue(*Interpreter, types.Value, any) error
}

var (
	ErrMarshalCycle           = errors.New("marshal cycle")
	ErrUnsupportedMarshalType = errors.New("unsupported marshal type")
	ErrInvalidUnmarshalTarget = errors.New("invalid unmarshal target")
	ErrValueOverflow          = errors.New("value overflow")
)

type reflectMarshaler struct{}

var _ Marshaler = reflectMarshaler{}

var DefaultMarshaler Marshaler = reflectMarshaler{}

var (
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
	return i.marshaler.MarshalValue(i, v)
}

func (i *Interpreter) Unmarshal(v types.Value, dst any) error {
	return i.marshaler.UnmarshalValue(i, v, dst)
}

func (reflectMarshaler) MarshalValue(i *Interpreter, v any) (types.Value, error) {
	state := newMarshalState(i)
	defer state.close()
	return state.value(reflect.ValueOf(v))
}

func (reflectMarshaler) UnmarshalValue(i *Interpreter, v types.Value, dst any) error {
	state := newUnmarshalState(i)
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

type marshalState struct {
	i    *Interpreter
	root int
	seen map[uintptr]bool
}

func newMarshalState(i *Interpreter) *marshalState {
	return &marshalState{
		i:    i,
		seen: make(map[uintptr]bool),
	}
}

func (s *marshalState) value(v reflect.Value) (types.Value, error) {
	if !v.IsValid() {
		return types.Null, nil
	}
	if val, ok, err := s.runtimeValue(v); ok || err != nil {
		return val, err
	}
	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return types.Null, nil
		}
		ptr := v.Pointer()
		if s.seen[ptr] {
			return nil, fmt.Errorf("%w: %s", ErrMarshalCycle, v.Type())
		}
		s.seen[ptr] = true
		defer delete(s.seen, ptr)

		v = v.Elem()
		if val, ok, err := s.runtimeValue(v); ok || err != nil {
			return val, err
		}
	}
	if s.hostable(v) {
		return s.hostObject(v)
	}
	return s.concrete(v)
}

// hostable reports whether v should be marshaled as a HostObject — i.e. a
// type that carries methods on T or *T, or a struct that holds unexported
// fields (which the plain *types.Struct path would silently drop).
func (s *marshalState) hostable(v reflect.Value) bool {
	t := v.Type()
	if t == nil {
		return false
	}
	if _, ok := s.runtimeType(t); ok {
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

func (s *marshalState) runtimeValue(v reflect.Value) (types.Value, bool, error) {
	if !v.CanInterface() {
		return nil, false, nil
	}
	val, ok := v.Interface().(types.Value)
	if !ok {
		return nil, false, nil
	}
	boxed, ok := val.(types.Boxed)
	if !ok {
		return val, true, nil
	}
	if boxed.Kind() != types.KindRef {
		return types.Unbox(boxed), true, nil
	}
	out, err := s.i.Load(boxed.Ref())
	if err != nil {
		return nil, true, fmt.Errorf("load ref %d: %w", boxed.Ref(), err)
	}
	return out, true, nil
}

func (s *marshalState) concrete(v reflect.Value) (types.Value, error) {
	switch v.Kind() {
	case reflect.Bool:
		return types.Bool(v.Bool()), nil
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return types.I32(v.Int()), nil
	case reflect.Int, reflect.Int64:
		return types.I64(v.Int()), nil
	case reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return types.I32(int32(v.Uint())), nil
	case reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return types.I64(int64(v.Uint())), nil
	case reflect.Float32:
		return types.F32(float32(v.Float())), nil
	case reflect.Float64:
		return types.F64(v.Float()), nil
	case reflect.String:
		return types.String(v.String()), nil
	case reflect.Func:
		typ, err := s.functionType(v.Type())
		if err != nil {
			return nil, err
		}
		return s.reflectFunc(v, typ), nil
	case reflect.Array, reflect.Slice:
		return s.array(v)
	case reflect.Map:
		return s.mapValue(v)
	case reflect.Struct:
		return s.structValue(v)
	default:
		return nil, fmt.Errorf("%w: kind=%s", ErrUnsupportedMarshalType, v.Kind())
	}
}

func (s *marshalState) array(v reflect.Value) (types.Value, error) {
	switch v.Type().Elem().Kind() {
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
			out[idx] = int64(v.Index(idx).Int())
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

	elem, err := s.typeOf(v.Type().Elem())
	if err != nil {
		return nil, fmt.Errorf("array element type: %w", err)
	}
	elems := make([]types.Boxed, v.Len())
	for idx := range elems {
		boxed, err := s.boxAs(v.Index(idx), elem)
		if err != nil {
			return nil, fmt.Errorf("array element %d: %w", idx, err)
		}
		elems[idx] = boxed
	}
	return types.NewArray(types.NewArrayType(elem), elems...), nil
}

func (s *marshalState) mapValue(v reflect.Value) (types.Value, error) {
	key, err := s.typeOf(v.Type().Key())
	if err != nil {
		return nil, fmt.Errorf("map key type: %w", err)
	}
	elem, err := s.typeOf(v.Type().Elem())
	if err != nil {
		return nil, fmt.Errorf("map value type: %w", err)
	}
	typ, err := s.mapType(key, elem)
	if err != nil {
		return nil, err
	}

	out := types.NewMapWithCapacity(typ, v.Len())
	for _, key := range v.MapKeys() {
		mapKey, entryKey, err := s.mapKey(typ.Key, key)
		if err != nil {
			return nil, fmt.Errorf("map key: %w", err)
		}
		entryVal, err := s.boxAs(v.MapIndex(key), typ.Elem)
		if err != nil {
			return nil, fmt.Errorf("map value: %w", err)
		}
		out.Set(mapKey, types.MapEntry{Key: entryKey, Value: entryVal})
	}
	return out, nil
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

func (s *marshalState) reflectFunc(fn reflect.Value, typ *types.FunctionType) *HostFunction {
	return NewHostFunction(typ, func(i *Interpreter, params []types.Boxed) ([]types.Boxed, error) {
		fnType := fn.Type()
		if len(params) != fnType.NumIn() {
			return nil, fmt.Errorf("%w: got %d params, want %d", ErrTypeMismatch, len(params), fnType.NumIn())
		}
		in := make([]reflect.Value, fnType.NumIn())
		unmarshal := newUnmarshalState(i)
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
		marshal := newMarshalState(i)
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

func (s *marshalState) hostObject(v reflect.Value) (types.Value, error) {
	rt := v.Type()
	ptr := reflect.New(rt)
	ptr.Elem().Set(v)

	var fields []types.StructField
	var slots []hostSlot
	names := make(map[string]bool)

	if rt.Kind() == reflect.Struct {
		for idx := 0; idx < rt.NumField(); idx++ {
			f := rt.Field(idx)
			if f.PkgPath != "" {
				continue
			}
			ft, ok := hostFieldType(f.Type)
			if !ok {
				continue
			}
			fields = append(fields, types.NewStructField(ft, types.FieldWithName(f.Name)))
			slots = append(slots, hostSlot{field: idx})
			names[f.Name] = true
		}
	}

	methodType := reflect.PointerTo(rt)
	for j := 0; j < methodType.NumMethod(); j++ {
		m := methodType.Method(j)
		if !m.IsExported() || names[m.Name] {
			continue
		}
		boundFn := ptr.Method(j)
		ft, err := s.functionType(boundFn.Type())
		if err != nil {
			return nil, fmt.Errorf("method %s: %w", m.Name, err)
		}
		hf := s.reflectFunc(boundFn, ft)
		addr := s.alloc(hf)
		fields = append(fields, types.NewStructField(ft, types.FieldWithName(m.Name)))
		slots = append(slots, hostSlot{field: -1, addr: addr})
		names[m.Name] = true
	}

	return &HostObject{
		Typ:      types.NewStructType(fields...),
		Receiver: ptr,
		slots:    slots,
		interp:   s.i,
	}, nil
}

// hostFieldType maps the reflect type of a HostObject data field to the VM
// type that drives field.Kind dispatch in STRUCT_GET / STRUCT_SET. Returns
// (_, false) for kinds the live reflect path does not support yet.
func hostFieldType(t reflect.Type) (types.Type, bool) {
	switch t.Kind() {
	case reflect.Bool, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return types.TypeI32, true
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return types.TypeI64, true
	case reflect.Float32:
		return types.TypeF32, true
	case reflect.Float64:
		return types.TypeF64, true
	case reflect.String:
		return types.TypeString, true
	}
	if t.Implements(typeValue) {
		return types.TypeRef, true
	}
	return nil, false
}

func (s *marshalState) structValue(v reflect.Value) (types.Value, error) {
	typ, indexes, err := s.structTypeSeen(v.Type(), make(map[reflect.Type]bool))
	if err != nil {
		return nil, err
	}
	out := types.NewStruct(typ)
	for idx, fieldIdx := range indexes {
		field := typ.Fields[idx]
		boxed, err := s.boxAs(v.Field(fieldIdx), field.Type)
		if err != nil {
			return nil, fmt.Errorf("struct field %s: %w", field.Name, err)
		}
		if field.Kind == types.KindI64 {
			out.SetRaw(idx, uint64(s.i.unboxI64(boxed)))
		} else {
			out.SetField(idx, boxed)
		}
	}
	return out, nil
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

func (s *marshalState) typeOf(t reflect.Type) (types.Type, error) {
	return s.typeOfSeen(t, make(map[reflect.Type]bool))
}

func (s *marshalState) typeOfSeen(t reflect.Type, seen map[reflect.Type]bool) (types.Type, error) {
	if typ, ok := s.runtimeType(t); ok {
		return typ, nil
	}
	if t.Kind() == reflect.Pointer {
		elem := t.Elem()
		if typ, ok := s.runtimeType(elem); ok {
			return typ, nil
		}
		if t.Implements(typeValue) || seen[elem] {
			return types.TypeRef, nil
		}
		return s.typeOfSeen(elem, seen)
	}
	switch t.Kind() {
	case reflect.Bool, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return types.TypeI32, nil
	case reflect.Int, reflect.Int64, reflect.Uint, reflect.Uint64, reflect.Uintptr:
		return types.TypeI64, nil
	case reflect.Float32:
		return types.TypeF32, nil
	case reflect.Float64:
		return types.TypeF64, nil
	case reflect.String:
		return types.TypeString, nil
	case reflect.Func:
		return s.functionType(t)
	case reflect.Array, reflect.Slice:
		elem, err := s.typeOfSeen(t.Elem(), seen)
		if err != nil {
			return nil, err
		}
		return types.NewArrayType(elem), nil
	case reflect.Map:
		key, err := s.typeOfSeen(t.Key(), seen)
		if err != nil {
			return nil, fmt.Errorf("map key type: %w", err)
		}
		elem, err := s.typeOfSeen(t.Elem(), seen)
		if err != nil {
			return nil, fmt.Errorf("map value type: %w", err)
		}
		return s.mapType(key, elem)
	case reflect.Struct:
		typ, _, err := s.structTypeSeen(t, seen)
		return typ, err
	default:
		if t.Implements(typeValue) {
			return types.TypeRef, nil
		}
		return nil, fmt.Errorf("%w: type=%s", ErrUnsupportedMarshalType, t)
	}
}

func (s *marshalState) structTypeSeen(t reflect.Type, seen map[reflect.Type]bool) (*types.StructType, []int, error) {
	if seen[t] {
		return nil, nil, fmt.Errorf("%w: type=%s", ErrMarshalCycle, t)
	}
	seen[t] = true
	defer delete(seen, t)

	fields := make([]types.StructField, 0, t.NumField())
	indexes := make([]int, 0, t.NumField())
	for idx := 0; idx < t.NumField(); idx++ {
		field := t.Field(idx)
		if field.PkgPath != "" {
			continue
		}
		typ, err := s.typeOfSeen(field.Type, seen)
		if err != nil {
			return nil, nil, fmt.Errorf("struct field %s: %w", field.Name, err)
		}
		fields = append(fields, types.NewStructField(typ, types.FieldWithName(field.Name)))
		indexes = append(indexes, idx)
	}
	return types.NewStructType(fields...), indexes, nil
}

func (s *marshalState) functionType(t reflect.Type) (*types.FunctionType, error) {
	params := make([]types.Type, t.NumIn())
	for idx := range params {
		typ, err := s.typeOf(t.In(idx))
		if err != nil {
			return nil, fmt.Errorf("function param %d: %w", idx, err)
		}
		params[idx] = typ
	}

	outs := t.NumOut()
	if outs > 0 && t.Out(outs-1).Implements(typeError) {
		outs--
	}
	returns := make([]types.Type, outs)
	for idx := range returns {
		typ, err := s.typeOf(t.Out(idx))
		if err != nil {
			return nil, fmt.Errorf("function return %d: %w", idx, err)
		}
		returns[idx] = typ
	}
	return &types.FunctionType{Params: params, Returns: returns}, nil
}

func (*marshalState) runtimeType(t reflect.Type) (types.Type, bool) {
	typ, ok := runtimeTypes[t]
	return typ, ok
}

func (*marshalState) mapType(key, elem types.Type) (*types.MapType, error) {
	if !types.IsComparableMapKeyType(key) {
		return nil, fmt.Errorf("%w: map key type=%s", ErrUnsupportedMarshalType, key)
	}
	return types.NewMapType(key, elem), nil
}

type unmarshalState struct {
	i *Interpreter
}

func newUnmarshalState(i *Interpreter) *unmarshalState {
	return &unmarshalState{i: i}
}

func (s *unmarshalState) value(val types.Value, dst reflect.Value) error {
	if !dst.CanSet() {
		return fmt.Errorf("%w: destination is not settable", ErrInvalidUnmarshalTarget)
	}
	if dst.Kind() == reflect.Pointer {
		if types.IsNull(val) {
			dst.SetZero()
			return nil
		}
		out := reflect.New(dst.Type().Elem())
		if err := s.value(val, out.Elem()); err != nil {
			return err
		}
		dst.Set(out)
		return nil
	}
	if dst.Type().Implements(typeValue) {
		return s.runtimeValue(val, dst)
	}

	value, err := s.resolve(val)
	if err != nil {
		return err
	}
	if ho, ok := value.(*HostObject); ok && ho.Receiver.IsValid() {
		rv := ho.Receiver
		if rv.Kind() == reflect.Pointer && !rv.IsNil() {
			if rv.Elem().Type() == dst.Type() {
				dst.Set(rv.Elem())
				return nil
			}
			if rv.Type() == dst.Type() {
				dst.Set(rv)
				return nil
			}
		}
	}
	return s.concrete(value, dst)
}

func (s *unmarshalState) runtimeValue(val types.Value, dst reflect.Value) error {
	value, err := s.resolve(val)
	if err != nil {
		return err
	}
	if reflect.TypeOf(value).AssignableTo(dst.Type()) {
		dst.Set(reflect.ValueOf(value))
		return nil
	}
	return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
}

func (s *unmarshalState) concrete(value types.Value, dst reflect.Value) error {
	switch dst.Kind() {
	case reflect.Bool:
		n, ok := signedValue(value)
		if !ok {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
		}
		dst.SetBool(n != 0)
		return nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok := signedValue(value)
		if !ok {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
		}
		if dst.OverflowInt(n) {
			return fmt.Errorf("%w: %d overflows %s", ErrValueOverflow, n, dst.Type())
		}
		dst.SetInt(n)
		return nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, ok := unsignedValue(value)
		if !ok {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
		}
		if dst.OverflowUint(n) {
			return fmt.Errorf("%w: %d overflows %s", ErrValueOverflow, n, dst.Type())
		}
		dst.SetUint(n)
		return nil
	case reflect.Float32, reflect.Float64:
		f, ok := floatValue(value)
		if !ok {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
		}
		if dst.OverflowFloat(f) {
			return fmt.Errorf("%w: %g overflows %s", ErrValueOverflow, f, dst.Type())
		}
		dst.SetFloat(f)
		return nil
	case reflect.String:
		str, ok := value.(types.String)
		if !ok {
			return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
		}
		dst.SetString(string(str))
		return nil
	case reflect.Slice:
		elems, err := s.arrayValues(value)
		if err != nil {
			return err
		}
		out := reflect.MakeSlice(dst.Type(), len(elems), len(elems))
		for idx, elem := range elems {
			if err := s.value(elem, out.Index(idx)); err != nil {
				return fmt.Errorf("slice element %d: %w", idx, err)
			}
		}
		dst.Set(out)
		return nil
	case reflect.Array:
		return s.array(value, dst)
	case reflect.Map:
		return s.mapValue(value, dst)
	case reflect.Struct:
		return s.structValue(value, dst)
	case reflect.Interface:
		if reflect.TypeOf(value).AssignableTo(dst.Type()) {
			dst.Set(reflect.ValueOf(value))
			return nil
		}
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
	default:
		return fmt.Errorf("%w: kind=%s", ErrUnsupportedMarshalType, dst.Kind())
	}
}

func (s *unmarshalState) array(value types.Value, dst reflect.Value) error {
	elems, err := s.arrayValues(value)
	if err != nil {
		return err
	}
	if len(elems) != dst.Len() {
		return fmt.Errorf("%w: array length %d does not match %d", ErrValueOverflow, len(elems), dst.Len())
	}
	for idx, elem := range elems {
		if err := s.value(elem, dst.Index(idx)); err != nil {
			return fmt.Errorf("array element %d: %w", idx, err)
		}
	}
	return nil
}

func (s *unmarshalState) mapValue(value types.Value, dst reflect.Value) error {
	m, ok := value.(*types.Map)
	if !ok {
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
	}
	out := reflect.MakeMapWithSize(dst.Type(), m.Len())
	var mapErr error
	m.Range(func(key types.MapKey, entry types.MapEntry) {
		if mapErr != nil {
			return
		}
		k := reflect.New(dst.Type().Key()).Elem()
		if err := s.mapKey(m.Typ.Key, key, entry.Key, k); err != nil {
			mapErr = fmt.Errorf("map key: %w", err)
			return
		}
		v := reflect.New(dst.Type().Elem()).Elem()
		entryValue, err := s.boxedValue(entry.Value)
		if err == nil {
			err = s.value(entryValue, v)
		}
		if err != nil {
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

func (s *unmarshalState) structValue(value types.Value, dst reflect.Value) error {
	var typ *types.StructType
	var fieldBox func(int) types.Boxed
	var rawBits func(int) uint64
	switch strct := value.(type) {
	case *types.Struct:
		typ = strct.Typ
		fieldBox = strct.Field
		rawBits = strct.Raw
	case *HostObject:
		typ = strct.Typ
		fieldBox = strct.Field
		rawBits = strct.Raw
	default:
		return fmt.Errorf("%w: source=%T target=%s", ErrTypeMismatch, value, dst.Type())
	}
	used := make([]bool, len(typ.Fields))
	for idx := 0; idx < dst.NumField(); idx++ {
		field := dst.Type().Field(idx)
		if field.PkgPath != "" {
			continue
		}
		src, ok := 0, false
		for i, vmField := range typ.Fields {
			if vmField.Name == field.Name {
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
		var val types.Value
		if typ.Fields[src].Kind == types.KindI64 {
			val = types.I64(int64(rawBits(src)))
		} else {
			var err error
			val, err = s.boxedValue(fieldBox(src))
			if err != nil {
				return fmt.Errorf("struct field %s: %w", field.Name, err)
			}
		}
		if err := s.value(val, dst.Field(idx)); err != nil {
			return fmt.Errorf("struct field %s: %w", field.Name, err)
		}
	}
	return nil
}

func (s *unmarshalState) mapKey(typ types.Type, key types.MapKey, entry types.Boxed, dst reflect.Value) error {
	switch {
	case typ.Equals(types.TypeString):
		return s.value(types.String(key.Text), dst)
	case typ.Equals(types.TypeI32):
		return s.value(types.I32(int32(key.Bits)), dst)
	case typ.Equals(types.TypeI64):
		return s.value(types.I64(int64(key.Bits)), dst)
	case typ.Equals(types.TypeF32):
		return s.value(types.F32(math.Float32frombits(uint32(key.Bits))), dst)
	case typ.Equals(types.TypeF64):
		return s.value(types.F64(math.Float64frombits(key.Bits)), dst)
	default:
		value, err := s.boxedValue(entry)
		if err != nil {
			return err
		}
		return s.value(value, dst)
	}
}

func (s *unmarshalState) arrayValues(value types.Value) ([]types.Value, error) {
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
			val, err := s.boxedValue(elem)
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

func (s *unmarshalState) resolve(val types.Value) (types.Value, error) {
	boxed, ok := val.(types.Boxed)
	if !ok {
		return val, nil
	}
	return s.boxedValue(boxed)
}

func (s *unmarshalState) boxedValue(val types.Boxed) (types.Value, error) {
	if val.Kind() != types.KindRef {
		return types.Unbox(val), nil
	}
	out, err := s.i.Load(val.Ref())
	if err != nil {
		return nil, fmt.Errorf("load ref %d: %w", val.Ref(), err)
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
