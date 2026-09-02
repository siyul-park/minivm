package codegen

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type match struct {
	op      instr.Opcode
	typ     reflect.Type
	exclude bool
}

type pattern []match

type family struct {
	sources []pattern
	binary  []instr.Opcode
	compare []instr.Opcode
}

var families = []family{
	{sources: producers[types.I32](instr.I32_CONST), binary: []instr.Opcode{
		instr.I32_ADD, instr.I32_SUB, instr.I32_MUL, instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U, instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U, instr.I32_XOR, instr.I32_AND, instr.I32_OR, instr.I32_ROTL, instr.I32_ROTR,
	}, compare: []instr.Opcode{instr.I32_EQ, instr.I32_NE, instr.I32_LT_S, instr.I32_LT_U, instr.I32_GT_S, instr.I32_GT_U, instr.I32_LE_S, instr.I32_LE_U, instr.I32_GE_S, instr.I32_GE_U}},
	{sources: producers[types.I64](instr.I64_CONST), binary: []instr.Opcode{
		instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U, instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U, instr.I64_XOR, instr.I64_AND, instr.I64_OR, instr.I64_ROTL, instr.I64_ROTR,
	}, compare: []instr.Opcode{instr.I64_EQ, instr.I64_NE, instr.I64_LT_S, instr.I64_LT_U, instr.I64_GT_S, instr.I64_GT_U, instr.I64_LE_S, instr.I64_LE_U, instr.I64_GE_S, instr.I64_GE_U}},
	{sources: producers[types.F32](instr.F32_CONST), binary: []instr.Opcode{
		instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV, instr.F32_REM, instr.F32_MOD, instr.F32_MIN, instr.F32_MAX, instr.F32_COPYSIGN,
	}, compare: []instr.Opcode{instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_GT, instr.F32_LE, instr.F32_GE}},
	{sources: producers[types.F64](instr.F64_CONST), binary: []instr.Opcode{
		instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV, instr.F64_REM, instr.F64_MOD, instr.F64_MIN, instr.F64_MAX, instr.F64_COPYSIGN,
	}, compare: []instr.Opcode{instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_GT, instr.F64_LE, instr.F64_GE}},
}

func (p pattern) width() int {
	size := 0
	for _, current := range p {
		size += width(current.op)
	}
	return size
}

func (p pattern) key() string {
	var key strings.Builder
	for index, current := range p {
		if index > 0 {
			key.WriteByte('/')
		}
		fmt.Fprintf(&key, "%d", current.op)
		if current.typ != nil {
			fmt.Fprintf(&key, ":%t:%s", current.exclude, current.typ)
		}
	}
	return key.String()
}

func (p pattern) overlaps(other pattern) bool {
	if len(p) != len(other) {
		return false
	}
	for index := range p {
		if p[index].op != other[index].op || !p[index].overlaps(other[index]) {
			return false
		}
	}
	return true
}

func (m match) overlaps(other match) bool {
	if m.typ == nil || other.typ == nil {
		return true
	}
	if !m.exclude && !other.exclude {
		return m.typ == other.typ
	}
	if m.exclude && other.exclude {
		return true
	}
	if m.exclude {
		m, other = other, m
	}
	return m.typ != other.typ
}

func catalog() []pattern {
	patterns := cross(
		[]pattern{op(instr.LOCAL_GET), op(instr.GLOBAL_GET), op(instr.UPVAL_GET)},
		op(instr.DROP),
		op(instr.REF_IS_NULL),
		seq(op(instr.REF_IS_NULL), op(instr.BR_IF)),
	)
	patterns = append(patterns, cross(
		[]pattern{except[types.String]()},
		op(instr.DROP),
		op(instr.REF_IS_NULL),
		seq(op(instr.REF_IS_NULL), op(instr.BR_IF)),
	)...)
	patterns = append(patterns,
		seq(op(instr.REF_NULL), op(instr.DROP)),
		seq(op(instr.REF_NULL), op(instr.REF_IS_NULL)),
		seq(op(instr.REF_NULL), op(instr.REF_IS_NULL), op(instr.BR_IF)),
		seq(op(instr.DUP), op(instr.DROP)),
		seq(op(instr.DUP), op(instr.REF_IS_NULL)),
		seq(op(instr.DUP), op(instr.REF_IS_NULL), op(instr.BR_IF)),
		seq(op(instr.CONST_GET), op(instr.CALL)),
		seq(op(instr.CONST_GET), op(instr.RETURN_CALL)),
		seq(op(instr.CONST_GET), op(instr.CLOSURE_NEW)),
	)
	patterns = append(patterns, scalars()...)
	sort.Slice(patterns, func(i, j int) bool {
		if len(patterns[i]) != len(patterns[j]) {
			return len(patterns[i]) > len(patterns[j])
		}
		return patterns[i].key() < patterns[j].key()
	})
	return patterns
}

func scalars() []pattern {
	var patterns []pattern
	for _, family := range families {
		ops := append(append([]instr.Opcode(nil), family.binary...), family.compare...)
		consumers := make([]pattern, 0, len(ops)+len(family.compare))
		for _, consumer := range ops {
			consumers = append(consumers, op(consumer))
			if !traps(consumer) && slices.Contains(family.binary, consumer) {
				patterns = append(patterns, seq(op(consumer), op(instr.LOCAL_SET)))
			}
		}
		for _, compare := range family.compare {
			consumers = append(consumers, seq(op(compare), op(instr.BR_IF)))
			patterns = append(patterns, seq(op(compare), op(instr.BR_IF)))
		}
		patterns = append(patterns, cross(family.sources, consumers...)...)

		immediate := family.sources[len(family.sources)-1]
		for _, rhs := range []pattern{immediate, op(instr.LOCAL_GET)} {
			for _, consumer := range ops {
				patterns = append(patterns, seq(op(instr.LOCAL_GET), rhs, op(consumer)))
				if !traps(consumer) && slices.Contains(family.binary, consumer) {
					patterns = append(patterns, seq(op(instr.LOCAL_GET), rhs, op(consumer), op(instr.LOCAL_SET)))
				}
			}
			for _, compare := range family.compare {
				patterns = append(patterns, seq(op(instr.LOCAL_GET), rhs, op(compare), op(instr.BR_IF)))
			}
		}

		secondary := []pattern{immediate, op(instr.LOCAL_GET), op(instr.GLOBAL_GET), op(instr.UPVAL_GET)}
		for _, lhs := range []pattern{op(instr.GLOBAL_GET), op(instr.UPVAL_GET)} {
			for _, rhs := range secondary {
				for _, consumer := range ops {
					patterns = append(patterns, seq(lhs, rhs, op(consumer)))
				}
				for _, compare := range family.compare {
					patterns = append(patterns, seq(lhs, rhs, op(compare), op(instr.BR_IF)))
				}
			}
		}
	}
	arrayConstants := []pattern{
		constant[types.TypedArray[bool]](),
		constant[types.TypedArray[int8]](),
		constant[types.TypedArray[int32]](),
		constant[types.TypedArray[int64]](),
		constant[types.TypedArray[float32]](),
		constant[types.TypedArray[float64]](),
	}
	arrayContainers := append([]pattern{}, arrayConstants...)
	for _, source := range slotSources {
		arrayContainers = append(arrayContainers,
			typedContainer[types.TypedArray[bool]](source),
			typedContainer[types.TypedArray[int8]](source),
			typedContainer[types.TypedArray[int32]](source),
			typedContainer[types.TypedArray[int64]](source),
			typedContainer[types.TypedArray[float32]](source),
			typedContainer[types.TypedArray[float64]](source),
		)
	}
	indexed := make([]pattern, 0, len(producers[types.I32](instr.I32_CONST)))
	for _, index := range producers[types.I32](instr.I32_CONST) {
		indexed = append(indexed, seq(index, op(instr.ARRAY_GET)))
	}
	patterns = append(patterns, cross(arrayContainers, indexed...)...)
	setPatterns := make([]pattern, 0, len(arrayContainers)*len(producers[types.I32](instr.I32_CONST))*4)
	for _, container := range arrayContainers {
		kind, _ := arrayKind(container[0].typ)
		for _, index := range producers[types.I32](instr.I32_CONST) {
			for _, val := range arrayProducers(kind) {
				setPatterns = append(setPatterns, seq(container, index, val, op(instr.ARRAY_SET)))
			}
		}
	}
	patterns = append(patterns, setPatterns...)
	// array.get and struct.get both accept a compile-time constant field
	// index alone, leaving whatever container instruction precedes them
	// unfused.
	fieldIndex := []pattern{op(instr.I32_CONST), constant[types.I32]()}
	patterns = append(patterns, cross(fieldIndex, op(instr.ARRAY_GET), op(instr.STRUCT_GET))...)
	// A struct.get container may also be a LOCAL_GET, GLOBAL_GET, or
	// UPVAL_GET whose declared type is a concrete struct, letting the fused
	// consumer resolve the accessed field's Kind from the declared type at
	// threading time.
	structFields := make([]pattern, 0, len(fieldIndex))
	for _, index := range fieldIndex {
		structFields = append(structFields, seq(index, op(instr.STRUCT_GET)))
	}
	structContainers := make([]pattern, 0, len(slotSources))
	for _, source := range slotSources {
		structContainers = append(structContainers, structContainer(source))
	}
	patterns = append(patterns, cross(structContainers, structFields...)...)
	return append(patterns,
		seq(op(instr.I32_EQZ), op(instr.BR_IF)),
		seq(op(instr.I64_EQZ), op(instr.BR_IF)),
		seq(op(instr.I32_CONST), op(instr.BR_IF)),
	)
}

func arrayProducers(kind instr.Kind) []pattern {
	switch kind.Repr() {
	case instr.KindI1, instr.KindI8, instr.KindI32:
		return producers[types.I32](instr.I32_CONST)
	case instr.KindI64:
		return producers[types.I64](instr.I64_CONST)
	case instr.KindF32:
		return producers[types.F32](instr.F32_CONST)
	case instr.KindF64:
		return producers[types.F64](instr.F64_CONST)
	default:
		panic(fmt.Sprintf("unsupported array element kind %s", kind))
	}
}

func producers[T types.Value](immediate instr.Opcode) []pattern {
	return []pattern{
		op(instr.LOCAL_GET),
		op(instr.GLOBAL_GET),
		op(instr.UPVAL_GET),
		constant[T](),
		op(immediate),
	}
}

func cross(sources []pattern, consumers ...pattern) []pattern {
	patterns := make([]pattern, 0, len(sources)*len(consumers))
	for _, source := range sources {
		for _, consumer := range consumers {
			patterns = append(patterns, seq(source, consumer))
		}
	}
	return patterns
}

func seq(parts ...pattern) pattern {
	var result pattern
	for _, part := range parts {
		result = append(result, part...)
	}
	return result
}

func op(code instr.Opcode) pattern {
	return pattern{{op: code}}
}

func constant[T types.Value]() pattern {
	return pattern{{op: instr.CONST_GET, typ: reflect.TypeFor[T]()}}
}

// slotSources lists the slot-read opcodes whose declared type can prove
// an array.get/struct.get container's shape at threading time: a local slot,
// a module global, or a closure's captured upvalue.
var slotSources = []instr.Opcode{instr.LOCAL_GET, instr.GLOBAL_GET, instr.UPVAL_GET}

// typedContainer matches source (LOCAL_GET, GLOBAL_GET, or UPVAL_GET) whose
// declared slot type is the concrete array type T, letting a fused consumer
// prove the container's element kind at threading time instead of
// re-deriving it from the runtime heap value.
func typedContainer[T types.Value](source instr.Opcode) pattern {
	return pattern{{op: source, typ: reflect.TypeFor[T]()}}
}

// structContainer matches source (LOCAL_GET, GLOBAL_GET, or UPVAL_GET)
// whose declared slot type is a struct. Unlike typedContainer, one Go type
// covers every struct shape, so the fused struct.get consumer resolves the
// specific declared *types.StructType (and each accessed field's Kind) at
// threading time instead of selecting it here.
func structContainer(source instr.Opcode) pattern {
	return pattern{{op: source, typ: reflect.TypeFor[types.Struct]()}}
}

func except[T types.Value]() pattern {
	return pattern{{op: instr.CONST_GET, typ: reflect.TypeFor[T](), exclude: true}}
}
