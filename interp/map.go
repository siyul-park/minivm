package interp

import (
	"math"

	"github.com/siyul-park/minivm/types"
)

func (i *Interpreter) mapKey(typ types.Type, val types.Boxed) (types.MapKey, types.Boxed) {
	switch {
	case typ.Equals(types.TypeI32):
		bits := uint64(uint32(val.I32()))
		return types.MapKey{Kind: types.KindI32, Bits: bits}, types.BoxI32(int32(bits))
	case typ.Equals(types.TypeI64):
		bits := uint64(i.unboxI64(val))
		return types.MapKey{Kind: types.KindI64, Bits: bits}, 0
	case typ.Equals(types.TypeF32):
		bits := math.Float32bits(val.F32())
		if bits == 1<<31 {
			bits = 0
		}
		return types.MapKey{Kind: types.KindF32, Bits: uint64(bits)}, types.BoxF32(math.Float32frombits(bits))
	case typ.Equals(types.TypeF64):
		bits := math.Float64bits(val.F64())
		if bits == 1<<63 {
			bits = 0
		}
		return types.MapKey{Kind: types.KindF64, Bits: bits}, types.BoxF64(math.Float64frombits(bits))
	case typ.Equals(types.TypeString):
		if val.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		str, ok := i.heap[val.Ref()].(types.String)
		if !ok {
			panic(ErrTypeMismatch)
		}
		return types.MapKey{Kind: types.KindRef, Text: string(str)}, types.BoxedNull
	case typ.Equals(types.TypeRef):
		if val.Kind() != types.KindRef {
			panic(ErrTypeMismatch)
		}
		return types.MapKey{Kind: types.KindRef, Bits: uint64(val.Ref())}, val
	default:
		panic(ErrTypeMismatch)
	}
}

func (i *Interpreter) releaseMapKeyArg(typ types.Type, val types.Boxed) {
	if val.Kind() != types.KindRef {
		return
	}
	if typ.Equals(types.TypeString) || typ.Equals(types.TypeRef) {
		i.release(val.Ref())
	}
}

func (i *Interpreter) mapValue(typ types.Type, val types.Boxed) types.Boxed {
	if typ.Equals(types.TypeI64) {
		return i.boxI64(i.unboxI64(val))
	}
	return val
}

func (i *Interpreter) releaseMapValue(val types.Boxed) {
	if val.Kind() == types.KindRef {
		i.release(val.Ref())
	}
}

func (i *Interpreter) zeroMapValue(typ types.Type) types.Boxed {
	switch typ.Kind() {
	case types.KindI32:
		return types.BoxI32(0)
	case types.KindI64:
		return types.BoxI64(0)
	case types.KindF32:
		return types.BoxF32(0)
	case types.KindF64:
		return types.BoxF64(0)
	case types.KindRef:
		i.retain(0)
		return types.BoxedNull
	default:
		return 0
	}
}

func (i *Interpreter) mapSet(m *types.Map, keyVal types.Boxed, val types.Boxed) {
	key, entryKey := i.mapKey(m.Typ.Key, keyVal)
	entryVal := i.mapValue(m.Typ.Elem, val)
	if old, ok := m.Entries[key]; ok {
		i.releaseMapKeyArg(m.Typ.Key, keyVal)
		i.releaseMapValue(old.Value)
	} else if !m.Typ.Key.Equals(types.TypeRef) {
		i.releaseMapKeyArg(m.Typ.Key, keyVal)
	}
	m.Entries[key] = types.MapEntry{Key: entryKey, Value: entryVal}
}

func (i *Interpreter) mapDelete(m *types.Map, keyVal types.Boxed) {
	key, _ := i.mapKey(m.Typ.Key, keyVal)
	if old, ok := m.Entries[key]; ok {
		if m.Typ.Key.Equals(types.TypeRef) && old.Key.Kind() == types.KindRef {
			i.release(old.Key.Ref())
		}
		i.releaseMapValue(old.Value)
		delete(m.Entries, key)
	}
	i.releaseMapKeyArg(m.Typ.Key, keyVal)
}

func (i *Interpreter) mapClear(m *types.Map) {
	for key, entry := range m.Entries {
		if m.Typ.Key.Equals(types.TypeRef) && entry.Key.Kind() == types.KindRef {
			i.release(entry.Key.Ref())
		}
		i.releaseMapValue(entry.Value)
		delete(m.Entries, key)
	}
}
