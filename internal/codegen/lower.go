package codegen

import (
	"fmt"
	"reflect"

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

type lowerer func(*state, step) (value, error)

var lowerers = [256]lowerer{
	instr.ARRAY_APPEND:        standaloneLowerer(arrayAppend),
	instr.ARRAY_COPY:          standaloneLowerer(arrayCopy),
	instr.ARRAY_DELETE:        standaloneLowerer(arrayDelete),
	instr.ARRAY_FILL:          standaloneLowerer(arrayFill),
	instr.ARRAY_GET:           containerGet,
	instr.ARRAY_LEN:           standaloneLowerer(arrayLen),
	instr.ARRAY_NEW:           standaloneLowerer(arrayNew),
	instr.ARRAY_NEW_DEFAULT:   standaloneLowerer(arrayNewDefault),
	instr.ARRAY_SET:           arrayStore,
	instr.ARRAY_SLICE:         standaloneLowerer(arraySlice),
	instr.BR:                  standaloneLowerer(br),
	instr.BR_IF:               branch,
	instr.BR_TABLE:            standaloneLowerer(brTable),
	instr.CALL:                call,
	instr.CLOSURE_NEW:         call,
	instr.CONST_GET:           slotRead,
	instr.CORO_DONE:           standaloneLowerer(coroDone),
	instr.CORO_VALUE:          standaloneLowerer(coroValue),
	instr.DROP:                refOp,
	instr.DUP:                 refOp,
	instr.ERROR_CODE:          standaloneLowerer(errorCode),
	instr.ERROR_GET:           standaloneLowerer(errorGet),
	instr.ERROR_NEW:           standaloneLowerer(errorNew),
	instr.F32_ABS:             standaloneLowerer(f32Abs),
	instr.F32_ADD:             arithmetic,
	instr.F32_CEIL:            standaloneLowerer(f32Ceil),
	instr.F32_CONST:           slotRead,
	instr.F32_COPYSIGN:        arithmetic,
	instr.F32_DIV:             arithmetic,
	instr.F32_EQ:              arithmetic,
	instr.F32_FLOOR:           standaloneLowerer(f32Floor),
	instr.F32_GE:              arithmetic,
	instr.F32_GT:              arithmetic,
	instr.F32_LE:              arithmetic,
	instr.F32_LT:              arithmetic,
	instr.F32_MAX:             arithmetic,
	instr.F32_MIN:             arithmetic,
	instr.F32_MOD:             arithmetic,
	instr.F32_MUL:             arithmetic,
	instr.F32_NE:              arithmetic,
	instr.F32_NEAREST:         standaloneLowerer(f32Nearest),
	instr.F32_NEG:             standaloneLowerer(f32Neg),
	instr.F32_REINTERPRET_I32: standaloneLowerer(f32ReinterpretI32),
	instr.F32_REM:             arithmetic,
	instr.F32_SQRT:            standaloneLowerer(f32Sqrt),
	instr.F32_SUB:             arithmetic,
	instr.F32_TO_F64:          standaloneLowerer(f32ToF64),
	instr.F32_TO_I32_S:        standaloneLowerer(f32ToI32S),
	instr.F32_TO_I32_U:        standaloneLowerer(f32ToI32U),
	instr.F32_TO_I64_S:        standaloneLowerer(f32ToI64S),
	instr.F32_TO_I64_U:        standaloneLowerer(f32ToI64U),
	instr.F32_TRUNC:           standaloneLowerer(f32Trunc),
	instr.F64_ABS:             standaloneLowerer(f64Abs),
	instr.F64_ADD:             arithmetic,
	instr.F64_CEIL:            standaloneLowerer(f64Ceil),
	instr.F64_CONST:           slotRead,
	instr.F64_COPYSIGN:        arithmetic,
	instr.F64_DIV:             arithmetic,
	instr.F64_EQ:              arithmetic,
	instr.F64_FLOOR:           standaloneLowerer(f64Floor),
	instr.F64_GE:              arithmetic,
	instr.F64_GT:              arithmetic,
	instr.F64_LE:              arithmetic,
	instr.F64_LT:              arithmetic,
	instr.F64_MAX:             arithmetic,
	instr.F64_MIN:             arithmetic,
	instr.F64_MOD:             arithmetic,
	instr.F64_MUL:             arithmetic,
	instr.F64_NE:              arithmetic,
	instr.F64_NEAREST:         standaloneLowerer(f64Nearest),
	instr.F64_NEG:             standaloneLowerer(f64Neg),
	instr.F64_REINTERPRET_I64: standaloneLowerer(f64ReinterpretI64),
	instr.F64_REM:             arithmetic,
	instr.F64_SQRT:            standaloneLowerer(f64Sqrt),
	instr.F64_SUB:             arithmetic,
	instr.F64_TO_F32:          standaloneLowerer(f64ToF32),
	instr.F64_TO_I32_S:        standaloneLowerer(f64ToI32S),
	instr.F64_TO_I32_U:        standaloneLowerer(f64ToI32U),
	instr.F64_TO_I64_S:        standaloneLowerer(f64ToI64S),
	instr.F64_TO_I64_U:        standaloneLowerer(f64ToI64U),
	instr.F64_TRUNC:           standaloneLowerer(f64Trunc),
	instr.GLOBAL_GET:          slotRead,
	instr.GLOBAL_SET:          standaloneLowerer(globalSet),
	instr.GLOBAL_TEE:          standaloneLowerer(globalTee),
	instr.I32_ADD:             arithmetic,
	instr.I32_AND:             arithmetic,
	instr.I32_CLZ:             standaloneLowerer(i32Clz),
	instr.I32_CONST:           slotRead,
	instr.I32_CTZ:             standaloneLowerer(i32Ctz),
	instr.I32_DIV_S:           arithmetic,
	instr.I32_DIV_U:           arithmetic,
	instr.I32_EQ:              arithmetic,
	instr.I32_EQZ:             arithmetic,
	instr.I32_EXTEND16_S:      standaloneLowerer(i32Extend16S),
	instr.I32_EXTEND8_S:       standaloneLowerer(i32Extend8S),
	instr.I32_GE_S:            arithmetic,
	instr.I32_GE_U:            arithmetic,
	instr.I32_GT_S:            arithmetic,
	instr.I32_GT_U:            arithmetic,
	instr.I32_LE_S:            arithmetic,
	instr.I32_LE_U:            arithmetic,
	instr.I32_LT_S:            arithmetic,
	instr.I32_LT_U:            arithmetic,
	instr.I32_MUL:             arithmetic,
	instr.I32_NE:              arithmetic,
	instr.I32_OR:              arithmetic,
	instr.I32_POPCNT:          standaloneLowerer(i32Popcnt),
	instr.I32_REINTERPRET_F32: standaloneLowerer(i32ReinterpretF32),
	instr.I32_REM_S:           arithmetic,
	instr.I32_REM_U:           arithmetic,
	instr.I32_ROTL:            arithmetic,
	instr.I32_ROTR:            arithmetic,
	instr.I32_SHL:             arithmetic,
	instr.I32_SHR_S:           arithmetic,
	instr.I32_SHR_U:           arithmetic,
	instr.I32_SUB:             arithmetic,
	instr.I32_TO_F32_S:        standaloneLowerer(i32ToF32S),
	instr.I32_TO_F32_U:        standaloneLowerer(i32ToF32U),
	instr.I32_TO_F64_S:        standaloneLowerer(i32ToF64S),
	instr.I32_TO_F64_U:        standaloneLowerer(i32ToF64U),
	instr.I32_TO_I64_S:        standaloneLowerer(i32ToI64S),
	instr.I32_TO_I64_U:        standaloneLowerer(i32ToI64U),
	instr.I32_XOR:             arithmetic,
	instr.I64_ADD:             arithmetic,
	instr.I64_AND:             arithmetic,
	instr.I64_CLZ:             standaloneLowerer(i64Clz),
	instr.I64_CONST:           slotRead,
	instr.I64_CTZ:             standaloneLowerer(i64Ctz),
	instr.I64_DIV_S:           arithmetic,
	instr.I64_DIV_U:           arithmetic,
	instr.I64_EQ:              arithmetic,
	instr.I64_EQZ:             arithmetic,
	instr.I64_EXTEND16_S:      standaloneLowerer(i64Extend16S),
	instr.I64_EXTEND32_S:      standaloneLowerer(i64Extend32S),
	instr.I64_EXTEND8_S:       standaloneLowerer(i64Extend8S),
	instr.I64_GE_S:            arithmetic,
	instr.I64_GE_U:            arithmetic,
	instr.I64_GT_S:            arithmetic,
	instr.I64_GT_U:            arithmetic,
	instr.I64_LE_S:            arithmetic,
	instr.I64_LE_U:            arithmetic,
	instr.I64_LT_S:            arithmetic,
	instr.I64_LT_U:            arithmetic,
	instr.I64_MUL:             arithmetic,
	instr.I64_NE:              arithmetic,
	instr.I64_OR:              arithmetic,
	instr.I64_POPCNT:          standaloneLowerer(i64Popcnt),
	instr.I64_REINTERPRET_F64: standaloneLowerer(i64ReinterpretF64),
	instr.I64_REM_S:           arithmetic,
	instr.I64_REM_U:           arithmetic,
	instr.I64_ROTL:            arithmetic,
	instr.I64_ROTR:            arithmetic,
	instr.I64_SHL:             arithmetic,
	instr.I64_SHR_S:           arithmetic,
	instr.I64_SHR_U:           arithmetic,
	instr.I64_SUB:             arithmetic,
	instr.I64_TO_F32_S:        standaloneLowerer(i64ToF32S),
	instr.I64_TO_F32_U:        standaloneLowerer(i64ToF32U),
	instr.I64_TO_F64_S:        standaloneLowerer(i64ToF64S),
	instr.I64_TO_F64_U:        standaloneLowerer(i64ToF64U),
	instr.I64_TO_I32:          standaloneLowerer(i64ToI32),
	instr.I64_XOR:             arithmetic,
	instr.LOCAL_GET:           slotRead,
	instr.LOCAL_SET:           localStore,
	instr.LOCAL_TEE:           standaloneLowerer(localTee),
	instr.MAP_CLEAR:           standaloneLowerer(mapClear),
	instr.MAP_DELETE:          standaloneLowerer(mapDelete),
	instr.MAP_GET:             standaloneLowerer(mapGet),
	instr.MAP_ITER:            standaloneLowerer(mapIter),
	instr.MAP_KEYS:            standaloneLowerer(mapKeys),
	instr.MAP_LEN:             standaloneLowerer(mapLen),
	instr.MAP_LOOKUP:          standaloneLowerer(mapLookup),
	instr.MAP_NEW:             standaloneLowerer(mapNew),
	instr.MAP_NEW_DEFAULT:     standaloneLowerer(mapNewDefault),
	instr.MAP_SET:             standaloneLowerer(mapSet),
	instr.NOP:                 standaloneLowerer(nop),
	instr.REF_CAST:            standaloneLowerer(refCast),
	instr.REF_EQ:              standaloneLowerer(refEq),
	instr.REF_GET:             standaloneLowerer(refGet),
	instr.REF_IS_NULL:         refOp,
	instr.REF_NE:              standaloneLowerer(refNe),
	instr.REF_NEW:             standaloneLowerer(refNew),
	instr.REF_NULL:            refOp,
	instr.REF_SET:             standaloneLowerer(refSet),
	instr.REF_TEST:            standaloneLowerer(refTest),
	instr.RESUME:              standaloneLowerer(resume),
	instr.RETURN:              standaloneLowerer(returnOp),
	instr.RETURN_CALL:         call,
	instr.SELECT:              standaloneLowerer(selectOp),
	instr.STRING_CONCAT:       standaloneLowerer(stringConcat),
	instr.STRING_ENCODE_UTF32: standaloneLowerer(stringEncodeUtf32),
	instr.STRING_EQ:           standaloneLowerer(stringEq),
	instr.STRING_GE:           standaloneLowerer(stringGe),
	instr.STRING_GT:           standaloneLowerer(stringGt),
	instr.STRING_ITER:         standaloneLowerer(stringIter),
	instr.STRING_LE:           standaloneLowerer(stringLe),
	instr.STRING_LEN:          standaloneLowerer(stringLen),
	instr.STRING_LT:           standaloneLowerer(stringLt),
	instr.STRING_NE:           standaloneLowerer(stringNe),
	instr.STRING_NEW_UTF32:    standaloneLowerer(stringNewUtf32),
	instr.STRUCT_GET:          containerGet,
	instr.STRUCT_NEW:          standaloneLowerer(structNew),
	instr.STRUCT_NEW_DEFAULT:  standaloneLowerer(structNewDefault),
	instr.STRUCT_SET:          standaloneLowerer(structSet),
	instr.SWAP:                standaloneLowerer(swap),
	instr.THROW:               standaloneLowerer(throw),
	instr.UNREACHABLE:         standaloneLowerer(unreachable),
	instr.UPVAL_GET:           slotRead,
	instr.UPVAL_SET:           standaloneLowerer(upvalSet),
	instr.YIELD:               standaloneLowerer(yield),
}

func standaloneLowerer(emit func() jen.Code) lowerer {
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

func standalone(op instr.Opcode, compile, body []jen.Code) jen.Code {
	code := append([]jen.Code(nil), compile...)
	code = append(code,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(op)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return threaderFunc(code...)
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
	if consumer == instr.ARRAY_SET && consumerAt == 3 && (steps[0].op == instr.CONST_GET || (isContainerSource(steps[0].op) && steps[0].typ != nil)) {
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

	kind, count, ok := operands(consumer)
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

func operands(op instr.Opcode) (instr.Kind, int, bool) {
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

func overflow() jen.Code {
	return jen.If(jen.Id("i").Dot("sp").Op("==").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow")))
}

func reject(label string) jen.Code {
	if label != "" {
		return jen.Goto().Id(label)
	}
	return jen.Return(jen.Nil())
}

func temp(index int) string {
	return fmt.Sprintf("v%d", index)
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

func numericKind(op instr.Opcode) (instr.Kind, bool) {
	pop := instr.TypeOf(op).Pop
	if len(pop) == 0 {
		return instr.KindAny, false
	}
	kind := pop[0].Repr()
	return kind, kind.IsNumeric()
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
