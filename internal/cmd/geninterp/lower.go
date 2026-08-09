package main

import (
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type value struct {
	op       instr.Opcode
	head     instr.Opcode
	compile  []jen.Code
	room     bool
	check    []jen.Code
	body     []jen.Code
	drop     []jen.Code
	push     []jen.Code
	raw      jen.Code
	boxed    jen.Code
	object   jen.Code
	typ      reflect.Type
	declared jen.Code // compile-time *types.StructType proven for a struct container (local, global, or upvalue)
	resident bool
	handler  jen.Code
}

type step struct {
	match
	kind   instr.Kind
	boxed  bool
	commit bool
}

type state struct {
	stack      []value
	offset     int
	width      int
	label      string
	standalone bool
}

type target struct {
	code   jen.Code
	addr   jen.Code
	upvals jen.Code
	ref    jen.Code
}

type loader struct {
	slot       int
	width      int
	raw        string
	boxed      string
	index      string
	addr       string
	pos        jen.Code
	label      string
	standalone bool
}

type lowerer func(*state, step) (value, error)

var lowerers = [256]lowerer{
	instr.ARRAY_APPEND:        bind(arrayAppend),
	instr.ARRAY_COPY:          bind(arrayCopy),
	instr.ARRAY_DELETE:        bind(arrayDelete),
	instr.ARRAY_FILL:          bind(arrayFill),
	instr.ARRAY_GET:           index,
	instr.ARRAY_LEN:           bind(arrayLen),
	instr.ARRAY_NEW:           bind(arrayNew),
	instr.ARRAY_NEW_DEFAULT:   bind(arrayNewDefault),
	instr.ARRAY_SET:           element,
	instr.ARRAY_SLICE:         bind(arraySlice),
	instr.BR:                  bind(br),
	instr.BR_IF:               branch,
	instr.BR_TABLE:            bind(brTable),
	instr.CALL:                call,
	instr.CLOSURE_NEW:         call,
	instr.CONST_GET:           source,
	instr.CORO_DONE:           bind(coroDone),
	instr.CORO_VALUE:          bind(coroValue),
	instr.DROP:                ref,
	instr.DUP:                 ref,
	instr.ERROR_CODE:          bind(errorCode),
	instr.ERROR_GET:           bind(errorGet),
	instr.ERROR_NEW:           bind(errorNew),
	instr.F32_ABS:             bind(f32Abs),
	instr.F32_ADD:             scalar,
	instr.F32_CEIL:            bind(f32Ceil),
	instr.F32_CONST:           source,
	instr.F32_COPYSIGN:        scalar,
	instr.F32_DIV:             scalar,
	instr.F32_EQ:              scalar,
	instr.F32_FLOOR:           bind(f32Floor),
	instr.F32_GE:              scalar,
	instr.F32_GT:              scalar,
	instr.F32_LE:              scalar,
	instr.F32_LT:              scalar,
	instr.F32_MAX:             scalar,
	instr.F32_MIN:             scalar,
	instr.F32_MOD:             scalar,
	instr.F32_MUL:             scalar,
	instr.F32_NE:              scalar,
	instr.F32_NEAREST:         bind(f32Nearest),
	instr.F32_NEG:             bind(f32Neg),
	instr.F32_REINTERPRET_I32: bind(f32ReinterpretI32),
	instr.F32_REM:             scalar,
	instr.F32_SQRT:            bind(f32Sqrt),
	instr.F32_SUB:             scalar,
	instr.F32_TO_F64:          bind(f32ToF64),
	instr.F32_TO_I32_S:        bind(f32ToI32S),
	instr.F32_TO_I32_U:        bind(f32ToI32U),
	instr.F32_TO_I64_S:        bind(f32ToI64S),
	instr.F32_TO_I64_U:        bind(f32ToI64U),
	instr.F32_TRUNC:           bind(f32Trunc),
	instr.F64_ABS:             bind(f64Abs),
	instr.F64_ADD:             scalar,
	instr.F64_CEIL:            bind(f64Ceil),
	instr.F64_CONST:           source,
	instr.F64_COPYSIGN:        scalar,
	instr.F64_DIV:             scalar,
	instr.F64_EQ:              scalar,
	instr.F64_FLOOR:           bind(f64Floor),
	instr.F64_GE:              scalar,
	instr.F64_GT:              scalar,
	instr.F64_LE:              scalar,
	instr.F64_LT:              scalar,
	instr.F64_MAX:             scalar,
	instr.F64_MIN:             scalar,
	instr.F64_MOD:             scalar,
	instr.F64_MUL:             scalar,
	instr.F64_NE:              scalar,
	instr.F64_NEAREST:         bind(f64Nearest),
	instr.F64_NEG:             bind(f64Neg),
	instr.F64_REINTERPRET_I64: bind(f64ReinterpretI64),
	instr.F64_REM:             scalar,
	instr.F64_SQRT:            bind(f64Sqrt),
	instr.F64_SUB:             scalar,
	instr.F64_TO_F32:          bind(f64ToF32),
	instr.F64_TO_I32_S:        bind(f64ToI32S),
	instr.F64_TO_I32_U:        bind(f64ToI32U),
	instr.F64_TO_I64_S:        bind(f64ToI64S),
	instr.F64_TO_I64_U:        bind(f64ToI64U),
	instr.F64_TRUNC:           bind(f64Trunc),
	instr.GLOBAL_GET:          source,
	instr.GLOBAL_SET:          bind(globalSet),
	instr.GLOBAL_TEE:          bind(globalTee),
	instr.I32_ADD:             scalar,
	instr.I32_AND:             scalar,
	instr.I32_CLZ:             bind(i32Clz),
	instr.I32_CONST:           source,
	instr.I32_CTZ:             bind(i32Ctz),
	instr.I32_DIV_S:           scalar,
	instr.I32_DIV_U:           scalar,
	instr.I32_EQ:              scalar,
	instr.I32_EQZ:             scalar,
	instr.I32_EXTEND16_S:      bind(i32Extend16S),
	instr.I32_EXTEND8_S:       bind(i32Extend8S),
	instr.I32_GE_S:            scalar,
	instr.I32_GE_U:            scalar,
	instr.I32_GT_S:            scalar,
	instr.I32_GT_U:            scalar,
	instr.I32_LE_S:            scalar,
	instr.I32_LE_U:            scalar,
	instr.I32_LT_S:            scalar,
	instr.I32_LT_U:            scalar,
	instr.I32_MUL:             scalar,
	instr.I32_NE:              scalar,
	instr.I32_OR:              scalar,
	instr.I32_POPCNT:          bind(i32Popcnt),
	instr.I32_REINTERPRET_F32: bind(i32ReinterpretF32),
	instr.I32_REM_S:           scalar,
	instr.I32_REM_U:           scalar,
	instr.I32_ROTL:            scalar,
	instr.I32_ROTR:            scalar,
	instr.I32_SHL:             scalar,
	instr.I32_SHR_S:           scalar,
	instr.I32_SHR_U:           scalar,
	instr.I32_SUB:             scalar,
	instr.I32_TO_F32_S:        bind(i32ToF32S),
	instr.I32_TO_F32_U:        bind(i32ToF32U),
	instr.I32_TO_F64_S:        bind(i32ToF64S),
	instr.I32_TO_F64_U:        bind(i32ToF64U),
	instr.I32_TO_I64_S:        bind(i32ToI64S),
	instr.I32_TO_I64_U:        bind(i32ToI64U),
	instr.I32_XOR:             scalar,
	instr.I64_ADD:             scalar,
	instr.I64_AND:             scalar,
	instr.I64_CLZ:             bind(i64Clz),
	instr.I64_CONST:           source,
	instr.I64_CTZ:             bind(i64Ctz),
	instr.I64_DIV_S:           scalar,
	instr.I64_DIV_U:           scalar,
	instr.I64_EQ:              scalar,
	instr.I64_EQZ:             scalar,
	instr.I64_EXTEND16_S:      bind(i64Extend16S),
	instr.I64_EXTEND32_S:      bind(i64Extend32S),
	instr.I64_EXTEND8_S:       bind(i64Extend8S),
	instr.I64_GE_S:            scalar,
	instr.I64_GE_U:            scalar,
	instr.I64_GT_S:            scalar,
	instr.I64_GT_U:            scalar,
	instr.I64_LE_S:            scalar,
	instr.I64_LE_U:            scalar,
	instr.I64_LT_S:            scalar,
	instr.I64_LT_U:            scalar,
	instr.I64_MUL:             scalar,
	instr.I64_NE:              scalar,
	instr.I64_OR:              scalar,
	instr.I64_POPCNT:          bind(i64Popcnt),
	instr.I64_REINTERPRET_F64: bind(i64ReinterpretF64),
	instr.I64_REM_S:           scalar,
	instr.I64_REM_U:           scalar,
	instr.I64_ROTL:            scalar,
	instr.I64_ROTR:            scalar,
	instr.I64_SHL:             scalar,
	instr.I64_SHR_S:           scalar,
	instr.I64_SHR_U:           scalar,
	instr.I64_SUB:             scalar,
	instr.I64_TO_F32_S:        bind(i64ToF32S),
	instr.I64_TO_F32_U:        bind(i64ToF32U),
	instr.I64_TO_F64_S:        bind(i64ToF64S),
	instr.I64_TO_F64_U:        bind(i64ToF64U),
	instr.I64_TO_I32:          bind(i64ToI32),
	instr.I64_XOR:             scalar,
	instr.LOCAL_GET:           source,
	instr.LOCAL_SET:           store,
	instr.LOCAL_TEE:           bind(localTee),
	instr.MAP_CLEAR:           bind(mapClear),
	instr.MAP_DELETE:          bind(mapDelete),
	instr.MAP_GET:             bind(mapGet),
	instr.MAP_ITER:            bind(mapIter),
	instr.MAP_KEYS:            bind(mapKeys),
	instr.MAP_LEN:             bind(mapLen),
	instr.MAP_LOOKUP:          bind(mapLookup),
	instr.MAP_NEW:             bind(mapNew),
	instr.MAP_NEW_DEFAULT:     bind(mapNewDefault),
	instr.MAP_SET:             bind(mapSet),
	instr.NOP:                 bind(nop),
	instr.REF_CAST:            bind(refCast),
	instr.REF_EQ:              bind(refEq),
	instr.REF_GET:             bind(refGet),
	instr.REF_IS_NULL:         ref,
	instr.REF_NE:              bind(refNe),
	instr.REF_NEW:             bind(refNew),
	instr.REF_NULL:            ref,
	instr.REF_SET:             bind(refSet),
	instr.REF_TEST:            bind(refTest),
	instr.RESUME:              bind(resume),
	instr.RETURN:              bind(returnOp),
	instr.RETURN_CALL:         call,
	instr.SELECT:              bind(selectOp),
	instr.STRING_CONCAT:       bind(stringConcat),
	instr.STRING_ENCODE_UTF32: bind(stringEncodeUtf32),
	instr.STRING_EQ:           bind(stringEq),
	instr.STRING_GE:           bind(stringGe),
	instr.STRING_GT:           bind(stringGt),
	instr.STRING_ITER:         bind(stringIter),
	instr.STRING_LE:           bind(stringLe),
	instr.STRING_LEN:          bind(stringLen),
	instr.STRING_LT:           bind(stringLt),
	instr.STRING_NE:           bind(stringNe),
	instr.STRING_NEW_UTF32:    bind(stringNewUtf32),
	instr.STRUCT_GET:          index,
	instr.STRUCT_NEW:          bind(structNew),
	instr.STRUCT_NEW_DEFAULT:  bind(structNewDefault),
	instr.STRUCT_SET:          bind(structSet),
	instr.SWAP:                bind(swap),
	instr.THROW:               bind(throw),
	instr.UNREACHABLE:         bind(unreachable),
	instr.UPVAL_GET:           source,
	instr.UPVAL_SET:           bind(upvalSet),
	instr.YIELD:               bind(yield),
}

func newLoader(op instr.Opcode, slot, offset int, label string, standalone bool) loader {
	name := temp(slot)
	at := add(jen.Id("start"), offset)
	if standalone {
		at = jen.Id("c").Dot("ip")
	}
	return loader{
		slot:       slot,
		width:      width(op),
		raw:        name,
		boxed:      boxed(name),
		index:      fmt.Sprintf("i%d", slot),
		addr:       fmt.Sprintf("i%d", 2+slot),
		pos:        at,
		label:      label,
		standalone: standalone,
	}
}

func (l loader) decode(result *value, op instr.Opcode) {
	if !l.standalone {
		return
	}
	switch op {
	case instr.LOCAL_GET, instr.UPVAL_GET:
		result.compile = append(result.compile,
			jen.Id(l.index).Op(":=").Int().Call(jen.Id("c").Dot("code").Index(jen.Add(l.pos).Op("+").Lit(1))),
		)
	case instr.GLOBAL_GET, instr.CONST_GET:
		result.compile = append(result.compile,
			jen.Id(l.index).Op(":=").Int().Call(
				jen.Op("*").Parens(jen.Op("*").Uint16()).Call(
					jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Add(l.pos).Op("+").Lit(1)))),
				),
			),
		)
	}
}

func (l loader) read(result *value, current step) error {
	field, _, ok := slotInfo(current.op)
	if !ok {
		return fmt.Errorf("unsupported slot opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	if current.op == instr.LOCAL_GET || !l.standalone {
		guard, err := l.bounds(current, field)
		if err != nil {
			return err
		}
		result.compile = append(result.compile, guard...)
	}
	// l.read is called only for LOCAL_GET, GLOBAL_GET, and UPVAL_GET (see
	// load's switch), so current.typ != nil alone identifies a container
	// guard; typedContainer/structContainer are the only pattern builders
	// that set it for these three opcodes.
	if current.typ != nil {
		if current.typ == reflect.TypeFor[types.Struct]() {
			if err := l.structGuard(result, current); err != nil {
				return err
			}
		} else if err := l.arrayGuard(result, current); err != nil {
			return err
		}
	}
	switch current.op {
	case instr.LOCAL_GET:
		if l.standalone {
			result.check = append(result.check,
				jen.Id(l.addr).Op(":=").Id("i").Dot("fr").Dot("bp").Op("+").Id(l.index),
				jen.If(jen.Id(l.addr).Op(">=").Id("i").Dot("sp")).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
			)
		} else {
			result.check = append(result.check,
				jen.If(jen.Id("i").Dot("fr").Dot("bp").Op("+").Id(l.index).Op(">=").Id("i").Dot("sp")).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
			)
			result.body = append(result.body, jen.Id(l.addr).Op(":=").Id("i").Dot("fr").Dot("bp").Op("+").Id(l.index))
		}
		result.body = append(result.body, jen.Id(l.boxed).Op(":=").Id("i").Dot("stack").Index(jen.Id(l.addr)))
	case instr.GLOBAL_GET:
		result.check = append(result.check,
			jen.If(jen.Id(l.index).Op(">=").Len(jen.Id("i").Dot("globals"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
		)
		result.body = append(result.body, jen.Id(l.boxed).Op(":=").Id("i").Dot("globals").Index(jen.Id(l.index)))
	case instr.UPVAL_GET:
		result.check = append(result.check,
			jen.If(jen.Id(l.index).Op(">=").Len(jen.Id("i").Dot("fr").Dot("upvals"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
		)
		result.body = append(result.body, jen.Id(l.boxed).Op(":=").Id("i").Dot("fr").Dot("upvals").Index(jen.Id(l.index)))
	}
	return nil
}

func (l loader) bounds(current step, field string) ([]jen.Code, error) {
	if !l.standalone {
		return l.slotGuard(current)
	}
	condition := jen.Id(l.index).Op(">=").Len(jen.Id("c").Dot(field))
	body := []jen.Code{
		jen.Return(jen.Func().Params(jen.Op("*").Id("Interpreter")).Block(jen.Panic(jen.Id("ErrSegmentationFault")))),
	}
	return []jen.Code{jen.If(condition).Block(body...)}, nil
}

func (l loader) slotGuard(current step) ([]jen.Code, error) {
	name, ok := kindName(current.kind)
	if !ok {
		return nil, fmt.Errorf("unsupported source kind %s", current.kind)
	}
	_, method, ok := slotInfo(current.op)
	if !ok {
		return nil, fmt.Errorf("unsupported slot opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	expected := jen.Qual("github.com/siyul-park/minivm/types", "Kind"+name)
	okName := fmt.Sprintf("ok%d", l.slot)
	return []jen.Code{
		jen.List(jen.Id(l.index), jen.Id(okName)).Op(":=").Id("c").Dot(method).Call(jen.Add(l.pos).Op("+").Lit(1), expected),
		jen.If(jen.Op("!").Id(okName)).Block(reject(l.label)),
	}, nil
}

func (l loader) constant(result *value, current step) error {
	if !l.standalone {
		return l.constantGuard(result, current)
	}
	condition := jen.Id(l.index).Op(">=").Len(jen.Id("c").Dot("constants"))
	result.compile = append(result.compile,
		jen.If(condition).Block(
			jen.Return(jen.Func().Params(jen.Op("*").Id("Interpreter")).Block(jen.Panic(jen.Id("ErrSegmentationFault")))),
		),
		jen.Id(l.boxed).Op(":=").Id("c").Dot("constants").Index(jen.Id(l.index)),
	)
	return nil
}

func (l loader) constantGuard(result *value, current step) error {
	name, ok := kindName(current.kind)
	if !ok {
		return fmt.Errorf("unsupported source kind %s", current.kind)
	}
	expected := jen.Qual("github.com/siyul-park/minivm/types", "Kind"+name)
	okName := fmt.Sprintf("ok%d", l.slot)
	result.compile = append(result.compile,
		jen.List(jen.Id(l.boxed), jen.Id(okName)).Op(":=").Id("c").Dot("constant").Call(jen.Add(l.pos).Op("+").Lit(1), expected),
		jen.If(jen.Op("!").Id(okName)).Block(reject(l.label)),
	)
	if name != "Ref" || current.typ == nil {
		return nil
	}
	guardName := current.typ.Name()
	if guardName == "" {
		return fmt.Errorf("unsupported constant guard %s", current.typ)
	}
	ref := fmt.Sprintf("c%d", l.slot)
	result.compile = append(result.compile,
		jen.Id(ref).Op(":=").Id(l.boxed).Dot("Ref").Call(),
		jen.If(jen.Id(ref).Op("<").Lit(0).Op("||").Id(ref).Op(">=").Len(jen.Id("c").Dot("heap"))).Block(reject(l.label)),
	)
	guard := jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id(ref)).Assert(jen.Qual("github.com/siyul-park/minivm/types", guardName))
	if current.exclude {
		result.compile = append(result.compile, jen.If(guard, jen.Id("ok")).Block(reject(l.label)))
	} else {
		result.compile = append(result.compile, guard, jen.If(jen.Op("!").Id("ok")).Block(reject(l.label)))
		result.object = jen.Id(ref)
		result.typ = current.typ
	}
	return nil
}

// arrayGuard proves, once at threading time, that the local, global, or
// upvalue slot l.index addresses is declared as the concrete typed-array
// element kind current.typ names. A miss rejects the fusion attempt so the
// container's runtime value keeps its own type check in the standalone
// ARRAY_GET handler; it never assumes the declared type from a narrower
// Kind, because ArrayType.Kind is KindRef for every element kind alike.
func (l loader) arrayGuard(result *value, current step) error {
	kind, ok := arrayKind(current.typ)
	if !ok {
		return fmt.Errorf("unsupported array element type %s", current.typ)
	}
	name, ok := fieldKindName(kind)
	if !ok {
		return fmt.Errorf("unsupported array element kind %s", kind)
	}
	field, ok := declaredTypesField(current.op)
	if !ok {
		return fmt.Errorf("unsupported array container opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	expected := jen.Qual("github.com/siyul-park/minivm/types", "Kind"+name)
	declared := fmt.Sprintf("d%d", l.slot)
	okName := fmt.Sprintf("dok%d", l.slot)
	result.compile = append(result.compile,
		jen.List(jen.Id(declared), jen.Id(okName)).Op(":=").Id("c").Dot(field).Index(jen.Id(l.index)).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "ArrayType")),
		jen.If(jen.Op("!").Id(okName).Op("||").Id(declared).Dot("ElemKind").Op("!=").Add(expected)).Block(reject(l.label)),
	)
	result.typ = current.typ
	return nil
}

// structGuard proves, once at threading time, that the local, global, or
// upvalue slot l.index addresses is declared as a concrete *types.StructType,
// and records that declared type so a fused STRUCT_GET consumer can resolve
// each accessed field's static Kind without re-deriving it from the runtime
// heap value. A miss rejects the fusion attempt so the container's runtime
// value keeps its own type check in the standalone STRUCT_GET handler; it
// never assumes the runtime struct shares the declared type's field shape.
func (l loader) structGuard(result *value, current step) error {
	field, ok := declaredTypesField(current.op)
	if !ok {
		return fmt.Errorf("unsupported struct container opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	declared := fmt.Sprintf("d%d", l.slot)
	okName := fmt.Sprintf("dok%d", l.slot)
	result.compile = append(result.compile,
		jen.List(jen.Id(declared), jen.Id(okName)).Op(":=").Id("c").Dot(field).Index(jen.Id(l.index)).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "StructType")),
		jen.If(jen.Op("!").Id(okName)).Block(reject(l.label)),
	)
	result.declared = jen.Id(declared)
	return nil
}

func (l loader) literal(result *value, current step) error {
	if _, ok := kindName(current.kind); !ok {
		return fmt.Errorf("unsupported source kind %s", current.kind)
	}
	if current.boxed {
		result.boxed = jen.Id(l.boxed)
		result.compile = append(result.compile,
			jen.Id(l.boxed).Op(":=").Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(immediate(current.kind, l.pos)),
		)
		return nil
	}
	result.raw = jen.Id(l.raw)
	result.compile = append(result.compile, jen.Id(l.raw).Op(":=").Add(immediate(current.kind, l.pos)))
	switch current.op {
	case instr.I32_CONST:
		result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(jen.Id(l.raw))
	case instr.I64_CONST:
		result.boxed = jen.Id("i").Dot("boxI64").Call(jen.Id(l.raw))
	case instr.F32_CONST:
		result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(jen.Id(l.raw))
	case instr.F64_CONST:
		result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(jen.Id(l.raw))
	}
	return nil
}

func (l loader) finish(result value, current step, indexed bool) (value, error) {
	if current.kind != instr.KindAny && !current.boxed && !current.commit && result.raw == nil {
		result.raw = jen.Id(l.raw)
		result.body = append(result.body, jen.Id(l.raw).Op(":=").Add(borrow(current.kind, result.boxed)))
	}

	if indexed {
		retain := current.kind == instr.KindAny || current.kind.Repr() == instr.KindI64 || current.kind.Repr() == instr.KindRef
		result.push = materialize(result, retain, l.width)
		return result, nil
	}
	if result.room {
		result.push = append(result.push, overflow())
	}
	result.push = append(result.push, result.check...)
	result.push = append(result.push, result.body...)
	if current.op == instr.CONST_GET && current.kind == instr.KindAny {
		result.push = append(result.push,
			jen.If(jen.Id(l.boxed).Dot("Kind").Call().Op("==").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(
				jen.Id("addr").Op(":=").Id(l.boxed).Dot("Ref").Call(),
				jen.If(jen.List(jen.Id("str"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Qual("github.com/siyul-park/minivm/types", "String")), jen.Id("ok")).Block(
					jen.Id(l.boxed).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxRef").Call(jen.Int().Call(jen.Id("i").Dot("intern").Call(jen.String().Call(jen.Id("str"))))),
				).Else().Block(jen.Id("i").Dot("retain").Call(jen.Id("addr"))),
			),
		)
	} else if current.op == instr.CONST_GET {
		result.push = append(result.push, jen.Id("i").Dot("retainBox").Call(result.boxed))
	}
	result.push = append(result.push,
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(result.boxed),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(l.width),
	)
	return result, nil
}

func bind(emit func() jen.Code) lowerer {
	return func(_ *state, current step) (value, error) {
		return value{op: current.op, head: current.op, handler: emit()}, nil
	}
}

func lower(op instr.Opcode) jen.Code {
	context := state{width: width(op), standalone: true}
	result, err := lowerers[op](&context, step{match: match{op: op}, kind: instr.KindAny})
	if err != nil {
		panic(err)
	}
	if result.handler != nil {
		return result.handler
	}
	if len(result.compile) == 0 {
		panic(fmt.Sprintf("no standalone lowering for %s", instr.TypeOf(op).Mnemonic))
	}
	return threaderFunc(result.compile...)
}

// threaderFunc wraps body as the `func(c *threader) func(*Interpreter)`
// shape shared by every lowering entry point.
func threaderFunc(body ...jen.Code) jen.Code {
	return jen.Func().Params(jen.Id("c").Op("*").Id("threader")).Params(
		jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")),
	).Block(body...)
}

func compose(pattern pattern, size int, label string) ([]jen.Code, error) {
	steps, err := resolve(pattern)
	if err != nil {
		return nil, err
	}

	context := state{width: size, label: label}
	var result value
	for _, current := range steps {
		emit := lowerers[current.op]
		if emit == nil {
			return nil, fmt.Errorf("no lowering for %s", instr.TypeOf(current.op).Mnemonic)
		}
		result, err = emit(&context, current)
		if err != nil {
			return nil, err
		}
		context.offset += width(current.op)
	}
	if result.handler != nil || len(result.compile) == 0 {
		consumer := steps[len(steps)-1].op
		return nil, fmt.Errorf("no fusion lowering for %s", instr.TypeOf(consumer).Mnemonic)
	}
	if len(context.stack) != 0 {
		return nil, fmt.Errorf("fusion leaves %d pending values", len(context.stack))
	}
	return result.compile, nil
}

func resolve(pattern pattern) ([]step, error) {
	steps := make([]step, len(pattern))
	for index, current := range pattern {
		steps[index] = step{match: current, kind: instr.KindAny}
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("empty fusion pattern")
	}

	consumerAt := len(steps) - 1
	stored := steps[consumerAt].op == instr.LOCAL_SET
	if stored {
		consumerAt--
	}
	branch := steps[consumerAt].op == instr.BR_IF
	if branch {
		consumerAt--
	}
	if consumerAt < 0 {
		return nil, fmt.Errorf("fusion pattern has no consumer")
	}
	consumer := steps[consumerAt].op
	if consumerAt == 0 {
		if stored {
			if _, ok := numericKind(consumer); !ok {
				return nil, fmt.Errorf("%s cannot feed local.set", instr.TypeOf(consumer).Mnemonic)
			}
			return steps, nil
		}
		if !branch {
			return nil, fmt.Errorf("fusion pattern has no source")
		}
		push := instr.TypeOf(consumer).Push
		if len(push) == 0 || push[len(push)-1].Repr() != instr.KindI32 {
			return nil, fmt.Errorf("%s cannot feed br_if", instr.TypeOf(consumer).Mnemonic)
		}
		steps[0].kind = push[len(push)-1].Repr()
		return steps, nil
	}
	if consumer == instr.ARRAY_GET && consumerAt == 2 {
		kind, ok := arrayKind(steps[0].typ)
		if !ok {
			return nil, fmt.Errorf("array.get cannot resolve element kind")
		}
		steps[0].kind = instr.KindRef
		steps[1].kind = instr.KindI32
		steps[2].kind = kind
		return steps, nil
	}
	if consumer == instr.ARRAY_SET && consumerAt == 3 {
		kind, ok := arrayKind(steps[0].typ)
		if !ok {
			return nil, fmt.Errorf("array.set cannot resolve element kind")
		}
		steps[0].kind = instr.KindRef
		steps[1].kind = instr.KindI32
		steps[2].kind = kind.Repr()
		steps[3].kind = kind.Repr()
		return steps, nil
	}
	if consumer == instr.STRUCT_GET && consumerAt == 2 {
		// The field's Kind depends on the runtime *types.StructType a struct
		// container declares, not on a Go type the pattern can name, so it is
		// resolved during composition instead of here.
		steps[0].kind = instr.KindRef
		steps[1].kind = instr.KindI32
		return steps, nil
	}

	kind, count, ok := input(consumer)
	if !ok {
		return nil, fmt.Errorf("no fusion lowering for %s", instr.TypeOf(consumer).Mnemonic)
	}
	if consumerAt > count {
		return nil, fmt.Errorf("%s accepts at most %d fused sources", instr.TypeOf(consumer).Mnemonic, count)
	}
	boxed := kind.Repr() == instr.KindRef || consumer == instr.I32_XOR || consumer == instr.I32_AND || consumer == instr.I32_OR
	for index := range steps[:consumerAt] {
		steps[index].kind = kind.Repr()
		steps[index].boxed = boxed
		steps[index].commit = traps(consumer)
	}
	return steps, nil
}

func input(op instr.Opcode) (instr.Kind, int, bool) {
	switch op {
	case instr.DROP, instr.REF_IS_NULL:
		return instr.KindRef, 1, true
	case instr.ARRAY_GET, instr.STRUCT_GET:
		return instr.KindI32, 1, true
	case instr.CALL, instr.RETURN_CALL, instr.CLOSURE_NEW:
		return instr.KindRef, 1, true
	}
	pop := instr.TypeOf(op).Pop
	if len(pop) == 0 || pop[0] == instr.KindAny {
		return instr.KindAny, 0, false
	}
	return pop[0], len(pop), true
}

func source(state *state, current step) (value, error) {
	if state.standalone {
		switch current.op {
		case instr.I32_CONST:
			current.kind = instr.KindI32
		case instr.I64_CONST:
			current.kind = instr.KindI64
		case instr.F32_CONST:
			current.kind = instr.KindF32
		case instr.F64_CONST:
			current.kind = instr.KindF64
		}
	}
	result, err := load(current, len(state.stack), state.offset, state.label, state.standalone)
	if err != nil {
		return value{}, err
	}
	if state.standalone {
		if current.op == instr.CONST_GET {
			result.handler = constHandler(current, result)
		} else if field, _, ok := slotInfo(current.op); ok {
			result.handler = slotHandler(current, result, field)
		} else {
			result.handler = handler(current.op, result.compile, result.push)
		}
		return result, nil
	}
	state.stack = append(state.stack, result)
	return result, nil
}

func slotHandler(current step, input value, field string) jen.Code {
	compile := append([]jen.Code(nil), input.compile...)
	scalar := materialize(input, false, width(current.op))
	owned := materialize(input, true, width(current.op))
	choose := jen.Switch(jen.Id("c").Dot(field).Index(jen.Id("i0")).Dot("Repr").Call()).Block(
		jen.Case(
			jen.Qual("github.com/siyul-park/minivm/types", "KindI32"),
			jen.Qual("github.com/siyul-park/minivm/types", "KindF32"),
			jen.Qual("github.com/siyul-park/minivm/types", "KindF64"),
		).Block(jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(scalar...))),
	)
	if current.op == instr.LOCAL_GET {
		compile = append(compile, choose)
	} else {
		compile = append(compile,
			jen.If(jen.Id("i0").Op("<").Len(jen.Id("c").Dot(field))).Block(choose),
		)
	}
	compile = append(compile,
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(owned...)),
	)
	return threaderFunc(compile...)
}

func constHandler(current step, input value) jen.Code {
	compile := append([]jen.Code(nil), input.compile...)
	boxed := input.boxed
	scalar := materialize(input, false, width(current.op))
	var owned []jen.Code
	if input.room {
		owned = append(owned, overflow())
	}
	owned = append(owned, input.check...)
	owned = append(owned, input.body...)
	owned = append(owned,
		jen.Id("i").Dot("retain").Call(jen.Id("addr")),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(input.boxed),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(width(current.op)),
	)
	compile = append(compile,
		jen.Switch(jen.Add(boxed).Dot("Kind").Call()).Block(
			jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(
				jen.Id("addr").Op(":=").Add(boxed).Dot("Ref").Call(),
				jen.If(
					jen.List(jen.Id("str"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Qual("github.com/siyul-park/minivm/types", "String")),
					jen.Id("ok"),
				).Block(
					jen.Id("text").Op(":=").String().Call(jen.Id("str")),
					jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(
						jen.If(jen.Id("i").Dot("sp").Op("==").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
						jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxRef").Call(jen.Int().Call(jen.Id("i").Dot("intern").Call(jen.Id("text")))),
						jen.Id("i").Dot("sp").Op("++"),
						jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(width(current.op)),
					)),
				),
				jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(owned...)),
			),
		),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(scalar...)),
	)
	return threaderFunc(compile...)
}

func materialize(input value, retain bool, advance int) []jen.Code {
	var body []jen.Code
	if input.room {
		body = append(body, overflow())
	}
	body = append(body, input.check...)
	body = append(body, input.body...)
	if retain {
		body = append(body, jen.Id("i").Dot("retainBox").Call(input.boxed))
	}
	return append(body,
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(input.boxed),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
	)
}

func ref(state *state, current step) (value, error) {
	switch current.op {
	case instr.REF_NULL, instr.DUP:
		return produce(state, current)
	case instr.DROP, instr.REF_IS_NULL:
		return consume(state, current)
	default:
		return value{}, fmt.Errorf("unsupported ref opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
}

func produce(state *state, current step) (value, error) {
	result := value{op: current.op, head: current.op}
	switch current.op {
	case instr.REF_NULL:
		result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxedNull")
		result.check = append(result.check, overflow())
		result.push = append(result.push, result.check...)
		result.push = append(result.push,
			jen.Id("i").Dot("retain").Call(jen.Lit(0)),
			jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(result.boxed),
			jen.Id("i").Dot("sp").Op("++"),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"),
		)
	case instr.DUP:
		result.boxed = jen.Id("value")
		result.check = append(result.check,
			jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			overflow(),
		)
		result.body = append(result.body,
			jen.Id("value").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
		)
		result.push = append(result.push, result.check...)
		result.push = append(result.push, result.body...)
		result.push = append(result.push,
			jen.Id("i").Dot("retainBox").Call(jen.Id("value")),
			jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("value"),
			jen.Id("i").Dot("sp").Op("++"),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"),
		)
	}
	if state.standalone {
		result.handler = handler(current.op, result.compile, result.push)
		return result, nil
	}
	state.stack = append(state.stack, result)
	return result, nil
}

func consume(state *state, current step) (value, error) {
	if state.standalone {
		state.stack = []value{{
			op:       current.op,
			head:     current.op,
			boxed:    jen.Id("value"),
			resident: true,
			check: []jen.Code{
				jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			},
			body: []jen.Code{
				jen.Id("value").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
			},
			drop: []jen.Code{
				jen.Id("i").Dot("releaseBox").Call(jen.Id("value")),
			},
		}}
	}
	if len(state.stack) == 0 {
		return value{}, fmt.Errorf("%s needs one pending value", instr.TypeOf(current.op).Mnemonic)
	}
	input := state.stack[len(state.stack)-1]
	compile := append([]jen.Code(nil), input.compile...)
	body := append([]jen.Code(nil), input.check...)

	switch current.op {
	case instr.DROP:
		if input.resident {
			body = append(body, input.body...)
			body = append(body, input.drop...)
			body = append(body, jen.Id("i").Dot("sp").Op("--"))
		}
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width))
	case instr.REF_IS_NULL:
		if !input.resident && input.room {
			body = append(body, overflow())
		}
		body = append(body, input.body...)
		condition := jen.Add(input.boxed).Dot("Ref").Call().Op("==").Lit(0)
		if !state.standalone && state.offset+width(current.op) < state.width {
			result := value{op: current.op, head: input.head, compile: compile, check: input.check, body: input.body, raw: condition}
			state.stack = []value{result}
			return result, nil
		}
		if input.resident {
			body = append(body,
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(condition),
			)
			body = append(body, input.drop...)
		} else {
			body = append(body,
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(condition),
				jen.Id("i").Dot("sp").Op("++"),
			)
			body = append(body, input.drop...)
		}
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width))
	}

	state.stack = nil
	if state.standalone {
		return value{op: current.op, head: current.op, handler: handler(current.op, compile, body)}, nil
	}
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(input.head)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return value{op: current.op, head: input.head, compile: compile}, nil
}

func index(state *state, current step) (value, error) {
	if state.standalone {
		body := []jen.Code{
			jen.If(jen.Id("i").Dot("sp").Op("<").Lit(2)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.Id("index").Op(":=").Int().Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Dot("I32").Call()),
			jen.Id("i").Dot("sp").Op("--"),
		}
		body = append(body, lookup(current.op, jen.Id("index"), width(current.op))...)
		return value{op: current.op, head: current.op, handler: handler(current.op, nil, body)}, nil
	}
	if len(state.stack) == 2 && current.op == instr.ARRAY_GET && state.stack[0].object != nil {
		container := state.stack[0]
		index := state.stack[1]
		compile := append([]jen.Code(nil), container.compile...)
		compile = append(compile, index.compile...)
		body := []jen.Code{overflow()}
		body = append(body, index.check...)
		body = append(body, index.body...)
		body = append(body,
			jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.object).Assert(typeName(container.typ)),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("at").Op(":=").Int().Call(index.raw),
			bounds(jen.Id("at"), jen.Lit(1), jen.Len(jen.Id("array"))),
			jen.Id("result").Op(":=").Add(boxArray(current.kind, jen.Id("array"), jen.Id("at"))),
			jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("result"),
			jen.Id("i").Dot("sp").Op("++"),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
		)
		compile = append(compile,
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
		)
		state.stack = nil
		return value{op: current.op, head: container.head, compile: compile}, nil
	}
	if len(state.stack) == 2 && current.op == instr.ARRAY_GET && isContainerSource(state.stack[0].op) && state.stack[0].typ != nil {
		container := state.stack[0]
		index := state.stack[1]
		compile := append([]jen.Code(nil), container.compile...)
		compile = append(compile, index.compile...)
		body := []jen.Code{overflow()}
		body = append(body, container.check...)
		body = append(body, container.body...)
		body = append(body, index.check...)
		body = append(body, index.body...)
		body = append(body,
			jen.If(jen.Add(container.boxed).Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("at").Op(":=").Int().Call(index.raw),
		)
		// Each arm below pushes its own result and returns immediately: no
		// variable is live across the two heap-type assertions, so the
		// success path stays straight-line with no join point.
		tail := func(result jen.Code) []jen.Code {
			return []jen.Code{
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(result),
				jen.Id("i").Dot("sp").Op("++"),
				jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
				jen.Return(),
			}
		}
		// arrayGuard only proves the container's declared element kind, never
		// the runtime value's concrete representation: LOCAL_SET, GLOBAL_SET,
		// and a closure's captured upvalue all assign into a declared slot
		// without re-checking the assigned value's concrete representation
		// against it (e.g. ARRAY_NEW_DEFAULT's ref-element path stores
		// through a differently-declared alias, or a store from a source
		// whose static type verification could not pin down leaves a
		// concrete TypedArray[U] with U != T in the slot), so a miss on the
		// specialized TypedArray[T] assertion falls back to
		// (*Interpreter).arrayElem, the same generic reader the unfused
		// handler calls unconditionally — every other TypedArray[_]
		// representation and the generic *types.Array alike — instead of
		// trapping a case the unfused handler accepts. This arm still
		// returns on its own success path, so no variable is live across it
		// and the fallback below.
		body = append(body, jen.If(
			jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.raw).Assert(typeName(container.typ)),
			jen.Id("ok"),
		).Block(append([]jen.Code{
			bounds(jen.Id("at"), jen.Lit(1), jen.Len(jen.Id("array"))),
		}, tail(boxArray(current.kind, jen.Id("array"), jen.Id("at")))...)...))
		body = append(body, tail(jen.Id("i").Dot("arrayElem").Call(container.raw, jen.Id("at")))...)
		compile = append(compile,
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
		)
		state.stack = nil
		return value{op: current.op, head: container.head, compile: compile}, nil
	}
	if len(state.stack) == 2 && current.op == instr.STRUCT_GET && isContainerSource(state.stack[0].op) && state.stack[0].declared != nil {
		return structIndex(state, current)
	}
	if len(state.stack) != 1 {
		return value{}, fmt.Errorf("%s needs one constant index", instr.TypeOf(current.op).Mnemonic)
	}
	index := state.stack[0]
	compile := append([]jen.Code(nil), index.compile...)
	body := append([]jen.Code(nil), index.check...)
	body = append(body, index.body...)
	body = append(body, lookup(current.op, jen.Int().Call(index.raw), state.width)...)
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(index.head)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	state.stack = nil
	return value{op: current.op, head: index.head, compile: compile}, nil
}

// structIndex fuses STRUCT_GET onto a LOCAL_GET, GLOBAL_GET, or UPVAL_GET
// container whose declared type is a concrete *types.StructType, with the
// field index a compile-time constant. A struct field's Kind depends on
// which StructType the container declares, not on any Go type the catalog
// can name ahead of time, so the switch over Kind runs once here at
// threading time instead of once per execution: it selects one specialized
// runtime closure per Kind, and that closure boxes the field directly with
// no switch of its own. Every runtime guard the standalone STRUCT_GET
// handler performs — ref kind, heap value type, field bounds, and the
// runtime field's actual Kind — still runs on every execution in the same
// order, because the declared type only proves what to specialize for, never
// what the runtime value actually holds; a mismatch on any guard, or a field
// index outside the declared type's own fields, rejects the fusion or traps
// exactly as the unfused sequence would.
func structIndex(state *state, current step) (value, error) {
	container := state.stack[0]
	idx := state.stack[1]
	compile := append([]jen.Code(nil), container.compile...)
	compile = append(compile, idx.compile...)
	compile = append(compile, idx.check...)
	compile = append(compile, idx.body...)
	compile = append(compile,
		jen.Id("at").Op(":=").Int().Call(idx.raw),
		jen.If(
			jen.Id("at").Op("<").Lit(0).Op("||").Id("at").Op(">=").Len(jen.Add(container.declared).Dot("Fields")),
		).Block(reject(state.label)),
	)

	kinds := []instr.Kind{instr.KindI1, instr.KindI8, instr.KindI32, instr.KindI64, instr.KindF32, instr.KindF64, instr.KindRef}
	cases := make([]jen.Code, 0, len(kinds)+1)
	for _, kind := range kinds {
		name, ok := fieldKindName(kind)
		if !ok {
			return value{}, fmt.Errorf("unsupported struct field kind %s", kind)
		}
		body := []jen.Code{overflow()}
		body = append(body, container.check...)
		body = append(body, container.body...)
		body = append(body,
			jen.If(jen.Add(container.boxed).Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		)
		// Each arm below pushes its own result and returns immediately: no
		// variable is live across the two heap-type assertions, so the
		// success path stays straight-line with no join point.
		tail := func(result jen.Code) []jen.Code {
			return []jen.Code{
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(result),
				jen.Id("i").Dot("sp").Op("++"),
				jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
				jen.Return(),
			}
		}
		structBody := []jen.Code{
			jen.If(jen.Id("at").Op(">=").Len(jen.Id("value").Dot("Typ").Dot("Fields"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
			jen.If(jen.Id("value").Dot("Typ").Dot("Fields").Index(jen.Id("at")).Dot("Kind").Op("!=").Qual("github.com/siyul-park/minivm/types", "Kind"+name)).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("result").Op(":=").Add(structFieldBox(kind, jen.Id("value").Dot("Data").Index(jen.Id("at")))),
		}
		if kind == instr.KindRef {
			structBody = append(structBody, jen.Id("i").Dot("retainBox").Call(jen.Id("result")))
		}
		structBody = append(structBody, tail(jen.Id("result"))...)
		// The declared *types.StructType only proves what to specialize for,
		// never the runtime value's concrete representation: the same local,
		// global, or upvalue slot the unfused STRUCT_GET handler would read
		// off a *HostObject can reach a fused STRUCT_GET too, so a miss on
		// the specialized *types.Struct assertion falls back to
		// (*Interpreter).structField, the same generic reader the unfused
		// handler calls unconditionally, instead of trapping a case it
		// accepts.
		body = append(body, jen.If(
			jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.raw).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Struct")),
			jen.Id("ok"),
		).Block(structBody...))
		body = append(body, tail(jen.Id("i").Dot("structField").Call(container.raw, jen.Id("at")))...)
		cases = append(cases, jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "Kind"+name)).Block(
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
		))
	}
	cases = append(cases, jen.Default().Block(reject(state.label)))

	compile = append(compile,
		jen.Switch(jen.Add(container.declared).Dot("Fields").Index(jen.Id("at")).Dot("Kind")).Block(cases...),
	)
	state.stack = nil
	return value{op: current.op, head: container.head, compile: compile}, nil
}

func call(state *state, current step) (value, error) {
	if state.standalone {
		body, err := dynamicCall(current.op)
		if err != nil {
			return value{}, err
		}
		return value{op: current.op, head: current.op, handler: handler(current.op, nil, body)}, nil
	}
	if len(state.stack) != 1 || state.stack[0].op != instr.CONST_GET {
		return value{}, fmt.Errorf("%s needs one constant target", instr.TypeOf(current.op).Mnemonic)
	}
	callee := state.stack[0]
	compile := append([]jen.Code(nil), callee.compile...)
	compile = append(compile,
		jen.Id("addr").Op(":=").Add(callee.boxed).Dot("Ref").Call(),
		jen.If(jen.Id("addr").Op("<").Lit(0).Op("||").Id("addr").Op(">=").Len(jen.Id("c").Dot("heap"))).Block(reject(state.label)),
	)
	switch current.op {
	case instr.CALL:
		compile = append(compile, dispatch(false, state.label, state.width))
	case instr.RETURN_CALL:
		compile = append(compile, dispatch(true, state.label, state.width))
	case instr.CLOSURE_NEW:
		compile = append(compile, closureNew(state.label, state.width))
	default:
		return value{}, fmt.Errorf("unsupported call opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	state.stack = nil
	return value{op: current.op, head: callee.head, compile: compile}, nil
}

func dynamicCall(op instr.Opcode) ([]jen.Code, error) {
	prefix := []jen.Code{
		jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("addr").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Dot("Ref").Call(),
	}
	if op == instr.CLOSURE_NEW {
		body := append(prefix,
			jen.List(jen.Id("fn"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("captures").Op(":=").Len(jen.Id("fn").Dot("Captures")),
		)
		body = append(body, create(1, false, 1)...)
		return body, nil
	}
	if op != instr.CALL && op != instr.RETURN_CALL {
		return nil, fmt.Errorf("unsupported call opcode %s", instr.TypeOf(op).Mnemonic)
	}

	tail := op == instr.RETURN_CALL
	functionTarget := target{code: jen.Id("addr"), addr: jen.Id("addr"), upvals: jen.Nil(), ref: jen.Id("addr")}
	closureTarget := target{code: jen.Id("fn").Dot("Fn"), addr: jen.Int().Parens(jen.Id("fn").Dot("Fn")), upvals: jen.Id("fn").Dot("Upvals"), ref: jen.Id("addr")}
	var function, closure []jen.Code
	if tail {
		functionBody := []jen.Code{
			jen.Id("code").Op(":=").Id("addr"),
			jen.Id("ref").Op(":=").Id("addr"),
			jen.Var().Id("upvals").Index().Qual("github.com/siyul-park/minivm/types", "Boxed"),
			jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
			jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
			jen.Id("locals").Op(":=").Len(jen.Id("fn").Dot("Locals")),
		}
		functionBody = append(functionBody, replace(
			target{code: jen.Id("code"), addr: jen.Id("code"), upvals: jen.Id("upvals"), ref: jen.Id("ref")},
			1, true, 1, "inlineTail2",
		)...)
		function = []jen.Code{jen.Block(functionBody...)}

		closureBody := []jen.Code{
			jen.Id("code").Op(":=").Int().Call(jen.Id("fn").Dot("Fn")),
			jen.Id("ref").Op(":=").Id("addr"),
			jen.Id("upvals").Op(":=").Id("fn").Dot("Upvals"),
			jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
			jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
			jen.Id("locals").Op(":=").Len(jen.Id("tmpl").Dot("Locals")),
		}
		closureBody = append(closureBody, replace(
			target{code: jen.Id("code"), addr: jen.Id("code"), upvals: jen.Id("upvals"), ref: jen.Id("ref")},
			1, true, 1, "inlineTail3",
		)...)
		closure = []jen.Code{
			jen.List(jen.Id("tmpl"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("fn").Dot("Fn")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Block(closureBody...),
		}
	} else {
		function = []jen.Code{
			frameOverflow(),
			jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
			jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
			jen.Id("locals").Op(":=").Len(jen.Id("fn").Dot("Locals")),
		}
		function = append(function, frame(functionTarget, 1, true, 1, jen.Id("addr"))...)
		closure = []jen.Code{
			frameOverflow(),
			jen.List(jen.Id("tmpl"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("fn").Dot("Fn")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
			jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
			jen.Id("locals").Op(":=").Len(jen.Id("tmpl").Dot("Locals")),
		}
		closure = append(closure, frame(closureTarget, 1, true, 1, jen.Int().Parens(jen.Id("fn").Dot("Fn")))...)
	}

	hostCore := []jen.Code{
		jen.Id("fn").Op(":=").Id("fn"),
		jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
	}
	hostCore = append(hostCore, invoke(1, 1, tail)...)
	host := []jen.Code{jen.Block(hostCore...)}
	if tail {
		host = append(host, jen.If(jen.Id("i").Dot("fp").Op(">").Lit(1)).Block(retire(nil)...))
	}
	body := append(prefix, jen.Switch(jen.Id("fn").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")).Block(function...),
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Closure")).Block(closure...),
		jen.Case(jen.Op("*").Id("HostFunction")).Block(host...),
		jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
	))
	return body, nil
}

func scalar(state *state, current step) (value, error) {
	head := current.op
	if len(state.stack) > 0 {
		head = state.stack[0].head
	}
	if !state.standalone && state.offset+width(current.op) < state.width {
		result := value{op: current.op, head: head}
		state.stack = append(state.stack, result)
		return result, nil
	}
	body, err := numeric(current.op, state.stack, state.width, state.label, false, nil)
	if err != nil {
		return value{}, err
	}
	state.stack = nil
	return value{op: current.op, head: head, compile: body}, nil
}

func store(state *state, current step) (value, error) {
	if state.standalone {
		return value{op: current.op, head: current.op, handler: localSet()}, nil
	}
	if len(state.stack) == 0 {
		return value{}, fmt.Errorf("%s needs one pending value", instr.TypeOf(current.op).Mnemonic)
	}
	consumer := state.stack[len(state.stack)-1]
	if _, ok := numericKind(consumer.op); !ok {
		return value{}, fmt.Errorf("%s cannot store %s", instr.TypeOf(current.op).Mnemonic, instr.TypeOf(consumer.op).Mnemonic)
	}
	result := instr.TypeOf(consumer.op).Push[0].Repr()
	compile := []jen.Code{
		jen.List(jen.Id("dst"), jen.Id("dstOK")).Op(":=").Id("c").Dot("local").Call(
			add(jen.Id("start"), state.offset+1),
			jen.Qual("github.com/siyul-park/minivm/types", "Kind"+mustKindName(result)),
		),
		jen.If(jen.Op("!").Id("dstOK")).Block(reject(state.label)),
	}
	body, err := numeric(consumer.op, state.stack[:len(state.stack)-1], state.width, state.label, false, jen.Id("dst"))
	if err != nil {
		return value{}, err
	}
	state.stack = nil
	return value{op: current.op, head: consumer.head, compile: append(compile, body...)}, nil
}

func branch(state *state, current step) (value, error) {
	if state.standalone {
		compile := []jen.Code{
			jen.Id("offset").Op(":=").Qual("github.com/siyul-park/minivm/instr", "ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("c").Dot("ip").Op("+").Lit(1)),
		}
		body := []jen.Code{
			jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		}
		condition := jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Dot("I32").Call().Op("!=").Lit(0)
		body = append(body, jump(condition, 1, width(current.op))...)
		return value{op: current.op, head: current.op, handler: handler(current.op, compile, body)}, nil
	}
	if len(state.stack) == 0 {
		return value{}, fmt.Errorf("%s needs one pending condition", instr.TypeOf(current.op).Mnemonic)
	}
	consumer := state.stack[len(state.stack)-1]
	if _, ok := arity(consumer.op); ok {
		body, err := numeric(consumer.op, state.stack[:len(state.stack)-1], state.width, state.label, true, nil)
		if err != nil {
			return value{}, err
		}
		state.stack = nil
		return value{op: current.op, head: consumer.head, compile: body}, nil
	}
	if consumer.raw == nil {
		return value{}, fmt.Errorf("%s has no branch condition", instr.TypeOf(consumer.op).Mnemonic)
	}
	condition := consumer.raw
	if consumer.op == instr.I32_CONST {
		condition = jen.Add(condition).Op("!=").Lit(0)
	}
	compile := append([]jen.Code(nil), consumer.compile...)
	body := append([]jen.Code(nil), consumer.check...)
	body = append(body, consumer.body...)
	body = append(body, jump(condition, 0, state.width)...)
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(consumer.head)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	state.stack = nil
	return value{op: current.op, head: consumer.head, compile: compile}, nil
}

func handler(op instr.Opcode, compile, body []jen.Code) jen.Code {
	code := append([]jen.Code(nil), compile...)
	code = append(code,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(op)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return threaderFunc(code...)
}

func dispatch(tail bool, label string, advance int) jen.Code {
	return jen.Switch(jen.Id("fn").Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")).Block(direct(tail, label, advance)...),
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Closure")).Block(closure(tail, label, advance)...),
		jen.Case(jen.Op("*").Id("HostFunction")).Block(host(tail, advance)...),
		jen.Default().Block(reject(label)),
	)
}

func direct(tail bool, label string, advance int) []jen.Code {
	guard := jen.If(jen.Id("addr").Op("<").Len(jen.Id("c").Dot("coros")).Op("&&").Id("c").Dot("coros").Index(jen.Id("addr"))).Block(reject(label))
	callee := target{code: jen.Id("addr"), addr: jen.Id("addr"), upvals: jen.Nil(), ref: jen.Id("addr")}
	if tail {
		return append([]jen.Code{guard}, reuse(callee, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("fn").Dot("Locals")), advance)...)
	}
	return append([]jen.Code{guard}, enter(callee, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("fn").Dot("Locals")), advance)...)
}

func closure(tail bool, label string, advance int) []jen.Code {
	preflight := []jen.Code{
		jen.Id("tmpl").Op(",").Id("ok").Op(":=").Id("c").Dot("heap").Index(jen.Id("fn").Dot("Fn")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
		jen.If(jen.Op("!").Id("ok")).Block(reject(label)),
		jen.If(jen.Int().Parens(jen.Id("fn").Dot("Fn")).Op("<").Len(jen.Id("c").Dot("coros")).Op("&&").Id("c").Dot("coros").Index(jen.Id("fn").Dot("Fn"))).Block(reject(label)),
	}
	callee := target{code: jen.Id("fn").Dot("Fn"), addr: jen.Int().Parens(jen.Id("fn").Dot("Fn")), upvals: jen.Id("fn").Dot("Upvals"), ref: jen.Id("addr")}
	if tail {
		return append(preflight, reuse(callee, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("tmpl").Dot("Locals")), advance)...)
	}
	return append(preflight, enter(callee, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("tmpl").Dot("Locals")), advance)...)
}

func enter(callee target, typ, locals jen.Code, advance int) []jen.Code {
	return []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Add(typ).Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Add(typ).Dot("Returns")),
		jen.Id("locals").Op(":=").Add(locals),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(frame(callee, 0, false, advance, nil)...)),
	}
}

func frame(callee target, targetSlots int, releaseTarget bool, advance int, coroutine jen.Code) []jen.Code {
	body := []jen.Code{}
	if targetSlots == 0 {
		body = append(body, overflow())
	}
	if targetSlots == 0 {
		body = append(body, frameOverflow())
	}
	if targetSlots == 1 {
		body = append(body,
			jen.If(jen.Id("i").Dot("sp").Op("<=").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("i").Dot("sp").Op("+").Id("locals").Op("-").Lit(1).Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
			jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(jen.Id("i").Dot("sp").Op("-").Lit(1), jen.Id("i").Dot("sp").Op("+").Id("locals").Op("-").Lit(1))),
		)
	} else {
		body = append(body,
			jen.If(jen.Id("i").Dot("sp").Op("<").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("i").Dot("sp").Op("+").Id("locals").Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
			jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(jen.Id("i").Dot("sp"), jen.Id("i").Dot("sp").Op("+").Id("locals"))),
		)
	}
	body = append(body,
		jen.Id("f").Op(":=").Op("&").Id("i").Dot("frames").Index(jen.Id("i").Dot("fp")),
		jen.Id("f").Dot("code").Op("=").Id("i").Dot("code").Index(jen.Add(callee.code)),
		jen.Id("f").Dot("upvals").Op("=").Add(callee.upvals),
		jen.Id("f").Dot("addr").Op("=").Add(callee.addr),
		jen.Id("f").Dot("ref").Op("=").Add(callee.ref),
		jen.Id("f").Dot("ip").Op("=").Lit(0),
		jen.Id("f").Dot("bp").Op("=").Add(adjust(jen.Id("i").Dot("sp").Op("-").Id("params"), -targetSlots)),
		jen.Id("f").Dot("returns").Op("=").Id("returns"),
		jen.Id("f").Dot("release").Op("=").Add(jen.Lit(releaseTarget)),
		jen.Id("f").Dot("coro").Op("=").Lit(0),
	)
	if coroutine != nil {
		body = append(body, jen.If(jen.Add(coroutine).Op("<").Len(jen.Id("i").Dot("coros")).Op("&&").Id("i").Dot("coros").Index(jen.Add(coroutine))).Block(
			jen.Id("f").Dot("coro").Op("=").Id("i").Dot("alloc").Call(jen.Op("&").Id("coroutine").Values(jen.Dict{jen.Id("typ"): jen.Id("fn").Dot("Typ")})),
		))
	}
	body = append(body,
		jen.Id("i").Dot("sp").Op("=").Id("f").Dot("bp").Op("+").Id("params").Op("+").Id("locals"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
		jen.Id("i").Dot("fp").Op("++"),
		jen.Id("i").Dot("fr").Op("=").Id("f"),
	)
	return body
}

func reuse(callee target, typ, locals jen.Code, advance int) []jen.Code {
	return []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Add(typ).Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Add(typ).Dot("Returns")),
		jen.Id("locals").Op(":=").Add(locals),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(replace(callee, 0, false, advance, "")...)),
	}
}

func replace(callee target, targetSlots int, releaseTarget bool, advance int, label string) []jen.Code {
	if targetSlots == 1 {
		body := []jen.Code{
			jen.If(jen.Id("i").Dot("sp").Op("<=").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.Var().Id("f").Op("*").Id("frame"),
			jen.Var().Id("base").Int(),
			jen.If(jen.Id("i").Dot("fp").Op("==").Lit(1)).Block(
				frameOverflow(),
				jen.If(jen.Id("i").Dot("sp").Op("+").Id("locals").Op("-").Lit(1).Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
				jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(jen.Id("i").Dot("sp").Op("-").Lit(1), jen.Id("i").Dot("sp").Op("+").Id("locals").Op("-").Lit(1))),
				jen.Id("f").Op(":=").Op("&").Id("i").Dot("frames").Index(jen.Id("i").Dot("fp")),
				jen.Id("f").Dot("code").Op("=").Id("i").Dot("code").Index(jen.Add(callee.code)),
				jen.Id("f").Dot("upvals").Op("=").Add(callee.upvals),
				jen.Id("f").Dot("addr").Op("=").Add(callee.addr),
				jen.Id("f").Dot("ref").Op("=").Add(callee.ref),
				jen.Id("f").Dot("ip").Op("=").Lit(0),
				jen.Id("f").Dot("bp").Op("=").Id("i").Dot("sp").Op("-").Id("params").Op("-").Lit(1),
				jen.Id("f").Dot("returns").Op("=").Id("returns"),
				jen.Id("f").Dot("release").Op("=").Add(jen.Lit(releaseTarget)),
				jen.Id("f").Dot("coro").Op("=").Lit(0),
				jen.Id("i").Dot("sp").Op("=").Id("f").Dot("bp").Op("+").Id("params").Op("+").Id("locals"),
				jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
				jen.Id("i").Dot("fp").Op("++"),
				jen.Id("i").Dot("fr").Op("=").Id("f"),
				jen.Goto().Id(label),
			),
			jen.Id("f").Op("=").Id("i").Dot("fr"),
			jen.Id("base").Op("=").Id("f").Dot("bp"),
			jen.If(jen.Id("base").Op("+").Id("params").Op("+").Id("locals").Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
			jen.Copy(jen.Id("i").Dot("stack").Index(jen.Id("base").Op(":").Id("base").Op("+").Id("params")), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("params").Op("-").Lit(1).Op(":").Id("i").Dot("sp").Op("-").Lit(1))),
			jen.If(jen.Id("f").Dot("release")).Block(jen.Id("i").Dot("release").Call(jen.Id("f").Dot("ref"))),
			jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(jen.Id("base").Op("+").Id("params"), jen.Id("base").Op("+").Id("params").Op("+").Id("locals"))),
			jen.Id("f").Dot("code").Op("=").Id("i").Dot("code").Index(jen.Add(callee.code)),
			jen.Id("f").Dot("upvals").Op("=").Add(callee.upvals),
			jen.Id("f").Dot("addr").Op("=").Add(callee.addr),
			jen.Id("f").Dot("ref").Op("=").Add(callee.ref),
			jen.Id("f").Dot("ip").Op("=").Lit(0),
			jen.Id("f").Dot("bp").Op("=").Id("base"),
			jen.Id("f").Dot("returns").Op("=").Id("returns"),
			jen.Id("f").Dot("release").Op("=").Add(jen.Lit(releaseTarget)),
			jen.Id("i").Dot("sp").Op("=").Id("base").Op("+").Id("params").Op("+").Id("locals"),
			jen.Id(label).Op(":").Add(jen.Null()),
		}
		return body
	}

	body := []jen.Code{overflow()}
	body = append(body,
		jen.If(jen.Id("i").Dot("sp").Op("<").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.If(jen.Id("i").Dot("fp").Op("==").Lit(1)).Block(
			frameOverflow(),
			jen.If(jen.Id("i").Dot("sp").Op("+").Id("locals").Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
			jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(jen.Id("i").Dot("sp"), jen.Id("i").Dot("sp").Op("+").Id("locals"))),
			jen.Id("f").Op(":=").Op("&").Id("i").Dot("frames").Index(jen.Id("i").Dot("fp")),
			jen.Id("f").Dot("code").Op("=").Id("i").Dot("code").Index(jen.Add(callee.code)),
			jen.Id("f").Dot("upvals").Op("=").Add(callee.upvals),
			jen.Id("f").Dot("addr").Op("=").Add(callee.addr),
			jen.Id("f").Dot("ref").Op("=").Add(callee.ref),
			jen.Id("f").Dot("ip").Op("=").Lit(0),
			jen.Id("f").Dot("bp").Op("=").Id("i").Dot("sp").Op("-").Id("params"),
			jen.Id("f").Dot("returns").Op("=").Id("returns"),
			jen.Id("f").Dot("release").Op("=").False(),
			jen.Id("f").Dot("coro").Op("=").Lit(0),
			jen.Id("i").Dot("sp").Op("=").Id("f").Dot("bp").Op("+").Id("params").Op("+").Id("locals"),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
			jen.Id("i").Dot("fp").Op("++"),
			jen.Id("i").Dot("fr").Op("=").Id("f"),
			jen.Return(),
		),
		jen.Id("f").Op(":=").Id("i").Dot("fr"),
		jen.Id("base").Op(":=").Id("f").Dot("bp"),
		jen.If(jen.Id("base").Op("+").Id("params").Op("+").Id("locals").Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
		jen.Copy(jen.Id("i").Dot("stack").Index(jen.Id("base").Op(":").Id("base").Op("+").Id("params")), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("params").Op(":").Id("i").Dot("sp"))),
		jen.If(jen.Id("f").Dot("release")).Block(jen.Id("i").Dot("release").Call(jen.Id("f").Dot("ref"))),
		jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(jen.Id("base").Op("+").Id("params"), jen.Id("base").Op("+").Id("params").Op("+").Id("locals"))),
		jen.Id("f").Dot("code").Op("=").Id("i").Dot("code").Index(jen.Add(callee.code)),
		jen.Id("f").Dot("upvals").Op("=").Add(callee.upvals),
		jen.Id("f").Dot("addr").Op("=").Add(callee.addr),
		jen.Id("f").Dot("ref").Op("=").Add(callee.ref),
		jen.Id("f").Dot("ip").Op("=").Lit(0),
		jen.Id("f").Dot("returns").Op("=").Id("returns"),
		jen.Id("f").Dot("release").Op("=").False(),
		jen.Id("i").Dot("sp").Op("=").Id("base").Op("+").Id("params").Op("+").Id("locals"),
	)
	return body
}

func frameOverflow() jen.Code {
	return jen.If(jen.Id("i").Dot("fp").Op("==").Len(jen.Id("i").Dot("frames"))).Block(jen.Panic(jen.Id("ErrFrameOverflow")))
}

func host(tail bool, advance int) []jen.Code {
	return []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(invoke(0, advance, tail)...)),
	}
}

func invoke(targetSlots, advance int, tail bool) []jen.Code {
	body := []jen.Code{}
	if targetSlots == 0 {
		body = append(body, overflow())
	}
	if targetSlots == 1 {
		body = append(body, jen.If(jen.Id("i").Dot("sp").Op("<=").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))))
	} else {
		body = append(body, jen.If(jen.Id("i").Dot("sp").Op("<").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))))
	}
	body = append(body,
		jen.If(adjust(jen.Id("i").Dot("sp").Op("+").Id("returns").Op("-").Id("params"), -targetSlots).Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
		jen.Id("args").Op(":=").Id("i").Dot("stack").Index(
			adjust(jen.Id("i").Dot("sp").Op("-").Id("params"), -targetSlots).Op(":").Add(adjust(jen.Id("i").Dot("sp"), -targetSlots)),
		),
		jen.Id("out").Op(",").Id("err").Op(":=").Id("fn").Dot("Fn").Call(jen.Id("i"), jen.Id("args")),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
		release(jen.Id("args"), jen.Id("out")),
	)
	if targetSlots > 0 {
		body = append(body, release(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(targetSlots).Op(":").Id("i").Dot("sp")), jen.Id("out")))
	}
	body = append(body,
		jen.Id("i").Dot("sp").Op("+=").Add(adjust(jen.Id("returns").Op("-").Id("params"), -targetSlots)),
		jen.Copy(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("returns").Op(":").Id("i").Dot("sp")), jen.Id("out")),
	)
	if tail {
		if targetSlots == 1 {
			body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance))
		} else {
			body = append(body, jen.If(jen.Id("i").Dot("fp").Op(">").Lit(1)).Block(retire(nil)...))
		}
	} else {
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance))
	}
	return body
}

func closureNew(label string, advance int) jen.Code {
	return jen.Switch(jen.Id("fn").Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")).Block(
			jen.Id("captures").Op(":=").Len(jen.Id("fn").Dot("Captures")),
			jen.Id("c").Dot("ip").Op("+=").Lit(3),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(create(0, true, advance)...)),
		),
		jen.Default().Block(reject(label)),
	)
}

func create(targetSlots int, borrowed bool, advance int) []jen.Code {
	body := []jen.Code{}
	if targetSlots == 0 {
		body = append(body, overflow())
	}
	body = append(body,
		jen.If(jen.Id("i").Dot("sp").Op("<").Add(adjust(jen.Id("captures"), targetSlots))).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("base").Op(":=").Add(adjust(jen.Id("i").Dot("sp").Op("-").Id("captures"), -targetSlots)),
		jen.Id("upvals").Op(":=").Append(jen.Index().Qual("github.com/siyul-park/minivm/types", "Boxed").Values(), jen.Id("i").Dot("stack").Index(jen.Id("base").Op(":").Id("base").Op("+").Id("captures")).Op("...")),
	)
	if borrowed {
		body = append(body, jen.Id("i").Dot("retain").Call(jen.Id("addr")))
	}
	body = append(body,
		jen.Id("closure").Op(":=").Qual("github.com/siyul-park/minivm/types", "NewClosure").Call(jen.Id("fn").Dot("Typ"), jen.Qual("github.com/siyul-park/minivm/types", "Ref").Parens(jen.Id("addr")), jen.Id("upvals")),
		jen.Id("i").Dot("sp").Op("=").Id("base"),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("closure"))),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
	)
	return body
}

func clearRange(start, end jen.Code) jen.Code {
	return jen.Clear(jen.Id("i").Dot("stack").Index(jen.Add(start).Op(":").Add(end)))
}

func adjust(expr jen.Code, delta int) *jen.Statement {
	if delta < 0 {
		return jen.Add(expr).Op("-").Lit(-delta)
	}
	if delta > 0 {
		return jen.Add(expr).Op("+").Lit(delta)
	}
	return jen.Add(expr)
}

func release(args, returns jen.Code) jen.Code {
	return jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Add(args)).Block(
		jen.If(jen.Id("value").Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Continue()),
		jen.Id("kept").Op(":=").False(),
		jen.For(jen.List(jen.Id("_"), jen.Id("result")).Op(":=").Range().Add(returns)).Block(
			jen.If(jen.Id("result").Op("==").Id("value")).Block(
				jen.Id("kept").Op("=").True(),
				jen.Break(),
			),
		),
		jen.If(jen.Op("!").Id("kept")).Block(jen.Id("i").Dot("release").Call(jen.Id("value").Dot("Ref").Call())),
	)
}

// retire emits a frame teardown. guard, when non-nil, is a condition the
// caller computed while threading: the slot release only runs when it holds.
func retire(guard jen.Code) []jen.Code {
	sweep := jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("i").Dot("stack").Index(
		jen.Id("f").Dot("bp").Op(":").Id("i").Dot("sp").Op("-").Id("f").Dot("returns"),
	)).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value")))
	var discard jen.Code = sweep
	if guard != nil {
		discard = jen.If(guard).Block(sweep)
	}
	return []jen.Code{
		jen.Id("f").Op(":=").Id("i").Dot("fr"),
		jen.If(jen.Id("i").Dot("sp").Op("<").Id("f").Dot("returns")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.If(jen.Id("f").Dot("coro").Op("!=").Lit(0)).Block(
			jen.Id("coAddr").Op(":=").Id("f").Dot("coro"),
			jen.List(jen.Id("co"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("coAddr")).Assert(jen.Op("*").Id("coroutine")),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.If(jen.Id("f").Dot("returns").Op(">").Lit(0)).Block(
				jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("i").Dot("stack").Index(
					jen.Id("f").Dot("bp").Op(":").Id("i").Dot("sp").Op("-").Lit(1),
				)).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value"))),
				jen.Id("co").Dot("value").Op("=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
			).Else().Block(
				jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("i").Dot("stack").Index(
					jen.Id("f").Dot("bp").Op(":").Id("i").Dot("sp"),
				)).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value"))),
				jen.Id("i").Dot("retain").Call(jen.Lit(0)),
				jen.Id("co").Dot("value").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxedNull"),
			),
			jen.Id("co").Dot("done").Op("=").True(),
			jen.Id("co").Dot("image").Op("=").Id("co").Dot("image").Index(jen.Empty().Op(":").Lit(0)),
			jen.Id("co").Dot("upvals").Op("=").Nil(),
			jen.If(jen.Id("f").Dot("release")).Block(jen.Id("i").Dot("release").Call(jen.Id("f").Dot("ref"))),
			jen.Id("co").Dot("ref").Op("=").Lit(0),
			jen.Id("co").Dot("release").Op("=").False(),
			jen.Id("bp").Op(":=").Id("f").Dot("bp"),
			jen.Id("f").Dot("code").Op("=").Nil(),
			jen.Id("f").Dot("upvals").Op("=").Nil(),
			jen.Id("f").Dot("coro").Op("=").Lit(0),
			jen.Id("i").Dot("fp").Op("--"),
			jen.Id("i").Dot("fr").Op("=").Op("&").Id("i").Dot("frames").Index(jen.Id("i").Dot("fp").Op("-").Lit(1)),
			jen.Id("i").Dot("stack").Index(jen.Id("bp")).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxRef").Call(jen.Id("coAddr")),
			jen.Id("i").Dot("sp").Op("=").Id("bp").Op("+").Lit(1),
			jen.Return(),
		),
		jen.Comment("A frame owns its params, locals, and any operands left below the"),
		jen.Comment("returned values; only the returns pass to the caller, so everything"),
		jen.Comment("under them is released here. land does the same when an exception"),
		jen.Comment("unwinds the frame, and the cycle collector needs counts to be exact."),
		discard,
		jen.Switch(jen.Id("f").Dot("returns")).Block(
			jen.Case(jen.Lit(0)).Block(),
			jen.Case(jen.Lit(1)).Block(jen.Id("i").Dot("stack").Index(jen.Id("f").Dot("bp")).Op("=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1))),
			jen.Default().Block(jen.Copy(
				jen.Id("i").Dot("stack").Index(jen.Id("f").Dot("bp").Op(":").Id("f").Dot("bp").Op("+").Id("f").Dot("returns")),
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("f").Dot("returns").Op(":").Id("i").Dot("sp")),
			)),
		),
		jen.Id("i").Dot("sp").Op("=").Id("f").Dot("bp").Op("+").Id("f").Dot("returns"),
		jen.If(jen.Id("f").Dot("release")).Block(jen.Id("i").Dot("release").Call(jen.Id("f").Dot("ref"))),
		jen.Id("f").Dot("code").Op("=").Nil(),
		jen.Id("i").Dot("fp").Op("--"),
		jen.Id("i").Dot("fr").Op("=").Op("&").Id("i").Dot("frames").Index(jen.Id("i").Dot("fp").Op("-").Lit(1)),
	}
}

// lookup emits ARRAY_GET and STRUCT_GET from a resolved index expression,
// delegating the per-representation read to (*Interpreter).arrayElem or
// (*Interpreter).structField so this generated handler and every fused
// fallback that reaches the same generic case share one runtime copy of the
// dispatch instead of duplicating it in generated code.
func lookup(op instr.Opcode, index jen.Code, advance int) []jen.Code {
	method := "arrayElem"
	if op != instr.ARRAY_GET {
		method = "structField"
	}
	return []jen.Code{
		jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("ref").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
		jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("addr").Op(":=").Id("ref").Dot("Ref").Call(),
		jen.Id("result").Op(":=").Id("i").Dot(method).Call(jen.Id("addr"), index),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("--"),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("result"),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
	}
}

func bounds(offset, size, length jen.Code) jen.Code {
	return jen.If(jen.Add(offset).Op("<").Lit(0).Op("||").Add(offset).Op("+").Add(size).Op(">").Add(length)).Block(
		jen.Panic(jen.Id("ErrIndexOutOfRange")),
	)
}

// numeric emits one numeric operation from virtual and resident operands.
func numeric(consumer instr.Opcode, inputs []value, advance int, label string, conditional bool, local jen.Code) ([]jen.Code, error) {
	arity, ok := arity(consumer)
	if !ok {
		return nil, fmt.Errorf("unsupported numeric consumer %s", instr.TypeOf(consumer).Mnemonic)
	}
	if len(inputs) > arity {
		return nil, fmt.Errorf("numeric pattern has %d sources", len(inputs))
	}
	kind, ok := numericKind(consumer)
	if !ok {
		return nil, fmt.Errorf("unsupported numeric kind for %s", instr.TypeOf(consumer).Mnemonic)
	}
	if traps(consumer) && local == nil {
		return checked(consumer, inputs, kind)
	}
	if traps(consumer) {
		return nil, fmt.Errorf("%s cannot fuse with local.set", instr.TypeOf(consumer).Mnemonic)
	}

	var compile, body []jen.Code
	// delta is the operation's net effect on the operand stack: it grows only
	// when every operand was folded into a temporary. Fused sources never
	// push, so one room check for delta covers the whole handler, and a
	// conditional consumer branches instead of pushing and needs none.
	delta := len(inputs) - arity + 1
	if !conditional && local == nil && delta > 0 {
		body = append(body, overflow())
	}
	for _, source := range inputs {
		compile = append(compile, source.compile...)
		body = append(body, source.check...)
		body = append(body, source.body...)
	}

	operands := make([]jen.Code, 0, arity)
	missing := arity - len(inputs)
	if missing > 0 {
		body = append(body, jen.If(jen.Id("i").Dot("sp").Op("<").Lit(missing)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))))
		for index := missing; index > 0; index-- {
			operands = append(operands, take(kind, jen.Id("i").Dot("sp").Op("-").Lit(index)))
		}
	}
	boxed := consumer == instr.I32_XOR || consumer == instr.I32_AND || consumer == instr.I32_OR
	for _, source := range inputs {
		if boxed {
			operands = append(operands, source.boxed)
		} else {
			operands = append(operands, source.raw)
		}
	}
	if boxed && missing > 0 {
		for index := range missing {
			operands[index] = jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(missing - index))
		}
	}

	result := temp(len(inputs))
	body = append(body, jen.Id(result).Op(":=").Add(apply(consumer, operands...)))
	if conditional {
		body = append(body, jump(jen.Id(result).Dot("Bool").Call(), missing, advance)...)
	} else if local != nil {
		body = append(body,
			jen.Id("addr").Op(":=").Id("i").Dot("fr").Dot("bp").Op("+").Add(local),
			jen.If(jen.Id("addr").Op(">=").Id("i").Dot("sp")).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
		)
		if instr.TypeOf(consumer).Push[0].Repr() == instr.KindI64 {
			body = append(body,
				jen.Id("old").Op(":=").Id("i").Dot("stack").Index(jen.Id("addr")),
				jen.If(jen.Id("old").Op("!=").Id(result)).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			)
		}
		body = append(body,
			jen.Id("i").Dot("stack").Index(jen.Id("addr")).Op("=").Id(result),
			jen.Id("i").Dot("sp").Op("-=").Lit(missing),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
		)
	} else {
		if delta > 0 {
			body = append(body, jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id(result), jen.Id("i").Dot("sp").Op("++"))
		} else {
			if delta < 0 {
				body = append(body, jen.Id("i").Dot("sp").Op("-=").Lit(-delta))
			}
			body = append(body, jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Op("=").Id(result))
		}
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance))
	}

	first := consumer
	if len(inputs) > 0 {
		first = inputs[0].op
	}
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(first)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return compile, nil
}

func jump(condition jen.Code, consume, advance int) []jen.Code {
	if consume == 0 {
		return []jen.Code{
			jen.If(condition).Block(jen.Id("i").Dot("fr").Dot("ip").Op("+=").Id("offset")),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
		}
	}
	return []jen.Code{
		jen.Id("i").Dot("sp").Op("-=").Lit(consume),
		jen.If(condition).Block(jen.Id("i").Dot("fr").Dot("ip").Op("+=").Id("offset")),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
	}
}

func arity(op instr.Opcode) (int, bool) {
	if op == instr.I32_EQZ || op == instr.I64_EQZ {
		return 1, true
	}
	for _, family := range families {
		if slices.Contains(family.binary, op) || slices.Contains(family.compare, op) {
			return 2, true
		}
	}
	return 0, false
}

func checked(consumer instr.Opcode, inputs []value, kind instr.Kind) ([]jen.Code, error) {
	var compile, body []jen.Code
	for _, source := range inputs {
		compile = append(compile, source.compile...)
		body = append(body, source.push...)
	}
	body = append(body,
		jen.If(jen.Id("i").Dot("sp").Op("<").Lit(2)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("rhs").Op(":=").Add(take(kind, jen.Id("i").Dot("sp").Op("-").Lit(1))),
		jen.Id("lhs").Op(":=").Add(take(kind, jen.Id("i").Dot("sp").Op("-").Lit(2))),
		jen.If(jen.Id("rhs").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrDivideByZero"))),
		jen.Id("result").Op(":=").Add(apply(consumer, jen.Id("lhs"), jen.Id("rhs"))),
		jen.Id("i").Dot("sp").Op("--"),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Op("=").Id("result"),
		jen.Id("i").Dot("fr").Dot("ip").Op("++"),
	)
	first := consumer
	if len(inputs) > 0 {
		first = inputs[0].op
	}
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(first)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return compile, nil
}

func apply(op instr.Opcode, operands ...jen.Code) jen.Code {
	lhs := operands[0]
	if len(operands) == 1 {
		switch op {
		case instr.I32_EQZ, instr.I64_EQZ:
			return jen.Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(jen.Add(lhs).Op("==").Lit(0))
		}
	}
	rhs := operands[1]
	binary := func(name string, value jen.Code) jen.Code {
		return jen.Qual("github.com/siyul-park/minivm/types", name).Call(value)
	}
	compare := func(value jen.Code) jen.Code {
		return binary("BoxI1", value)
	}
	switch op {
	case instr.I32_ADD:
		return binary("BoxI32", jen.Add(lhs).Op("+").Add(rhs))
	case instr.I32_SUB:
		return binary("BoxI32", jen.Add(lhs).Op("-").Add(rhs))
	case instr.I32_MUL:
		return binary("BoxI32", jen.Add(lhs).Op("*").Add(rhs))
	case instr.I32_DIV_S:
		return binary("BoxI32", jen.Add(lhs).Op("/").Add(rhs))
	case instr.I32_DIV_U:
		return binary("BoxI32", jen.Int32().Call(jen.Uint32().Call(lhs).Op("/").Uint32().Call(rhs)))
	case instr.I32_REM_S:
		return binary("BoxI32", jen.Add(lhs).Op("%").Add(rhs))
	case instr.I32_REM_U:
		return binary("BoxI32", jen.Int32().Call(jen.Uint32().Call(lhs).Op("%").Uint32().Call(rhs)))
	case instr.I32_SHL:
		return binary("BoxI32", jen.Add(lhs).Op("<<").Parens(jen.Add(rhs).Op("&").Lit(0x1f)))
	case instr.I32_SHR_S:
		return binary("BoxI32", jen.Add(lhs).Op(">>").Parens(jen.Add(rhs).Op("&").Lit(0x1f)))
	case instr.I32_SHR_U:
		return binary("BoxI32", jen.Int32().Call(jen.Uint32().Call(lhs).Op(">>").Parens(jen.Add(rhs).Op("&").Lit(0x1f))))
	case instr.I32_XOR:
		payload := jen.Uint64().Call(lhs).Op("^").Uint64().Call(rhs)
		tag := jen.Uint64().Call(lhs).Op("&").Uint64().Call(rhs).Op("&").Op("^").Uint64().Call(jen.Qual("github.com/siyul-park/minivm/types", "VMask"))
		return jen.Qual("github.com/siyul-park/minivm/types", "Boxed").Call(tag.Op("|").Parens(payload.Op("&").Qual("github.com/siyul-park/minivm/types", "VMask")))
	case instr.I32_OR:
		return jen.Qual("github.com/siyul-park/minivm/types", "Boxed").Call(jen.Uint64().Call(lhs).Op("|").Uint64().Call(rhs))
	case instr.I32_AND:
		return jen.Qual("github.com/siyul-park/minivm/types", "Boxed").Call(jen.Uint64().Call(lhs).Op("&").Uint64().Call(rhs))
	case instr.I32_ROTL:
		return binary("BoxI32", jen.Int32().Call(jen.Qual("math/bits", "RotateLeft32").Call(jen.Uint32().Call(lhs), jen.Int().Call(rhs))))
	case instr.I32_ROTR:
		return binary("BoxI32", jen.Int32().Call(jen.Qual("math/bits", "RotateLeft32").Call(jen.Uint32().Call(lhs), jen.Op("-").Int().Call(rhs))))
	case instr.I32_EQ:
		return compare(jen.Add(lhs).Op("==").Add(rhs))
	case instr.I32_NE:
		return compare(jen.Add(lhs).Op("!=").Add(rhs))
	case instr.I32_LT_S:
		return compare(jen.Add(lhs).Op("<").Add(rhs))
	case instr.I32_LT_U:
		return compare(jen.Uint32().Call(lhs).Op("<").Uint32().Call(rhs))
	case instr.I32_GT_S:
		return compare(jen.Add(lhs).Op(">").Add(rhs))
	case instr.I32_GT_U:
		return compare(jen.Uint32().Call(lhs).Op(">").Uint32().Call(rhs))
	case instr.I32_LE_S:
		return compare(jen.Add(lhs).Op("<=").Add(rhs))
	case instr.I32_LE_U:
		return compare(jen.Uint32().Call(lhs).Op("<=").Uint32().Call(rhs))
	case instr.I32_GE_S:
		return compare(jen.Add(lhs).Op(">=").Add(rhs))
	case instr.I32_GE_U:
		return compare(jen.Uint32().Call(lhs).Op(">=").Uint32().Call(rhs))
	case instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_DIV_S, instr.I64_REM_S,
		instr.I64_SHL, instr.I64_SHR_S, instr.I64_XOR, instr.I64_AND, instr.I64_OR:
		token := "+"
		switch op {
		case instr.I64_SUB:
			token = "-"
		case instr.I64_MUL:
			token = "*"
		case instr.I64_DIV_S:
			token = "/"
		case instr.I64_REM_S:
			token = "%"
		case instr.I64_SHL:
			token = "<<"
		case instr.I64_SHR_S:
			token = ">>"
		case instr.I64_XOR:
			token = "^"
		case instr.I64_AND:
			token = "&"
		case instr.I64_OR:
			token = "|"
		}
		value := jen.Add(lhs).Op(token).Add(rhs)
		if op == instr.I64_SHL || op == instr.I64_SHR_S {
			value = jen.Add(lhs).Op(token).Parens(jen.Add(rhs).Op("&").Lit(0x3f))
		}
		return jen.Id("i").Dot("boxI64").Call(value)
	case instr.I64_DIV_U, instr.I64_REM_U:
		token := "/"
		if op == instr.I64_REM_U {
			token = "%"
		}
		return jen.Id("i").Dot("boxI64").Call(jen.Int64().Call(jen.Uint64().Call(lhs).Op(token).Uint64().Call(rhs)))
	case instr.I64_SHR_U:
		return jen.Id("i").Dot("boxI64").Call(jen.Int64().Call(jen.Uint64().Call(lhs).Op(">>").Parens(jen.Add(rhs).Op("&").Lit(0x3f))))
	case instr.I64_ROTL, instr.I64_ROTR:
		count := jen.Int().Call(rhs)
		if op == instr.I64_ROTR {
			count = jen.Op("-").Int().Call(rhs)
		}
		return jen.Id("i").Dot("boxI64").Call(jen.Int64().Call(jen.Qual("math/bits", "RotateLeft64").Call(jen.Uint64().Call(lhs), count)))
	case instr.I64_EQ, instr.I64_NE, instr.I64_LT_S, instr.I64_GT_S, instr.I64_LE_S, instr.I64_GE_S:
		token := "=="
		switch op {
		case instr.I64_NE:
			token = "!="
		case instr.I64_LT_S:
			token = "<"
		case instr.I64_GT_S:
			token = ">"
		case instr.I64_LE_S:
			token = "<="
		case instr.I64_GE_S:
			token = ">="
		}
		return compare(jen.Add(lhs).Op(token).Add(rhs))
	case instr.I64_LT_U, instr.I64_GT_U, instr.I64_LE_U, instr.I64_GE_U:
		token := "<"
		switch op {
		case instr.I64_GT_U:
			token = ">"
		case instr.I64_LE_U:
			token = "<="
		case instr.I64_GE_U:
			token = ">="
		}
		return compare(jen.Uint64().Call(lhs).Op(token).Uint64().Call(rhs))
	case instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV:
		token := "+"
		switch op {
		case instr.F32_SUB:
			token = "-"
		case instr.F32_MUL:
			token = "*"
		case instr.F32_DIV:
			token = "/"
		}
		return binary("BoxF32", jen.Add(lhs).Op(token).Add(rhs))
	case instr.F32_REM:
		return binary("BoxF32", jen.Float32().Call(jen.Qual("math", "Mod").Call(jen.Float64().Call(lhs), jen.Float64().Call(rhs))))
	case instr.F32_MOD:
		return jen.Func().Params(jen.Id("lhs"), jen.Id("rhs").Float32()).Qual("github.com/siyul-park/minivm/types", "Boxed").Block(
			jen.Id("m").Op(":=").Qual("math", "Mod").Call(jen.Float64().Call(jen.Id("lhs")), jen.Float64().Call(jen.Id("rhs"))),
			jen.If(jen.Id("m").Op("!=").Lit(0).Op("&&").Parens(jen.Id("m").Op("<").Lit(0)).Op("!=").Parens(jen.Id("rhs").Op("<").Lit(0))).Block(jen.Id("m").Op("+=").Float64().Call(jen.Id("rhs"))),
			jen.Return(jen.Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(jen.Float32().Call(jen.Id("m")))),
		).Call(lhs, rhs)
	case instr.F32_MIN:
		return binary("BoxF32", jen.Min(lhs, rhs))
	case instr.F32_MAX:
		return binary("BoxF32", jen.Max(lhs, rhs))
	case instr.F32_COPYSIGN:
		return binary("BoxF32", jen.Float32().Call(jen.Qual("math", "Copysign").Call(jen.Float64().Call(lhs), jen.Float64().Call(rhs))))
	case instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV:
		token := "+"
		switch op {
		case instr.F64_SUB:
			token = "-"
		case instr.F64_MUL:
			token = "*"
		case instr.F64_DIV:
			token = "/"
		}
		return binary("BoxF64", jen.Add(lhs).Op(token).Add(rhs))
	case instr.F64_REM:
		return binary("BoxF64", jen.Qual("math", "Mod").Call(lhs, rhs))
	case instr.F64_MOD:
		return jen.Func().Params(jen.Id("lhs"), jen.Id("rhs").Float64()).Qual("github.com/siyul-park/minivm/types", "Boxed").Block(
			jen.Id("m").Op(":=").Qual("math", "Mod").Call(jen.Id("lhs"), jen.Id("rhs")),
			jen.If(jen.Id("m").Op("!=").Lit(0).Op("&&").Parens(jen.Id("m").Op("<").Lit(0)).Op("!=").Parens(jen.Id("rhs").Op("<").Lit(0))).Block(jen.Id("m").Op("+=").Id("rhs")),
			jen.Return(jen.Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(jen.Id("m"))),
		).Call(lhs, rhs)
	case instr.F64_MIN:
		return binary("BoxF64", jen.Qual("math", "Min").Call(lhs, rhs))
	case instr.F64_MAX:
		return binary("BoxF64", jen.Qual("math", "Max").Call(lhs, rhs))
	case instr.F64_COPYSIGN:
		return binary("BoxF64", jen.Qual("math", "Copysign").Call(lhs, rhs))
	case instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_GT, instr.F32_LE, instr.F32_GE,
		instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_GT, instr.F64_LE, instr.F64_GE:
		token := "=="
		switch op {
		case instr.F32_NE, instr.F64_NE:
			token = "!="
		case instr.F32_LT, instr.F64_LT:
			token = "<"
		case instr.F32_GT, instr.F64_GT:
			token = ">"
		case instr.F32_LE, instr.F64_LE:
			token = "<="
		case instr.F32_GE, instr.F64_GE:
			token = ">="
		}
		return compare(jen.Add(lhs).Op(token).Add(rhs))
	default:
		panic(fmt.Sprintf("unsupported numeric opcode %s", instr.TypeOf(op).Mnemonic))
	}
}

func traps(op instr.Opcode) bool {
	switch op {
	case instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U,
		instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U,
		instr.F32_DIV, instr.F32_REM, instr.F32_MOD, instr.F64_DIV, instr.F64_REM, instr.F64_MOD:
		return true
	default:
		return false
	}
}

func load(current step, slot, offset int, label string, standalone bool) (value, error) {
	loader := newLoader(current.op, slot, offset, label, standalone)
	result := value{op: current.op, head: current.op, boxed: jen.Id(loader.boxed)}
	// A source only needs stack room when it pushes on its own. Fused into a
	// consumer it stays in a temporary, and the consumer checks the room its
	// own net push needs.
	result.room = true

	_, _, indexed := slotInfo(current.op)
	loader.decode(&result, current.op)
	if standalone && (indexed || current.op == instr.CONST_GET) {
		result.compile = append(result.compile, jen.Id("c").Dot("ip").Op("+=").Lit(loader.width))
	}

	var err error
	switch current.op {
	case instr.LOCAL_GET, instr.GLOBAL_GET, instr.UPVAL_GET:
		err = loader.read(&result, current)
	case instr.CONST_GET:
		err = loader.constant(&result, current)
	case instr.I32_CONST, instr.I64_CONST, instr.F32_CONST, instr.F64_CONST:
		err = loader.literal(&result, current)
	default:
		err = fmt.Errorf("unsupported source opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	if err != nil {
		return value{}, err
	}
	return loader.finish(result, current, indexed)
}

func slotInfo(op instr.Opcode) (field, method string, ok bool) {
	switch op {
	case instr.LOCAL_GET:
		return "locals", "local", true
	case instr.GLOBAL_GET:
		return "globals", "global", true
	case instr.UPVAL_GET:
		return "captures", "upval", true
	default:
		return "", "", false
	}
}

// isContainerSource reports whether op is a slot-read opcode (LOCAL_GET,
// GLOBAL_GET, UPVAL_GET) that array.get/struct.get container fusion can
// prove a declared element or field type from.
func isContainerSource(op instr.Opcode) bool {
	_, _, ok := slotInfo(op)
	return ok
}

// declaredTypesField names the threader field holding op's declared
// types.Type per slot, indexed the same way slotInfo's Kind-only field is:
// LOCAL_GET by localTypes, GLOBAL_GET by globalTypes, UPVAL_GET by
// captureTypes.
func declaredTypesField(op instr.Opcode) (string, bool) {
	switch op {
	case instr.LOCAL_GET:
		return "localTypes", true
	case instr.GLOBAL_GET:
		return "globalTypes", true
	case instr.UPVAL_GET:
		return "captureTypes", true
	default:
		return "", false
	}
}

func overflow() jen.Code {
	return jen.If(jen.Id("i").Dot("sp").Op("==").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow")))
}

func temp(index int) string {
	return fmt.Sprintf("v%d", index)
}

func boxed(raw string) string {
	return "r" + strings.TrimPrefix(raw, "v")
}

func reject(label string) jen.Code {
	if label != "" {
		return jen.Goto().Id(label)
	}
	return jen.Return(jen.Nil())
}

func numericKind(op instr.Opcode) (instr.Kind, bool) {
	pop := instr.TypeOf(op).Pop
	if len(pop) == 0 {
		return instr.KindAny, false
	}
	kind := pop[0].Repr()
	return kind, kind.IsNumeric()
}

func borrow(kind instr.Kind, boxed jen.Code) jen.Code {
	if kind.Repr() == instr.KindI64 {
		return jen.Id("i").Dot("borrowI64").Call(boxed)
	}
	name, ok := kindName(kind)
	if !ok {
		panic(fmt.Sprintf("unsupported borrowed kind %s", kind))
	}
	return jen.Add(boxed).Dot(name).Call()
}

func take(kind instr.Kind, index jen.Code) jen.Code {
	value := jen.Id("i").Dot("stack").Index(index)
	if kind.Repr() == instr.KindI64 {
		return jen.Id("i").Dot("unboxI64").Call(value)
	}
	name, ok := kindName(kind)
	if !ok {
		panic(fmt.Sprintf("unsupported consumed kind %s", kind))
	}
	return value.Dot(name).Call()
}

func kindName(kind instr.Kind) (string, bool) {
	switch kind.Repr() {
	case instr.KindI32:
		return "I32", true
	case instr.KindI64:
		return "I64", true
	case instr.KindF32:
		return "F32", true
	case instr.KindF64:
		return "F64", true
	case instr.KindRef:
		return "Ref", true
	default:
		return "", false
	}
}

// fieldKindName names the types.Kind constant for kind, keeping i1 and i8
// narrow instead of kindName's reduced Repr, because ArrayType.ElemKind and
// StructField.Kind both store the element or field's real width.
func fieldKindName(kind instr.Kind) (string, bool) {
	switch kind {
	case instr.KindI1:
		return "I1", true
	case instr.KindI8:
		return "I8", true
	default:
		return kindName(kind)
	}
}

// structFieldBox returns the boxed expression for a struct field's raw
// 64-bit data slot once kind is known, mirroring (*types.Struct).Field's
// per-kind decoding without its runtime switch over the field's Kind.
func structFieldBox(kind instr.Kind, data jen.Code) jen.Code {
	switch kind {
	case instr.KindI1:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(jen.Add(data).Op("!=").Lit(0))
	case instr.KindI8:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI8").Call(jen.Int8().Call(jen.Uint32().Call(data)))
	case instr.KindI32:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(jen.Int32().Call(jen.Uint32().Call(data)))
	case instr.KindI64:
		return jen.Id("i").Dot("boxI64").Call(jen.Int64().Call(data))
	case instr.KindF32:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(jen.Qual("math", "Float32frombits").Call(jen.Uint32().Call(data)))
	case instr.KindF64:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(jen.Qual("math", "Float64frombits").Call(data))
	case instr.KindRef:
		return jen.Qual("github.com/siyul-park/minivm/types", "Boxed").Call(data)
	default:
		panic(fmt.Sprintf("unsupported struct field kind %s", kind))
	}
}

func arrayKind(typ reflect.Type) (instr.Kind, bool) {
	switch typ {
	case reflect.TypeFor[types.TypedArray[bool]]():
		return instr.KindI1, true
	case reflect.TypeFor[types.TypedArray[int8]]():
		return instr.KindI8, true
	case reflect.TypeFor[types.TypedArray[int32]]():
		return instr.KindI32, true
	case reflect.TypeFor[types.TypedArray[int64]]():
		return instr.KindI64, true
	case reflect.TypeFor[types.TypedArray[float32]]():
		return instr.KindF32, true
	case reflect.TypeFor[types.TypedArray[float64]]():
		return instr.KindF64, true
	default:
		return instr.KindAny, false
	}
}

func typeName(typ reflect.Type) jen.Code {
	return jen.Qual(typ.PkgPath(), typ.Name())
}

func storeArray(kind instr.Kind, array, index, raw jen.Code) jen.Code {
	elem := jen.Add(array).Index(index)
	switch kind {
	case instr.KindI1:
		return jen.List(elem).Op("=").List(jen.Add(raw).Op("!=").Lit(0))
	case instr.KindI8:
		return jen.List(elem).Op("=").List(jen.Int8().Call(raw))
	case instr.KindI32:
		return jen.List(elem).Op("=").List(jen.Int32().Call(raw))
	case instr.KindI64:
		return jen.List(elem).Op("=").List(raw)
	case instr.KindF32:
		return jen.List(elem).Op("=").List(jen.Float32().Call(raw))
	case instr.KindF64:
		return jen.List(elem).Op("=").List(jen.Float64().Call(raw))
	default:
		panic(fmt.Sprintf("unsupported array element kind %s", kind))
	}
}

func boxArray(kind instr.Kind, array, index jen.Code) jen.Code {
	elem := jen.Add(array).Index(index)
	switch kind {
	case instr.KindI1:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(elem)
	case instr.KindI8:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI8").Call(elem)
	case instr.KindI32:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(elem)
	case instr.KindI64:
		return jen.Id("i").Dot("boxI64").Call(elem)
	case instr.KindF32:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(elem)
	case instr.KindF64:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(elem)
	default:
		panic(fmt.Sprintf("unsupported array element kind %s", kind))
	}
}

func mustKindName(kind instr.Kind) string {
	name, ok := kindName(kind)
	if !ok {
		panic(fmt.Sprintf("unsupported kind %s", kind))
	}
	return name
}

func immediate(kind instr.Kind, at jen.Code) jen.Code {
	operand := jen.Qual("github.com/siyul-park/minivm/instr", "Instruction").Call(jen.Id("c").Dot("code").Index(jen.Add(at).Op(":"))).Dot("Operand").Call(jen.Lit(0))
	switch kind.Repr() {
	case instr.KindI32:
		return jen.Int32().Call(operand)
	case instr.KindI64:
		return jen.Int64().Call(operand)
	case instr.KindF32:
		return jen.Qual("github.com/siyul-park/minivm/types", "Box").Call(jen.Uint64().Call(jen.Uint32().Call(operand)), jen.Qual("github.com/siyul-park/minivm/types", "KindF32")).Dot("F32").Call()
	case instr.KindF64:
		return jen.Qual("github.com/siyul-park/minivm/types", "Boxed").Call(operand).Dot("F64").Call()
	default:
		panic(fmt.Sprintf("unsupported immediate kind %s", kind))
	}
}

func width(op instr.Opcode) int {
	width := 1
	for _, operand := range instr.TypeOf(op).Widths {
		width += operand
	}
	return width
}

func add(expr jen.Code, offset int) *jen.Statement {
	if offset == 0 {
		return jen.Add(expr)
	}
	return jen.Add(expr).Op("+").Lit(offset)
}

func arrayAppend() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("n")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.If(jen.Id("n").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("n").Op("+").Add(jen.Lit(2))))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("n")).Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.List(jen.Id("base")).Op(":=").List(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("n")).Op("-").Add(jen.Lit(1))),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("Bool").Call()))),
				jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("int8").Call(jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("I32").Call())))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("I32").Call()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))))))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("F32").Call()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("F64").Call()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Dot("Elems")).Op("=").List(jen.Id("append").Call(jen.Id("arr").Dot("Elems"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("n").Op("+").Add(jen.Lit(1))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayCopy() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(5))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.If(jen.Id("size").Op("<").Lit(0)).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
			jen.List(jen.Id("srcOffset")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))).Dot("I32").Call())),
			jen.List(jen.Id("srcRef")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.List(jen.Id("dstOffset")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(4))).Dot("I32").Call())),
			jen.List(jen.Id("dstRef")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(5)))),
			jen.If(jen.Id("srcRef").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef")).Op("||").Add(jen.Id("dstRef").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("srcAddr")).Op(":=").List(jen.Id("srcRef").Dot("Ref").Call()),
			jen.List(jen.Id("dstAddr")).Op(":=").List(jen.Id("dstRef").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("dst")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("dstAddr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool")))),
				jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
				jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Op("*").Add(jen.Id("types").Dot("Array")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src").Dot("Elems"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst").Dot("Elems"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(jen.Id("src").Dot("Elems").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("v"))),
					jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(jen.Id("dst").Dot("Elems").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("v"))),
					jen.Id("copy").Call(jen.Id("dst").Dot("Elems").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Dot("Elems").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("srcAddr")),
			jen.Id("i").Dot("release").Call(jen.Id("dstAddr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(5)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayDelete() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Var().Add(jen.List(jen.Id("val"))).Add(jen.Id("types").Dot("Boxed")),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
				jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
				jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
				jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("arr").Index(jen.Id("idx")))),
				jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
				jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxI8").Call(jen.Id("arr").Index(jen.Id("idx")))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("arr").Index(jen.Id("idx"))))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("arr").Index(jen.Id("idx"))))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("arr").Index(jen.Id("idx"))))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("arr").Index(jen.Id("idx"))))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr").Dot("Elems"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("arr").Dot("Elems").Index(jen.Id("idx"))),
					jen.Id("copy").Call(jen.Id("arr").Dot("Elems").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Dot("Elems").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("arr").Dot("Elems").Index(jen.Id("len").Call(jen.Id("arr").Dot("Elems")).Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxedNull")),
					jen.List(jen.Id("arr").Dot("Elems")).Op("=").List(jen.Id("arr").Dot("Elems").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr").Dot("Elems")).Op("-").Add(jen.Lit(1)))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayFill() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(4))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(4)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
				jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
				jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.List(jen.Id("v")).Op(":=").List(jen.Id("val").Dot("Bool").Call()),
				jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("int8").Call(jen.Id("val").Dot("I32").Call())),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("val").Dot("I32").Call()),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("val"))),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("val").Dot("F32").Call()),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("val").Dot("F64").Call()),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr").Dot("Elems"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.If(jen.Id("val").Dot("Kind").Call().Op("==").Add(jen.Id("types").Dot("KindRef")).Op("&&").Add(jen.Id("size").Op(">").Add(jen.Lit(1)))).Block(jen.Id("i").Dot("retains").Call(jen.Id("val").Dot("Ref").Call(), jen.Id("size").Op("-").Add(jen.Lit(1)))),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("old")).Op(":=").List(jen.Id("arr").Dot("Elems").Index(jen.Id("k"))),
						jen.List(jen.Id("arr").Dot("Elems").Index(jen.Id("k"))).Op("=").List(jen.Id("val")),
						jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
					jen.If(jen.Id("size").Op("<=").Add(jen.Lit(0))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("val")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(4)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayLen() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.Var().Add(jen.List(jen.Id("n"))).Add(jen.Id("int32")),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("unbox").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr").Dot("Elems"))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("n"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayNew() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("ArrayType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.Switch(jen.Id("typ").Dot("ElemKind")).Block(jen.Case(jen.Id("types").Dot("KindI1")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool")), jen.Id("size"))),
			jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("Bool").Call())),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI8")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("int8").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("I32").Call()))),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("I32").Call())),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j")))))),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("F32").Call())),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("F64").Call())),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Default().Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("newArray").Call(jen.Id("typ"), jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Id("size")))),
				jen.Id("copy").Call(jen.Id("val").Dot("Elems"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op(":").Add(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))))
}

func arrayNewDefault() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("ArrayType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.Switch(jen.Id("typ").Dot("ElemKind")).Block(jen.Case(jen.Id("types").Dot("KindI1")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool")), jen.Id("size"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI8")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Default().Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("newArray").Call(jen.Id("typ"), jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Id("size")))),
				jen.For(jen.List(jen.Id("j")).Op(":=").Range().Add(jen.Id("val").Dot("Elems"))).Block(jen.List(jen.Id("val").Dot("Elems").Index(jen.Id("j"))).Op("=").List(jen.Id("types").Dot("BoxedNull"))),
				jen.Id("i").Dot("retains").Call(jen.Lit(0), jen.Id("int").Call(jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))))
}

func element(state *state, current step) (value, error) {
	if state.standalone {
		return value{op: current.op, head: current.op, handler: arraySet()}, nil
	}
	if current.op != instr.ARRAY_SET || len(state.stack) != 3 {
		return value{}, fmt.Errorf("no fusion lowering for %s", instr.TypeOf(current.op).Mnemonic)
	}

	container := state.stack[0]
	index := state.stack[1]
	val := state.stack[2]
	kind, ok := arrayKind(container.typ)
	if !ok {
		return value{}, fmt.Errorf("array.set cannot resolve element kind")
	}
	compile := append([]jen.Code(nil), container.compile...)
	compile = append(compile, index.compile...)
	compile = append(compile, val.compile...)
	body := []jen.Code{overflow()}

	if container.object != nil {
		body = append(body, index.check...)
		body = append(body, index.body...)
		body = append(body, val.check...)
		body = append(body, val.body...)
		raw := val.raw
		if raw == nil {
			raw = val.boxed
		}
		body = append(body,
			jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.object).Assert(typeName(container.typ)),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("at").Op(":=").Int().Call(index.raw),
			bounds(jen.Id("at"), jen.Lit(1), jen.Len(jen.Id("array"))),
			storeArray(kind, jen.Id("array"), jen.Id("at"), raw),
			jen.Id("i").Dot("sp").Op("-=").Lit(3),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
			jen.Return(),
		)
		compile = append(compile,
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
		)
		state.stack = nil
		return value{op: current.op, head: container.head, compile: compile}, nil
	}

	if isContainerSource(container.op) && container.typ != nil {
		body = append(body, val.check...)
		body = append(body, val.body...)
		body = append(body, container.check...)
		body = append(body, container.body...)
		body = append(body, index.check...)
		body = append(body, index.body...)
		raw := val.raw
		if raw == nil {
			raw = val.boxed
		}
		body = append(body,
			jen.If(jen.Add(container.boxed).Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("at").Op(":=").Int().Call(index.raw),
			jen.If(jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.raw).Assert(typeName(container.typ)), jen.Id("ok")).Block(
				bounds(jen.Id("at"), jen.Lit(1), jen.Len(jen.Id("array"))),
				storeArray(kind, jen.Id("array"), jen.Id("at"), raw),
				jen.Id("i").Dot("sp").Op("-=").Lit(3),
				jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
				jen.Return(),
			),
			jen.Id("i").Dot("retainBox").Call(val.boxed),
			jen.Id("i").Dot("setArrayElem").Call(container.raw, jen.Id("at"), val.boxed),
			jen.Id("i").Dot("sp").Op("-=").Lit(3),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
		)
		compile = append(compile,
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
		)
		state.stack = nil
		return value{op: current.op, head: container.head, compile: compile}, nil
	}

	return value{}, fmt.Errorf("no fusion lowering for %s", instr.TypeOf(current.op).Mnemonic)
}

func arraySet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(3))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
				jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
				jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
				jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.List(jen.Id("arr").Index(jen.Id("idx"))).Op("=").List(jen.Id("val").Dot("Bool").Call())),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("arr").Index(jen.Id("idx"))).Op("=").List(jen.Id("int8").Call(jen.Id("val").Dot("I32").Call()))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("arr").Index(jen.Id("idx"))).Op("=").List(jen.Id("val").Dot("I32").Call())),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("arr").Index(jen.Id("idx"))).Op("=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("val")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("arr").Index(jen.Id("idx"))).Op("=").List(jen.Id("val").Dot("F32").Call())),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("arr").Index(jen.Id("idx"))).Op("=").List(jen.Id("val").Dot("F64").Call())),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr").Dot("Elems"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("elem")).Op(":=").List(jen.Id("arr").Dot("Elems").Index(jen.Id("idx"))),
					jen.List(jen.Id("arr").Dot("Elems").Index(jen.Id("idx"))).Op("=").List(jen.Id("val")),
					jen.Id("i").Dot("releaseBox").Call(jen.Id("elem"))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(3)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arraySlice() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(3))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("end")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.List(jen.Id("start")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Var().Add(jen.List(jen.Id("out"))).Add(jen.Id("types").Dot("Value")),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
				jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool")), jen.Id("end").Op("-").Add(jen.Id("start")))),
				jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
				jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr").Dot("Elems")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("elems")).Op(":=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("elems"), jen.Id("arr").Dot("Elems").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(jen.Id("elems"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("v"))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("i").Dot("newArray").Call(jen.Id("arr").Dot("Typ"), jen.Id("elems")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("newAddr")).Op(":=").List(jen.Id("i").Dot("alloc").Call(jen.Id("out"))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(2)),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("newAddr"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func br() jen.Code {
	apply := []jen.Code{
		jen.Id("f").Op(":=").Id("i").Dot("fr"),
		jen.Id("f").Dot("ip").Op("+=").Id("offset").Op("+").Lit(3),
	}
	observe := append([]jen.Code(nil), apply...)
	observe = append(observe,
		jen.If(jen.Id("err").Op(":=").Id("c").Dot("backedge").Call(jen.Id("i"), jen.Id("f")), jen.Id("err").Op("!=").Nil()).Block(
			jen.Panic(jen.Id("err")),
		),
	)
	return threaderFunc(
		jen.Id("offset").Op(":=").Id("instr").Dot("ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("c").Dot("ip").Op("+").Lit(1)),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.If(jen.Id("c").Dot("backedge").Op("==").Nil().Op("||").Id("offset").Op(">").Lit(-3)).Block(
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(apply...)),
		),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(observe...)),
	)
}

func brTable() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("count")).Op(":=").List(jen.Id("int").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))),
		jen.List(jen.Id("offsets")).Op(":=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("int")), jen.Id("count").Op("+").Add(jen.Lit(1)))),
		jen.For(jen.List(jen.Id("i")).Op(":=").List(jen.Lit(0)), jen.Id("i").Op("<").Add(jen.Id("len").Call(jen.Id("offsets"))), jen.Id("i").Op("++")).Block(jen.List(jen.Id("at")).Op(":=").List(jen.Id("c").Dot("ip").Op("+").Add(jen.Id("i").Op("*").Add(jen.Lit(2))).Op("+").Add(jen.Lit(2))),
			jen.List(jen.Id("offsets").Index(jen.Id("i"))).Op("=").List(jen.Id("instr").Dot("ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("at")))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Id("count").Op("*").Add(jen.Lit(2)).Op("+").Add(jen.Lit(4))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("cond")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Dot("I32").Call())),
			jen.If(jen.Id("cond").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("cond").Op(">=").Add(jen.Id("count")))).Block(jen.List(jen.Id("cond")).Op("=").List(jen.Id("count"))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Id("offsets").Index(jen.Id("cond")).Op("+").Add(jen.Id("count").Op("*").Add(jen.Lit(2))).Op("+").Add(jen.Lit(4))))))
}

func coroDone() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("done")).Op(":=").List(jen.Id("int32").Call(jen.Lit(0))),
			jen.Switch(jen.List(jen.Id("co")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("ref").Dot("Ref").Call()).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("coroutine"))).Block(jen.If(jen.Id("co").Dot("done")).Block(jen.List(jen.Id("done")).Op("=").List(jen.Lit(1)))),
				jen.Case(jen.Id("types").Dot("Iterator")).Block(jen.If(jen.Id("co").Dot("Done").Call()).Block(jen.List(jen.Id("done")).Op("=").List(jen.Lit(1)))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("done").Op("!=").Add(jen.Lit(0)))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func coroValue() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("box")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("box").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.Var().Add(jen.List(jen.Id("val"))).Add(jen.Id("types").Dot("Boxed")),
			jen.Switch(jen.List(jen.Id("co")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("box").Dot("Ref").Call()).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("coroutine"))).Block(jen.List(jen.Id("val")).Op("=").List(jen.Id("co").Dot("value")),
				jen.Id("i").Dot("retainBox").Call(jen.Id("val"))),
				jen.Case(jen.Id("types").Dot("Iterator")).Block(jen.List(jen.Id("current")).Op(":=").List(jen.Id("co").Dot("Current").Call()),
					jen.If(jen.Id("current").Op("==").Add(jen.Id("nil"))).Block(jen.Id("i").Dot("retain").Call(jen.Lit(0)),
						jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxedNull"))).Else().Block(jen.List(jen.Id("val")).Op("=").List(jen.Id("i").Dot("box").Call(jen.Id("current"))),
						jen.Switch(jen.List(jen.Id("current")).Op(":=").List(jen.Id("current").Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("Boxed")).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("current"))),
							jen.Case(jen.Id("types").Dot("Ref")).Block(jen.Id("i").Dot("retain").Call(jen.Id("int").Call(jen.Id("current"))))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("box")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func errorCode() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("box")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("box").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("e"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("box").Dot("Ref").Call()).Assert(jen.Op("*").Add(jen.Id("types").Dot("Error")))),
			jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("code")).Op(":=").List(jen.Id("e").Dot("Code").Call()),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("box")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("code")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func errorGet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("box")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("box").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("e"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("box").Dot("Ref").Call()).Assert(jen.Op("*").Add(jen.Id("types").Dot("Error")))),
			jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("e").Dot("Value").Call()),
			jen.Id("i").Dot("retainBox").Call(jen.Id("val")),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("box")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func errorNew() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("code")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("code").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindI32"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("payload")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("alloc").Call(jen.Id("types").Dot("NewError").Call(jen.Id("types").Dot("ErrorCode").Call(jen.Id("code").Dot("I32").Call()), jen.Id("i").Dot("message").Call(jen.Id("payload")), jen.Id("payload")))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("addr"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Abs() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Abs").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Ceil() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Ceil").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Floor() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Floor").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Nearest() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "RoundToEven").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Neg() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Op("-").Add(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ReinterpretI32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Qual("math", "Float32frombits").Call(jen.Id("uint32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Sqrt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Sqrt").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToF64() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToI32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("int32")),
				jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("float64").Call(jen.Id("v")))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
					jen.Case(jen.Id("float64").Call(jen.Id("v")).Op(">=").Add(jen.Lit(2147483648.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxInt32"))),
					jen.Case(jen.Id("float64").Call(jen.Id("v")).Op("<").Add(jen.Op("-").Add(jen.Lit(2147483648.0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MinInt32"))),
					jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("int32").Call(jen.Id("float64").Call(jen.Id("v")))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("result")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToI32U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("uint32")),
				jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("float64").Call(jen.Id("v"))).Op("||").Add(jen.Id("float64").Call(jen.Id("v")).Op("<").Add(jen.Lit(0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
					jen.Case(jen.Id("float64").Call(jen.Id("v")).Op(">=").Add(jen.Lit(4294967296.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxUint32"))),
					jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("uint32").Call(jen.Id("float64").Call(jen.Id("v")))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("result"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToI64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("int64")),
			jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("float64").Call(jen.Id("v")))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
				jen.Case(jen.Id("float64").Call(jen.Id("v")).Op(">=").Add(jen.Lit(9223372036854775808.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxInt64"))),
				jen.Case(jen.Id("float64").Call(jen.Id("v")).Op("<").Add(jen.Op("-").Add(jen.Lit(9223372036854775808.0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MinInt64"))),
				jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("int64").Call(jen.Id("v"))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("result"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToI64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("uint64")),
			jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("float64").Call(jen.Id("v"))).Op("||").Add(jen.Id("float64").Call(jen.Id("v")).Op("<").Add(jen.Lit(0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
				jen.Case(jen.Id("float64").Call(jen.Id("v")).Op(">=").Add(jen.Lit(18446744073709551616.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxUint64"))),
				jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("uint64").Call(jen.Id("v"))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("result")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Trunc() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Trunc").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Abs() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Abs").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Ceil() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Ceil").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Floor() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Floor").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Nearest() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "RoundToEven").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Neg() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Op("-").Add(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ReinterpretI64() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Float64frombits").Call(jen.Id("uint64").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Sqrt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Sqrt").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToF32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToI32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("int32")),
				jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("v"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
					jen.Case(jen.Id("v").Op(">=").Add(jen.Lit(2147483648.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxInt32"))),
					jen.Case(jen.Id("v").Op("<").Add(jen.Op("-").Add(jen.Lit(2147483648.0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MinInt32"))),
					jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("int32").Call(jen.Id("v"))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("result")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToI32U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("uint32")),
				jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("v")).Op("||").Add(jen.Id("v").Op("<").Add(jen.Lit(0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
					jen.Case(jen.Id("v").Op(">=").Add(jen.Lit(4294967296.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxUint32"))),
					jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("uint32").Call(jen.Id("v"))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("result"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToI64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("int64")),
			jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("v"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
				jen.Case(jen.Id("v").Op(">=").Add(jen.Lit(9223372036854775808.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxInt64"))),
				jen.Case(jen.Id("v").Op("<").Add(jen.Op("-").Add(jen.Lit(9223372036854775808.0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MinInt64"))),
				jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("int64").Call(jen.Id("v"))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("result"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToI64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("uint64")),
			jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("v")).Op("||").Add(jen.Id("v").Op("<").Add(jen.Lit(0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
				jen.Case(jen.Id("v").Op(">=").Add(jen.Lit(18446744073709551616.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxUint64"))),
				jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("uint64").Call(jen.Id("v"))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("result")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Trunc() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Trunc").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func globalSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("globals")))).Block(jen.Switch(jen.Id("c").Dot("globals").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("globals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("globals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func globalTee() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("globals")))).Block(jen.Switch(jen.Id("c").Dot("globals").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("globals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("globals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("val")),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))).Op("=").List(jen.Id("val")),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func i32Clz() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Qual("math/bits", "LeadingZeros32").Call(jen.Id("uint32").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Ctz() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Qual("math/bits", "TrailingZeros32").Call(jen.Id("uint32").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Extend16S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("int16").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Extend8S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("int8").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Popcnt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Qual("math/bits", "OnesCount32").Call(jen.Id("uint32").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ReinterpretF32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Qual("math", "Float32bits").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToF32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToF32U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("uint32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToF64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToF64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("uint32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToI64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToI64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("uint32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Clz() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Qual("math/bits", "LeadingZeros64").Call(jen.Id("uint64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Ctz() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Qual("math/bits", "TrailingZeros64").Call(jen.Id("uint64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Extend16S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("int16").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Extend32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("int32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Extend8S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("int8").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Popcnt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Qual("math/bits", "OnesCount64").Call(jen.Id("uint64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ReinterpretF64() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Qual("math", "Float64bits").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToF32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToF32U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("uint64").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToF64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToF64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("uint64").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToI32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func localSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(2)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("locals")))).Block(jen.Switch(jen.Id("c").Dot("locals").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("fr").Dot("bp").Op("+").Add(jen.Id("idx"))),
			jen.If(jen.Id("addr").Op(">=").Add(jen.Id("i").Dot("sp"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("fr").Dot("bp").Op("+").Add(jen.Id("idx"))),
			jen.If(jen.Id("addr").Op(">=").Add(jen.Id("i").Dot("sp"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2)))))
}

func localTee() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(2)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("locals")))).Block(jen.Switch(jen.Id("c").Dot("locals").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("fr").Dot("bp").Op("+").Add(jen.Id("idx"))),
			jen.If(jen.Id("addr").Op(">=").Add(jen.Id("i").Dot("sp"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("fr").Dot("bp").Op("+").Add(jen.Id("idx"))),
			jen.If(jen.Id("addr").Op(">=").Add(jen.Id("i").Dot("sp"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("val")),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))).Op("=").List(jen.Id("val")),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2)))))
}

func mapClear() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("m")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8")))).Block(jen.Id("m").Dot("Clear").Call(jen.Func().Params(jen.Id("value").Add(jen.Id("types").Dot("Boxed"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value"))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool")))).Block(jen.Id("m").Dot("Clear").Call(jen.Func().Params(jen.Id("value").Add(jen.Id("types").Dot("Boxed"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value"))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32")))).Block(jen.Id("m").Dot("Clear").Call(jen.Func().Params(jen.Id("value").Add(jen.Id("types").Dot("Boxed"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value"))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64")))).Block(jen.Id("m").Dot("Clear").Call(jen.Func().Params(jen.Id("value").Add(jen.Id("types").Dot("Boxed"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value"))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32")))).Block(jen.Id("m").Dot("Clear").Call(jen.Func().Params(jen.Id("value").Add(jen.Id("types").Dot("Boxed"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value"))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64")))).Block(jen.Id("m").Dot("Clear").Call(jen.Func().Params(jen.Id("value").Add(jen.Id("types").Dot("Boxed"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("value"))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(jen.Id("m").Dot("Clear").Call(jen.Func().Params(jen.Id("entry").Add(jen.Id("types").Dot("MapEntry"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("entry").Dot("Key")),
					jen.Id("i").Dot("releaseBox").Call(jen.Id("entry").Dot("Value"))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func mapDelete() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("key")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("m")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Delete").Call(jen.Id("key").Dot("I8").Call())),
				jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Delete").Call(jen.Id("key").Dot("Bool").Call())),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Delete").Call(jen.Id("key").Dot("I32").Call())),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Delete").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Delete").Call(jen.Id("key").Dot("F32").Call())),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Delete").Call(jen.Id("key").Dot("F64").Call())),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(jen.Var().Add(jen.List(jen.Id("k"))).Add(jen.Id("types").Dot("MapKey")),
					jen.List(jen.Id("keyRef")).Op(":=").List(jen.Lit(0)),
					jen.List(jen.Id("drop")).Op(":=").List(jen.Id("false")),
					jen.Switch(jen.Id("key").Dot("Kind").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI32"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("uint32").Call(jen.Id("key").Dot("I32").Call()))}))),
						jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))),
						jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float32bits").Call(jen.Id("key").Dot("F32").Call())),
							jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(31)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF32"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("bits"))}))),
						jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float64bits").Call(jen.Id("key").Dot("F64").Call())),
							jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(63)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF64"), jen.Id("Bits"): jen.Id("bits")}))),
						jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("keyRef")).Op("=").List(jen.Id("key").Dot("Ref").Call()),
							jen.If(jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("keyRef")).Assert(jen.Id("types").Dot("I64"))), jen.Id("ok")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))).Else().Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindRef"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("keyRef"))})),
								jen.List(jen.Id("drop")).Op("=").List(jen.Id("true")))),
						jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
					jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Delete").Call(jen.Id("k"))),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old").Dot("Key")),
						jen.Id("i").Dot("releaseBox").Call(jen.Id("old").Dot("Value"))),
					jen.If(jen.Id("drop")).Block(jen.Id("i").Dot("release").Call(jen.Id("keyRef")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(2)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func mapGet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("key")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("types").Dot("Boxed")),
			jen.Switch(jen.List(jen.Id("m")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8")))).Block(jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("I8").Call())),
				jen.If(jen.Id("ok")).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("value"))).Else().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool")))).Block(jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("Bool").Call())),
					jen.If(jen.Id("ok")).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("value"))).Else().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32")))).Block(jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("I32").Call())),
					jen.If(jen.Id("ok")).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("value"))).Else().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64")))).Block(jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Get").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))),
					jen.If(jen.Id("ok")).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("value"))).Else().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32")))).Block(jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("F32").Call())),
					jen.If(jen.Id("ok")).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("value"))).Else().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64")))).Block(jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("F64").Call())),
					jen.If(jen.Id("ok")).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("value"))).Else().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(jen.Var().Add(jen.List(jen.Id("k"))).Add(jen.Id("types").Dot("MapKey")),
					jen.List(jen.Id("keyRef")).Op(":=").List(jen.Lit(0)),
					jen.List(jen.Id("drop")).Op(":=").List(jen.Id("false")),
					jen.Switch(jen.Id("key").Dot("Kind").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI32"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("uint32").Call(jen.Id("key").Dot("I32").Call()))}))),
						jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))),
						jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float32bits").Call(jen.Id("key").Dot("F32").Call())),
							jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(31)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF32"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("bits"))}))),
						jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float64bits").Call(jen.Id("key").Dot("F64").Call())),
							jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(63)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF64"), jen.Id("Bits"): jen.Id("bits")}))),
						jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("keyRef")).Op("=").List(jen.Id("key").Dot("Ref").Call()),
							jen.If(jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("keyRef")).Assert(jen.Id("types").Dot("I64"))), jen.Id("ok")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))).Else().Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindRef"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("keyRef"))})),
								jen.List(jen.Id("drop")).Op("=").List(jen.Id("true")))),
						jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
					jen.List(jen.Id("entry"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Get").Call(jen.Id("k"))),
					jen.If(jen.Id("drop")).Block(jen.Id("i").Dot("release").Call(jen.Id("keyRef"))),
					jen.If(jen.Id("ok")).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("entry").Dot("Value"))).Else().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("retainBox").Call(jen.Id("result")),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("result")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func mapIter() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8"))), jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool"))), jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32"))), jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64"))), jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32"))), jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64"))), jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("iter")).Op(":=").List(jen.Id("types").Dot("NewMapIterator").Call(jen.Id("types").Dot("Ref").Call(jen.Id("addr")), jen.Id("i").Dot("heap").Index(jen.Id("addr")))),
			jen.Id("iter").Dot("Next").Call(),
			jen.If(jen.Op("!").Add(jen.Id("iter").Dot("Done").Call())).Block(jen.List(jen.Id("current")).Op(":=").List(jen.Id("iter").Dot("Current").Call()),
				jen.Switch(jen.List(jen.Id("current")).Op(":=").List(jen.Id("current").Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("Boxed")).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("current"))),
					jen.Case(jen.Id("types").Dot("Ref")).Block(jen.Id("i").Dot("retain").Call(jen.Id("int").Call(jen.Id("current")))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("iter")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func mapKeys() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Var().Add(jen.List(jen.Id("keyType"))).Add(jen.Id("types").Dot("Type")),
			jen.Var().Add(jen.List(jen.Id("elems"))).Add(jen.Index().Add(jen.Id("types").Dot("Boxed"))),
			jen.Switch(jen.List(jen.Id("m")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8")))).Block(jen.List(jen.Id("keyType")).Op("=").List(jen.Id("m").Dot("Typ").Dot("Key")),
				jen.List(jen.Id("elems")).Op("=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Lit(0), jen.Id("m").Dot("Len").Call())),
				jen.Id("m").Dot("Range").Call(jen.Func().Params(jen.Id("k").Add(jen.Id("int8")), jen.Id("_").Add(jen.Id("types").Dot("Boxed"))).Block(jen.List(jen.Id("elems")).Op("=").List(jen.Id("append").Call(jen.Id("elems"), jen.Id("types").Dot("BoxI8").Call(jen.Id("k"))))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool")))).Block(jen.List(jen.Id("keyType")).Op("=").List(jen.Id("m").Dot("Typ").Dot("Key")),
					jen.List(jen.Id("elems")).Op("=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Lit(0), jen.Id("m").Dot("Len").Call())),
					jen.Id("m").Dot("Range").Call(jen.Func().Params(jen.Id("k").Add(jen.Id("bool")), jen.Id("_").Add(jen.Id("types").Dot("Boxed"))).Block(jen.List(jen.Id("elems")).Op("=").List(jen.Id("append").Call(jen.Id("elems"), jen.Id("types").Dot("BoxI1").Call(jen.Id("k"))))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32")))).Block(jen.List(jen.Id("keyType")).Op("=").List(jen.Id("m").Dot("Typ").Dot("Key")),
					jen.List(jen.Id("elems")).Op("=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Lit(0), jen.Id("m").Dot("Len").Call())),
					jen.Id("m").Dot("Range").Call(jen.Func().Params(jen.Id("k").Add(jen.Id("int32")), jen.Id("_").Add(jen.Id("types").Dot("Boxed"))).Block(jen.List(jen.Id("elems")).Op("=").List(jen.Id("append").Call(jen.Id("elems"), jen.Id("types").Dot("BoxI32").Call(jen.Id("k"))))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64")))).Block(jen.List(jen.Id("keyType")).Op("=").List(jen.Id("m").Dot("Typ").Dot("Key")),
					jen.List(jen.Id("elems")).Op("=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Lit(0), jen.Id("m").Dot("Len").Call())),
					jen.Id("m").Dot("Range").Call(jen.Func().Params(jen.Id("k").Add(jen.Id("int64")), jen.Id("_").Add(jen.Id("types").Dot("Boxed"))).Block(jen.List(jen.Id("elems")).Op("=").List(jen.Id("append").Call(jen.Id("elems"), jen.Id("i").Dot("boxI64").Call(jen.Id("k"))))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32")))).Block(jen.List(jen.Id("keyType")).Op("=").List(jen.Id("m").Dot("Typ").Dot("Key")),
					jen.List(jen.Id("elems")).Op("=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Lit(0), jen.Id("m").Dot("Len").Call())),
					jen.Id("m").Dot("Range").Call(jen.Func().Params(jen.Id("k").Add(jen.Id("float32")), jen.Id("_").Add(jen.Id("types").Dot("Boxed"))).Block(jen.List(jen.Id("elems")).Op("=").List(jen.Id("append").Call(jen.Id("elems"), jen.Id("types").Dot("BoxF32").Call(jen.Id("k"))))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64")))).Block(jen.List(jen.Id("keyType")).Op("=").List(jen.Id("m").Dot("Typ").Dot("Key")),
					jen.List(jen.Id("elems")).Op("=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Lit(0), jen.Id("m").Dot("Len").Call())),
					jen.Id("m").Dot("Range").Call(jen.Func().Params(jen.Id("k").Add(jen.Id("float64")), jen.Id("_").Add(jen.Id("types").Dot("Boxed"))).Block(jen.List(jen.Id("elems")).Op("=").List(jen.Id("append").Call(jen.Id("elems"), jen.Id("types").Dot("BoxF64").Call(jen.Id("k"))))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(jen.List(jen.Id("keyType")).Op("=").List(jen.Id("m").Dot("Typ").Dot("Key")),
					jen.List(jen.Id("elems")).Op("=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Lit(0), jen.Id("m").Dot("Len").Call())),
					jen.Id("m").Dot("Range").Call(jen.Func().Params(jen.Id("_").Add(jen.Id("types").Dot("MapKey")), jen.Id("entry").Add(jen.Id("types").Dot("MapEntry"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("entry").Dot("Key")),
						jen.List(jen.Id("elems")).Op("=").List(jen.Id("append").Call(jen.Id("elems"), jen.Id("entry").Dot("Key")))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("newArray").Call(jen.Id("types").Dot("NewArrayType").Call(jen.Id("keyType")), jen.Id("elems"))),
			jen.List(jen.Id("out")).Op(":=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("arr")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("out")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func mapLen() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.List(jen.Id("n")).Op(":=").List(jen.Lit(0)),
			jen.Switch(jen.List(jen.Id("m")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8")))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("m").Dot("Len").Call())),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool")))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("m").Dot("Len").Call())),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32")))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("m").Dot("Len").Call())),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64")))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("m").Dot("Len").Call())),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32")))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("m").Dot("Len").Call())),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64")))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("m").Dot("Len").Call())),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("m").Dot("Len").Call())),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("n")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func mapLookup() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("key")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("types").Dot("Boxed")),
			jen.Var().Add(jen.List(jen.Id("found"))).Add(jen.Id("bool")),
			jen.Switch(jen.List(jen.Id("m")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8")))).Block(jen.List(jen.Id("result"), jen.Id("found")).Op("=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("I8").Call())),
				jen.If(jen.Op("!").Add(jen.Id("found"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool")))).Block(jen.List(jen.Id("result"), jen.Id("found")).Op("=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("Bool").Call())),
					jen.If(jen.Op("!").Add(jen.Id("found"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32")))).Block(jen.List(jen.Id("result"), jen.Id("found")).Op("=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("I32").Call())),
					jen.If(jen.Op("!").Add(jen.Id("found"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64")))).Block(jen.List(jen.Id("result"), jen.Id("found")).Op("=").List(jen.Id("m").Dot("Get").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))),
					jen.If(jen.Op("!").Add(jen.Id("found"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32")))).Block(jen.List(jen.Id("result"), jen.Id("found")).Op("=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("F32").Call())),
					jen.If(jen.Op("!").Add(jen.Id("found"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64")))).Block(jen.List(jen.Id("result"), jen.Id("found")).Op("=").List(jen.Id("m").Dot("Get").Call(jen.Id("key").Dot("F64").Call())),
					jen.If(jen.Op("!").Add(jen.Id("found"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(jen.Var().Add(jen.List(jen.Id("k"))).Add(jen.Id("types").Dot("MapKey")),
					jen.List(jen.Id("keyRef")).Op(":=").List(jen.Lit(0)),
					jen.List(jen.Id("drop")).Op(":=").List(jen.Id("false")),
					jen.Switch(jen.Id("key").Dot("Kind").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI32"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("uint32").Call(jen.Id("key").Dot("I32").Call()))}))),
						jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))),
						jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float32bits").Call(jen.Id("key").Dot("F32").Call())),
							jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(31)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF32"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("bits"))}))),
						jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float64bits").Call(jen.Id("key").Dot("F64").Call())),
							jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(63)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF64"), jen.Id("Bits"): jen.Id("bits")}))),
						jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("keyRef")).Op("=").List(jen.Id("key").Dot("Ref").Call()),
							jen.If(jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("keyRef")).Assert(jen.Id("types").Dot("I64"))), jen.Id("ok")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))).Else().Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindRef"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("keyRef"))})),
								jen.List(jen.Id("drop")).Op("=").List(jen.Id("true")))),
						jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
					jen.List(jen.Id("entry"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Get").Call(jen.Id("k"))),
					jen.If(jen.Id("drop")).Block(jen.Id("i").Dot("release").Call(jen.Id("keyRef"))),
					jen.List(jen.Id("found")).Op("=").List(jen.Id("ok")),
					jen.If(jen.Id("ok")).Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("entry").Dot("Value"))).Else().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("m").Dot("Zero")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("retainBox").Call(jen.Id("result")),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))).Op("=").List(jen.Id("result")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("found"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func mapNew() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("MapType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
			jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("*").Add(jen.Lit(2)).Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("m")).Op(":=").List(jen.Id("types").Dot("NewMapForType").Call(jen.Id("typ"), jen.Id("size"))),
			jen.List(jen.Id("base")).Op(":=").List(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)).Op("-").Add(jen.Id("size").Op("*").Add(jen.Lit(2)))),
			jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("key")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("j").Op("*").Add(jen.Lit(2))))),
				jen.List(jen.Id("value")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("j").Op("*").Add(jen.Lit(2))).Op("+").Add(jen.Lit(1)))),
				jen.Switch(jen.List(jen.Id("m")).Op(":=").List(jen.Id("m").Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("I8").Call(), jen.Id("value"))),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
					jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("Bool").Call(), jen.Id("value"))),
						jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
					jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("I32").Call(), jen.Id("value"))),
						jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
					jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")), jen.Id("value"))),
						jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
					jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("F32").Call(), jen.Id("value"))),
						jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
					jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("F64").Call(), jen.Id("value"))),
						jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
					jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(jen.Var().Add(jen.List(jen.Id("k"))).Add(jen.Id("types").Dot("MapKey")),
						jen.List(jen.Id("entry")).Op(":=").List(jen.Id("types").Dot("MapEntry").Values(jen.Dict{jen.Id("Value"): jen.Id("value")})),
						jen.List(jen.Id("keyRef")).Op(":=").List(jen.Lit(0)),
						jen.List(jen.Id("drop")).Op(":=").List(jen.Id("false")),
						jen.Switch(jen.Id("key").Dot("Kind").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Id("uint64").Call(jen.Id("uint32").Call(jen.Id("key").Dot("I32").Call()))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI32"), jen.Id("Bits"): jen.Id("bits")})),
							jen.List(jen.Id("entry").Dot("Key")).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("bits"))))),
							jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))),
							jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float32bits").Call(jen.Id("key").Dot("F32").Call())),
								jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(31)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
								jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF32"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("bits"))})),
								jen.List(jen.Id("entry").Dot("Key")).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Qual("math", "Float32frombits").Call(jen.Id("bits"))))),
							jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float64bits").Call(jen.Id("key").Dot("F64").Call())),
								jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(63)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
								jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF64"), jen.Id("Bits"): jen.Id("bits")})),
								jen.List(jen.Id("entry").Dot("Key")).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Float64frombits").Call(jen.Id("bits"))))),
							jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("keyRef")).Op("=").List(jen.Id("key").Dot("Ref").Call()),
								jen.If(jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("keyRef")).Assert(jen.Id("types").Dot("I64"))), jen.Id("ok")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))).Else().Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindRef"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("keyRef"))})),
									jen.List(jen.Id("entry").Dot("Key")).Op("=").List(jen.Id("key")),
									jen.List(jen.Id("drop")).Op("=").List(jen.Id("true")))),
							jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
						jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("k"), jen.Id("entry"))),
						jen.If(jen.Id("ok")).Block(jen.If(jen.Id("drop")).Block(jen.Id("i").Dot("release").Call(jen.Id("keyRef"))),
							jen.Id("i").Dot("releaseBox").Call(jen.Id("old").Dot("Value")))),
					jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
			jen.Var().Add(jen.List(jen.Id("addr"))).Add(jen.Id("int")),
			jen.If(jen.Id("typ").Dot("TraceKeys").Op("||").Add(jen.Id("typ").Dot("TraceValues"))).Block(jen.List(jen.Id("addr")).Op("=").List(jen.Id("i").Dot("alloc").Call(jen.Id("m")))).Else().Block(jen.List(jen.Id("addr")).Op("=").List(jen.Id("i").Dot("alloc").Call(jen.Id("m")))),
			jen.List(jen.Id("i").Dot("sp")).Op("=").List(jen.Id("base").Op("+").Add(jen.Lit(1))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("base"))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("addr"))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func mapNewDefault() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("MapType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("capacity")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.If(jen.Id("capacity").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("types").Dot("NewMapForType").Call(jen.Id("typ"), jen.Id("capacity"))))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func mapSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(3))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("value")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("key")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("m")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int8")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("I8").Call(), jen.Id("value"))),
				jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("bool")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("Bool").Call(), jen.Id("value"))),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int32")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("I32").Call(), jen.Id("value"))),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("int64")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")), jen.Id("value"))),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float32")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("F32").Call(), jen.Id("value"))),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("TypedMap").Index(jen.Id("float64")))).Block(jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("key").Dot("F64").Call(), jen.Id("value"))),
					jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Map"))).Block(jen.Var().Add(jen.List(jen.Id("k"))).Add(jen.Id("types").Dot("MapKey")),
					jen.List(jen.Id("entry")).Op(":=").List(jen.Id("types").Dot("MapEntry").Values(jen.Dict{jen.Id("Value"): jen.Id("value")})),
					jen.List(jen.Id("keyRef")).Op(":=").List(jen.Lit(0)),
					jen.List(jen.Id("drop")).Op(":=").List(jen.Id("false")),
					jen.Switch(jen.Id("key").Dot("Kind").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Id("uint64").Call(jen.Id("uint32").Call(jen.Id("key").Dot("I32").Call()))),
						jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI32"), jen.Id("Bits"): jen.Id("bits")})),
						jen.List(jen.Id("entry").Dot("Key")).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("bits"))))),
						jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))),
						jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float32bits").Call(jen.Id("key").Dot("F32").Call())),
							jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(31)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF32"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("bits"))})),
							jen.List(jen.Id("entry").Dot("Key")).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Qual("math", "Float32frombits").Call(jen.Id("bits"))))),
						jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.List(jen.Id("bits")).Op(":=").List(jen.Qual("math", "Float64bits").Call(jen.Id("key").Dot("F64").Call())),
							jen.If(jen.Id("bits").Op("==").Add(jen.Lit(1).Op("<<").Add(jen.Lit(63)))).Block(jen.List(jen.Id("bits")).Op("=").List(jen.Lit(0))),
							jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindF64"), jen.Id("Bits"): jen.Id("bits")})),
							jen.List(jen.Id("entry").Dot("Key")).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Float64frombits").Call(jen.Id("bits"))))),
						jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("keyRef")).Op("=").List(jen.Id("key").Dot("Ref").Call()),
							jen.If(jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("keyRef")).Assert(jen.Id("types").Dot("I64"))), jen.Id("ok")).Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindI64"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("key")))}))).Else().Block(jen.List(jen.Id("k")).Op("=").List(jen.Id("types").Dot("MapKey").Values(jen.Dict{jen.Id("Kind"): jen.Id("types").Dot("KindRef"), jen.Id("Bits"): jen.Id("uint64").Call(jen.Id("keyRef"))})),
								jen.List(jen.Id("entry").Dot("Key")).Op("=").List(jen.Id("key")),
								jen.List(jen.Id("drop")).Op("=").List(jen.Id("true")))),
						jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
					jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").List(jen.Id("m").Dot("Set").Call(jen.Id("k"), jen.Id("entry"))),
					jen.If(jen.Id("ok")).Block(jen.If(jen.Id("drop")).Block(jen.Id("i").Dot("release").Call(jen.Id("keyRef"))),
						jen.Id("i").Dot("releaseBox").Call(jen.Id("old").Dot("Value")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(3)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func nop() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("skip")).Op(":=").List(jen.Lit(0)),
		jen.For(jen.Op("!").Add(jen.Id("c").Dot("exact")).Op("&&").Add(jen.Id("c").Dot("ip").Op("+").Add(jen.Id("skip")).Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("code")))).Op("&&").Add(jen.Id("instr").Dot("Opcode").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Id("skip")))).Op("==").Add(jen.Id("instr").Dot("NOP")))).Block(jen.Id("skip").Op("++")),
		jen.If(jen.Id("c").Dot("exact")).Block(jen.List(jen.Id("skip")).Op("=").List(jen.Lit(1))),
		jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Id("skip")))))
}

func refCast() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx"))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Switch(jen.Id("kind").Op(":=").Id("val").Dot("Kind").Call(), jen.Id("kind")).Block(jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("val").Dot("Ref").Call())),
				jen.If(jen.Op("!").Add(jen.Id("typ").Dot("Cast").Call(jen.Id("ref").Dot("Type").Call()))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
				jen.Default().Block(jen.If(jen.Op("!").Add(jen.Id("typ").Dot("Cast").Call(jen.Id("val").Dot("Type").Call()))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func refEq() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("==").Add(jen.Id("v1")))),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("v1")),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("v2")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func refGet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Var().Add(jen.List(jen.Id("val"))).Add(jen.Id("types").Dot("Boxed")),
			jen.Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
				jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
				jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
				jen.Switch(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(jen.Case(
					jen.Id("types").Dot("I1"), jen.Id("types").Dot("I8"),
					jen.Id("types").Dot("I32"), jen.Id("types").Dot("I64"),
					jen.Id("types").Dot("F32"), jen.Id("types").Dot("F64"),
				).Block(),
					jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
				jen.List(jen.Id("result")).Op(":=").List(jen.Id("i").Dot("box").Call(jen.Id("i").Dot("heap").Index(jen.Id("addr")))),
				jen.Id("i").Dot("release").Call(jen.Id("addr")),
				jen.Id("i").Dot("sp").Op("--"),
				jen.List(jen.Id("val")).Op("=").List(jen.Id("result"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp"))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("sp").Op("++"),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func refNe() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("!=").Add(jen.Id("v1")))),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("v1")),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("v2")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func refNew() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("v").Dot("Kind").Call().Op("==").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("types").Dot("Unbox").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func refSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("value")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("value").Dot("Kind").Call().Op("==").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
				jen.Case(
					jen.Id("types").Dot("I1"), jen.Id("types").Dot("I8"),
					jen.Id("types").Dot("I32"), jen.Id("types").Dot("I64"),
					jen.Id("types").Dot("F32"), jen.Id("types").Dot("F64"),
				),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			),
			jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("types").Dot("Unbox").Call(jen.Id("value"))),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(2)),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func refTest() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx"))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Var().Add(jen.List(jen.Id("cond"))).Add(jen.Id("types").Dot("Boxed")),
			jen.Switch(jen.Id("kind").Op(":=").Id("val").Dot("Kind").Call(), jen.Id("kind")).Block(jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("val").Dot("Ref").Call())),
				jen.List(jen.Id("cond")).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("typ").Dot("Equals").Call(jen.Id("ref").Dot("Type").Call())))),
				jen.Default().Block(jen.List(jen.Id("cond")).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("typ").Dot("Kind").Call().Op("==").Add(jen.Id("kind")))))),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("val")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("cond")),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func resume() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("i").Dot("fp").Op("==").Add(jen.Id("len").Call(jen.Id("i").Dot("frames")))).Block(jen.Id("panic").Call(jen.Id("ErrFrameOverflow"))),
			jen.List(jen.Id("in")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("box")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("box").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("coAddr")).Op(":=").List(jen.Id("box").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("co")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("coAddr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("coroutine"))).Block(jen.Block(jen.List(jen.Id("coAddr")).Op(":=").List(jen.Id("coAddr")),
				jen.List(jen.Id("co")).Op(":=").List(jen.Id("co")),
				jen.List(jen.Id("in")).Op(":=").List(jen.Id("in")),
				jen.If(jen.Id("co").Dot("done")).Block(jen.Id("panic").Call(jen.Id("ErrCoroutineDone"))),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(2)),
				jen.List(jen.Id("base")).Op(":=").List(jen.Id("i").Dot("sp")),
				jen.If(jen.Id("base").Op("+").Add(jen.Id("len").Call(jen.Id("co").Dot("image"))).Op("+").Add(jen.Lit(1)).Op(">").Add(jen.Id("len").Call(jen.Id("i").Dot("stack")))).Block(jen.Id("panic").Call(jen.Id("ErrStackOverflow"))),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("co").Dot("value")),
				jen.List(jen.Id("co").Dot("value")).Op("=").List(jen.Id("types").Dot("Boxed").Call(jen.Lit(0))),
				jen.Id("copy").Call(jen.Id("i").Dot("stack").Index(jen.Id("base").Op(":").Add(jen.Empty())), jen.Id("co").Dot("image")),
				jen.List(jen.Id("i").Dot("sp")).Op("=").List(jen.Id("base").Op("+").Add(jen.Id("len").Call(jen.Id("co").Dot("image")))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp"))).Op("=").List(jen.Id("in")),
				jen.Id("i").Dot("sp").Op("++"),
				jen.List(jen.Id("f")).Op(":=").List(jen.Op("&").Add(jen.Id("i").Dot("frames").Index(jen.Id("i").Dot("fp")))),
				jen.List(jen.Id("f").Dot("code")).Op("=").List(jen.Id("i").Dot("code").Index(jen.Id("co").Dot("addr"))),
				jen.List(jen.Id("f").Dot("upvals")).Op("=").List(jen.Id("co").Dot("upvals")),
				jen.List(jen.Id("f").Dot("addr")).Op("=").List(jen.Id("co").Dot("addr")),
				jen.List(jen.Id("f").Dot("ref")).Op("=").List(jen.Id("co").Dot("ref")),
				jen.List(jen.Id("f").Dot("returns")).Op("=").List(jen.Id("co").Dot("returns")),
				jen.List(jen.Id("f").Dot("release")).Op("=").List(jen.Id("co").Dot("release")),
				jen.List(jen.Id("f").Dot("ip")).Op("=").List(jen.Id("co").Dot("ip")),
				jen.List(jen.Id("f").Dot("bp")).Op("=").List(jen.Id("base")),
				jen.List(jen.Id("f").Dot("coro")).Op("=").List(jen.Id("coAddr")),
				jen.List(jen.Id("co").Dot("image")).Op("=").List(jen.Id("co").Dot("image").Index(jen.Empty().Op(":").Add(jen.Lit(0)))),
				jen.List(jen.Id("co").Dot("upvals")).Op("=").List(jen.Id("nil")),
				jen.List(jen.Id("co").Dot("ref")).Op("=").List(jen.Lit(0)),
				jen.List(jen.Id("co").Dot("release")).Op("=").List(jen.Id("false")),
				jen.Id("i").Dot("fr").Dot("ip").Op("++"),
				jen.Id("i").Dot("fp").Op("++"),
				jen.List(jen.Id("i").Dot("fr")).Op("=").List(jen.Id("f")))),
				jen.Case(jen.Id("types").Dot("Iterator")).Block(jen.Block(jen.List(jen.Id("iter")).Op(":=").List(jen.Id("co")),
					jen.List(jen.Id("in")).Op(":=").List(jen.Id("in")),
					jen.If(jen.Id("iter").Dot("Done").Call()).Block(jen.Id("panic").Call(jen.Id("ErrCoroutineDone"))),
					jen.Id("i").Dot("releaseBox").Call(jen.Id("in")),
					jen.Block(jen.List(jen.Id("iter")).Op(":=").List(jen.Id("iter")),
						jen.If(jen.Id("iter").Dot("Done").Call()).Block(jen.Goto().Id("inlineReleaseiteratorcurrent7")),
						jen.If(jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").List(jen.Id("iter").Assert(jen.Op("*").Add(jen.Id("types").Dot("MapIterator")))), jen.Id("ok")).Block(jen.Block(jen.List(jen.Id("val")).Op(":=").List(jen.Id("iter").Dot("Current").Call()),
							jen.Switch(jen.List(jen.Id("val")).Op(":=").List(jen.Id("val").Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("Boxed")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("val"))),
								jen.Case(jen.Id("types").Dot("Ref")).Block(jen.Id("i").Dot("release").Call(jen.Id("int").Call(jen.Id("val"))))))),
						jen.Id("inlineReleaseiteratorcurrent7").Op(":").Add(jen.Null())),
					jen.Id("iter").Dot("Next").Call(),
					jen.Block(jen.List(jen.Id("iter")).Op(":=").List(jen.Id("iter")),
						jen.If(jen.Id("iter").Dot("Done").Call()).Block(jen.Goto().Id("inlineRetainiteratorcurrent8")),
						jen.If(jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").List(jen.Id("iter").Assert(jen.Op("*").Add(jen.Id("types").Dot("MapIterator")))), jen.Id("ok")).Block(jen.Block(jen.List(jen.Id("val")).Op(":=").List(jen.Id("iter").Dot("Current").Call()),
							jen.Switch(jen.List(jen.Id("val")).Op(":=").List(jen.Id("val").Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("Boxed")).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("val"))),
								jen.Case(jen.Id("types").Dot("Ref")).Block(jen.Id("i").Dot("retain").Call(jen.Id("int").Call(jen.Id("val"))))))),
						jen.Id("inlineRetainiteratorcurrent8").Op(":").Add(jen.Null())),
					jen.Id("i").Dot("sp").Op("--"),
					jen.Id("i").Dot("fr").Dot("ip").Op("++"))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))))))
}

func returnOp() jen.Code {
	return jen.Func().
		Params(jen.Id("c").Op("*").Id("threader")).
		Params(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter"))).
		Block(
			jen.Id("c").Dot("ip").Op("++"),
			jen.Comment("A frame whose every slot is a plain scalar can only be holding"),
			jen.Comment("scalars when its operand stack is balanced, so the release sweep"),
			jen.Comment("is skipped for it. The kinds that can carry a ref are the same ones"),
			jen.Comment("LOCAL_GET retains for."),
			jen.Id("slots").Op(":=").Len(jen.Id("c").Dot("locals")),
			jen.Id("owned").Op(":=").False(),
			jen.For(jen.List(jen.Id("_"), jen.Id("kind")).Op(":=").Range().Id("c").Dot("locals")).Block(
				jen.Switch(jen.Id("kind").Dot("Repr").Call()).Block(
					jen.Case(
						jen.Qual("github.com/siyul-park/minivm/types", "KindI32"),
						jen.Qual("github.com/siyul-park/minivm/types", "KindF32"),
						jen.Qual("github.com/siyul-park/minivm/types", "KindF64"),
					).Block(),
					jen.Default().Block(jen.Id("owned").Op("=").True()),
				),
			),
			jen.Return(
				jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(
					jen.If(jen.Id("i").Dot("fp").Op("==").Lit(1)).Block(jen.Panic(jen.Id("ErrFrameUnderflow"))),
					jen.Block(retire(jen.Id("owned").Op("||").Id("i").Dot("sp").Op("!=").Id("f").Dot("bp").Op("+").Id("slots").Op("+").Id("f").Dot("returns"))...),
				),
			),
		)
}

func selectOp() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(3))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("cond")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.List(jen.Id("selected")).Op(":=").List(jen.Id("v1")),
			jen.List(jen.Id("discarded")).Op(":=").List(jen.Id("v2")),
			jen.If(jen.Id("cond").Op("==").Add(jen.Lit(0))).Block(jen.List(jen.Id("selected")).Op("=").List(jen.Id("v2")),
				jen.List(jen.Id("discarded")).Op("=").List(jen.Id("v1"))),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("discarded")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))).Op("=").List(jen.Id("selected")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(2)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringConcat() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("int").Call(jen.Id("i").Dot("intern").Call(jen.Id("string").Call(jen.Id("v2").Op("+").Add(jen.Id("v1"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringEncodeUtf32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")).Call(jen.Id("val"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringEq() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("v1")),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("v2")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("==").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringGe() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op(">=").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringGt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op(">").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringIter() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.List(jen.Id("val"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Id("types").Dot("String"))),
			jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("iter")).Op(":=").List(jen.Id("types").Dot("NewStringIterator").Call(jen.Id("types").Dot("Ref").Call(jen.Id("addr")), jen.Id("val"))),
			jen.Id("iter").Dot("Next").Call(),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("iter")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringLe() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("<=").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringLen() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringLt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("<").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringNe() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("v1")),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("v2")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("!=").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringNewUtf32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("int").Call(jen.Id("i").Dot("intern").Call(jen.Id("string").Call(jen.Id("types").Dot("String").Call(jen.Id("val"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func structNew() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("StructType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.List(jen.Id("size")).Op(":=").List(jen.Id("len").Call(jen.Id("typ").Dot("Fields"))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size"))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("s")).Op(":=").List(jen.Id("types").Dot("NewStruct").Call(jen.Id("typ"))),
			jen.For(jen.List(jen.Id("j"), jen.Id("f")).Op(":=").Range().Add(jen.Id("typ").Dot("Fields"))).Block(jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("+").Add(jen.Id("j")))),
				jen.Switch(jen.Id("f").Dot("Kind")).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindI8"), jen.Id("types").Dot("KindI1"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64"), jen.Id("types").Dot("KindRef")).Block(jen.Id("s").Dot("SetField").Call(jen.Id("j"), jen.Id("val"))),
					jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.Id("s").Dot("SetRaw").Call(jen.Id("j"), jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("val"))))),
					jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size").Op("-").Add(jen.Lit(1))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("s")))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func structNewDefault() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("StructType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Id("len").Call(jen.Id("i").Dot("stack")))).Block(jen.Id("panic").Call(jen.Id("ErrStackOverflow"))),
			jen.List(jen.Id("s")).Op(":=").List(jen.Id("types").Dot("NewStruct").Call(jen.Id("typ"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp"))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("s")))),
			jen.Id("i").Dot("sp").Op("++"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func structSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(3))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("s")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Struct"))).Block(jen.List(jen.Id("typ")).Op(":=").List(jen.Id("s").Dot("Typ")),
				jen.If(jen.Id("idx").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("typ").Dot("Fields"))))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("field")).Op(":=").List(jen.Id("typ").Dot("Fields").Index(jen.Id("idx"))),
				jen.Switch(jen.Id("field").Dot("Kind")).Block(jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.List(jen.Id("s").Dot("Data").Index(jen.Id("idx"))).Op("=").List(jen.Id("uint64").Call(jen.Id("uint32").Call(jen.Id("val").Dot("I32").Call())))),
					jen.Case(jen.Id("types").Dot("KindI8")).Block(jen.List(jen.Id("s").Dot("Data").Index(jen.Id("idx"))).Op("=").List(jen.Id("uint64").Call(jen.Id("uint32").Call(jen.Id("int32").Call(jen.Id("val").Dot("I8").Call()))))),
					jen.Case(jen.Id("types").Dot("KindI1")).Block(jen.If(jen.Id("val").Dot("Bool").Call()).Block(jen.List(jen.Id("s").Dot("Data").Index(jen.Id("idx"))).Op("=").List(jen.Lit(1))).Else().Block(jen.List(jen.Id("s").Dot("Data").Index(jen.Id("idx"))).Op("=").List(jen.Lit(0)))),
					jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.List(jen.Id("s").Dot("Data").Index(jen.Id("idx"))).Op("=").List(jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("val"))))),
					jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.List(jen.Id("s").Dot("Data").Index(jen.Id("idx"))).Op("=").List(jen.Id("uint64").Call(jen.Qual("math", "Float32bits").Call(jen.Id("val").Dot("F32").Call())))),
					jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.List(jen.Id("s").Dot("Data").Index(jen.Id("idx"))).Op("=").List(jen.Qual("math", "Float64bits").Call(jen.Id("val").Dot("F64").Call()))),
					jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("old")).Op(":=").List(jen.Id("types").Dot("Boxed").Call(jen.Id("s").Dot("Data").Index(jen.Id("idx")))),
						jen.Id("i").Dot("releaseBox").Call(jen.Id("old")),
						jen.List(jen.Id("s").Dot("Data").Index(jen.Id("idx"))).Op("=").List(jen.Id("uint64").Call(jen.Id("val")))),
					jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
				jen.Case(jen.Op("*").Add(jen.Id("HostObject"))).Block(jen.List(jen.Id("kind"), jen.Id("ok")).Op(":=").List(jen.Id("s").Dot("writeKind").Call(jen.Id("idx"))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
					jen.Switch(jen.Id("kind")).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindI8"), jen.Id("types").Dot("KindI1"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Id("s").Dot("SetField").Call(jen.Id("idx"), jen.Id("val"))),
						jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.Id("s").Dot("SetRaw").Call(jen.Id("idx"), jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("val"))))),
						jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.Id("s").Dot("SetRaw").Call(jen.Id("idx"), jen.Id("uint64").Call(jen.Id("val")))),
						jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(3)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func swap() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func throw() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("exc")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp"))),
			jen.If(jen.List(jen.Id("fp"), jen.Id("h"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("handler").Call()), jen.Id("ok")).Block(jen.Id("i").Dot("land").Call(jen.Id("fp"), jen.Id("h"), jen.Id("exc")),
				jen.Return()),
			jen.Id("panic").Call(jen.Id("escape").Values(jen.Id("i").Dot("uncaught").Call(jen.Id("exc")))))))
}

func unreachable() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("i").Dot("fr").Dot("ip").Op("++"),
			jen.Id("panic").Call(jen.Id("ErrUnreachableExecuted")))))
}

func upvalSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(2)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("captures")))).Block(jen.Switch(jen.Id("c").Dot("captures").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("fr").Dot("upvals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("fr").Dot("upvals").Index(jen.Id("idx"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("fr").Dot("upvals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("fr").Dot("upvals").Index(jen.Id("idx"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("fr").Dot("upvals").Index(jen.Id("idx"))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2)))))
}

func yield() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"),
			jen.If(jen.Id("i").Dot("fp").Op("==").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("errYield"))),
			jen.Block(jen.List(jen.Id("f")).Op(":=").List(jen.Id("i").Dot("fr")),
				jen.List(jen.Id("coAddr")).Op(":=").List(jen.Id("f").Dot("coro")),
				jen.List(jen.Id("co"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("coAddr")).Assert(jen.Op("*").Add(jen.Id("coroutine")))),
				jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
				jen.Id("i").Dot("sp").Op("--"),
				jen.List(jen.Id("co").Dot("value")).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp"))),
				jen.List(jen.Id("co").Dot("addr")).Op("=").List(jen.Id("f").Dot("addr")),
				jen.List(jen.Id("co").Dot("ref")).Op("=").List(jen.Id("f").Dot("ref")),
				jen.List(jen.Id("co").Dot("returns")).Op("=").List(jen.Id("f").Dot("returns")),
				jen.List(jen.Id("co").Dot("release")).Op("=").List(jen.Id("f").Dot("release")),
				jen.List(jen.Id("co").Dot("ip")).Op("=").List(jen.Id("f").Dot("ip")),
				jen.List(jen.Id("co").Dot("upvals")).Op("=").List(jen.Id("f").Dot("upvals")),
				jen.List(jen.Id("co").Dot("image")).Op("=").List(jen.Id("append").Call(jen.Id("co").Dot("image").Index(jen.Empty().Op(":").Add(jen.Lit(0))), jen.Id("i").Dot("stack").Index(jen.Id("f").Dot("bp").Op(":").Add(jen.Id("i").Dot("sp"))).Op("..."))),
				jen.List(jen.Id("bp")).Op(":=").List(jen.Id("f").Dot("bp")),
				jen.Id("clear").Call(jen.Id("i").Dot("stack").Index(jen.Id("bp").Op(":").Add(jen.Id("i").Dot("sp")))),
				jen.List(jen.Id("f").Dot("code")).Op("=").List(jen.Id("nil")),
				jen.List(jen.Id("f").Dot("upvals")).Op("=").List(jen.Id("nil")),
				jen.List(jen.Id("f").Dot("coro")).Op("=").List(jen.Lit(0)),
				jen.Id("i").Dot("fp").Op("--"),
				jen.List(jen.Id("i").Dot("fr")).Op("=").List(jen.Op("&").Add(jen.Id("i").Dot("frames").Index(jen.Id("i").Dot("fp").Op("-").Add(jen.Lit(1))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("bp"))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("coAddr"))),
				jen.List(jen.Id("i").Dot("sp")).Op("=").List(jen.Id("bp").Op("+").Add(jen.Lit(1)))))))
}
