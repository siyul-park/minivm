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

// typesPkg and instrPkg are the import paths generated code qualifies with.
const (
	typesPkg = "github.com/siyul-park/minivm/types"
	instrPkg = "github.com/siyul-park/minivm/instr"
)

var lowerers = [256]lowerer{
	instr.ARRAY_APPEND:        emit(arrayAppend()),
	instr.ARRAY_COPY:          emit(arrayCopy()),
	instr.ARRAY_DELETE:        emit(arrayDelete()),
	instr.ARRAY_FILL:          emit(arrayFill()),
	instr.ARRAY_GET:           containerGet,
	instr.ARRAY_LEN:           emit(arrayLen()),
	instr.ARRAY_NEW:           emit(arrayNew()),
	instr.ARRAY_NEW_DEFAULT:   emit(arrayNewDefault()),
	instr.ARRAY_SET:           arrayStore,
	instr.ARRAY_SLICE:         emit(arraySlice()),
	instr.BR:                  emit(br()),
	instr.BR_IF:               branch,
	instr.BR_TABLE:            emit(brTable()),
	instr.CALL:                call,
	instr.CLOSURE_NEW:         call,
	instr.CONST_GET:           slotRead,
	instr.CORO_DONE:           emit(coroDone()),
	instr.CORO_VALUE:          emit(coroValue()),
	instr.DROP:                refOp,
	instr.DUP:                 refOp,
	instr.ERROR_CODE:          emit(errorCode()),
	instr.ERROR_GET:           emit(errorGet()),
	instr.ERROR_NEW:           emit(errorNew()),
	instr.F32_ABS:             emit(float32Math("Abs")),
	instr.F32_ADD:             arithmetic,
	instr.F32_CEIL:            emit(float32Math("Ceil")),
	instr.F32_CONST:           slotRead,
	instr.F32_COPYSIGN:        arithmetic,
	instr.F32_DIV:             arithmetic,
	instr.F32_EQ:              arithmetic,
	instr.F32_FLOOR:           emit(float32Math("Floor")),
	instr.F32_GE:              arithmetic,
	instr.F32_GT:              arithmetic,
	instr.F32_LE:              arithmetic,
	instr.F32_LT:              arithmetic,
	instr.F32_MAX:             arithmetic,
	instr.F32_MIN:             arithmetic,
	instr.F32_MOD:             arithmetic,
	instr.F32_MUL:             arithmetic,
	instr.F32_NE:              arithmetic,
	instr.F32_NEAREST:         emit(float32Math("RoundToEven")),
	instr.F32_NEG:             emit(convert(narrow("F32"), "F32", neg)),
	instr.F32_REINTERPRET_I32: emit(convert(narrow("I32"), "F32", as("uint32"), via("math", "Float32frombits"))),
	instr.F32_REM:             arithmetic,
	instr.F32_SQRT:            emit(float32Math("Sqrt")),
	instr.F32_SUB:             arithmetic,
	instr.F32_TO_F64:          emit(convert(narrow("F32"), "F64", as("float64"))),
	instr.F32_TO_I32_S:        emit(saturate("F32", "int32")),
	instr.F32_TO_I32_U:        emit(saturate("F32", "uint32")),
	instr.F32_TO_I64_S:        emit(saturate("F32", "int64")),
	instr.F32_TO_I64_U:        emit(saturate("F32", "uint64")),
	instr.F32_TRUNC:           emit(float32Math("Trunc")),
	instr.F64_ABS:             emit(float64Math("Abs")),
	instr.F64_ADD:             arithmetic,
	instr.F64_CEIL:            emit(float64Math("Ceil")),
	instr.F64_CONST:           slotRead,
	instr.F64_COPYSIGN:        arithmetic,
	instr.F64_DIV:             arithmetic,
	instr.F64_EQ:              arithmetic,
	instr.F64_FLOOR:           emit(float64Math("Floor")),
	instr.F64_GE:              arithmetic,
	instr.F64_GT:              arithmetic,
	instr.F64_LE:              arithmetic,
	instr.F64_LT:              arithmetic,
	instr.F64_MAX:             arithmetic,
	instr.F64_MIN:             arithmetic,
	instr.F64_MOD:             arithmetic,
	instr.F64_MUL:             arithmetic,
	instr.F64_NE:              arithmetic,
	instr.F64_NEAREST:         emit(float64Math("RoundToEven")),
	instr.F64_NEG:             emit(convert(narrow("F64"), "F64", neg)),
	instr.F64_REINTERPRET_I64: emit(convert(wide(), "F64", as("uint64"), via("math", "Float64frombits"))),
	instr.F64_REM:             arithmetic,
	instr.F64_SQRT:            emit(float64Math("Sqrt")),
	instr.F64_SUB:             arithmetic,
	instr.F64_TO_F32:          emit(convert(narrow("F64"), "F32", as("float32"))),
	instr.F64_TO_I32_S:        emit(saturate("F64", "int32")),
	instr.F64_TO_I32_U:        emit(saturate("F64", "uint32")),
	instr.F64_TO_I64_S:        emit(saturate("F64", "int64")),
	instr.F64_TO_I64_U:        emit(saturate("F64", "uint64")),
	instr.F64_TRUNC:           emit(float64Math("Trunc")),
	instr.GLOBAL_GET:          slotRead,
	instr.GLOBAL_SET:          emit(store(globalSlot, false)),
	instr.GLOBAL_TEE:          emit(store(globalSlot, true)),
	instr.I32_ADD:             arithmetic,
	instr.I32_AND:             arithmetic,
	instr.I32_CLZ:             emit(convert(narrow("I32"), "I32", as("uint32"), via("math/bits", "LeadingZeros32"), as("int32"))),
	instr.I32_CONST:           slotRead,
	instr.I32_CTZ:             emit(convert(narrow("I32"), "I32", as("uint32"), via("math/bits", "TrailingZeros32"), as("int32"))),
	instr.I32_DIV_S:           arithmetic,
	instr.I32_DIV_U:           arithmetic,
	instr.I32_EQ:              arithmetic,
	instr.I32_EQZ:             arithmetic,
	instr.I32_EXTEND16_S:      emit(convert(narrow("I32"), "I32", as("int16"), as("int32"))),
	instr.I32_EXTEND8_S:       emit(convert(narrow("I32"), "I32", as("int8"), as("int32"))),
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
	instr.I32_POPCNT:          emit(convert(narrow("I32"), "I32", as("uint32"), via("math/bits", "OnesCount32"), as("int32"))),
	instr.I32_REINTERPRET_F32: emit(convert(narrow("F32"), "I32", via("math", "Float32bits"), as("int32"))),
	instr.I32_REM_S:           arithmetic,
	instr.I32_REM_U:           arithmetic,
	instr.I32_ROTL:            arithmetic,
	instr.I32_ROTR:            arithmetic,
	instr.I32_SHL:             arithmetic,
	instr.I32_SHR_S:           arithmetic,
	instr.I32_SHR_U:           arithmetic,
	instr.I32_SUB:             arithmetic,
	instr.I32_TO_F32_S:        emit(convert(narrow("I32"), "F32", as("float32"))),
	instr.I32_TO_F32_U:        emit(convert(narrow("I32"), "F32", as("uint32"), as("float32"))),
	instr.I32_TO_F64_S:        emit(convert(narrow("I32"), "F64", as("float64"))),
	instr.I32_TO_F64_U:        emit(convert(narrow("I32"), "F64", as("uint32"), as("float64"))),
	instr.I32_TO_I64_S:        emit(convert(narrow("I32"), "I64", as("int64"))),
	instr.I32_TO_I64_U:        emit(convert(narrow("I32"), "I64", as("uint32"), as("int64"))),
	instr.I32_XOR:             arithmetic,
	instr.I64_ADD:             arithmetic,
	instr.I64_AND:             arithmetic,
	instr.I64_CLZ:             emit(convert(wide(), "I64", as("uint64"), via("math/bits", "LeadingZeros64"), as("int64"))),
	instr.I64_CONST:           slotRead,
	instr.I64_CTZ:             emit(convert(wide(), "I64", as("uint64"), via("math/bits", "TrailingZeros64"), as("int64"))),
	instr.I64_DIV_S:           arithmetic,
	instr.I64_DIV_U:           arithmetic,
	instr.I64_EQ:              arithmetic,
	instr.I64_EQZ:             arithmetic,
	instr.I64_EXTEND16_S:      emit(convert(wide(), "I64", as("int16"), as("int64"))),
	instr.I64_EXTEND32_S:      emit(convert(wide(), "I64", as("int32"), as("int64"))),
	instr.I64_EXTEND8_S:       emit(convert(wide(), "I64", as("int8"), as("int64"))),
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
	instr.I64_POPCNT:          emit(convert(wide(), "I64", as("uint64"), via("math/bits", "OnesCount64"), as("int64"))),
	instr.I64_REINTERPRET_F64: emit(convert(narrow("F64"), "I64", via("math", "Float64bits"), as("int64"))),
	instr.I64_REM_S:           arithmetic,
	instr.I64_REM_U:           arithmetic,
	instr.I64_ROTL:            arithmetic,
	instr.I64_ROTR:            arithmetic,
	instr.I64_SHL:             arithmetic,
	instr.I64_SHR_S:           arithmetic,
	instr.I64_SHR_U:           arithmetic,
	instr.I64_SUB:             arithmetic,
	instr.I64_TO_F32_S:        emit(convert(wide(), "F32", as("float32"))),
	instr.I64_TO_F32_U:        emit(convert(wide(), "F32", as("uint64"), as("float32"))),
	instr.I64_TO_F64_S:        emit(convert(wide(), "F64", as("float64"))),
	instr.I64_TO_F64_U:        emit(convert(wide(), "F64", as("uint64"), as("float64"))),
	instr.I64_TO_I32:          emit(convert(wide(), "I32", as("int32"))),
	instr.I64_XOR:             arithmetic,
	instr.LOCAL_GET:           slotRead,
	instr.LOCAL_SET:           localStore,
	instr.LOCAL_TEE:           emit(store(localSlot, true)),
	instr.MAP_CLEAR:           emit(mapClear()),
	instr.MAP_DELETE:          emit(mapDelete()),
	instr.MAP_GET:             emit(mapGet()),
	instr.MAP_ITER:            emit(mapIter()),
	instr.MAP_KEYS:            emit(mapKeys()),
	instr.MAP_LEN:             emit(mapLen()),
	instr.MAP_LOOKUP:          emit(mapLookup()),
	instr.MAP_NEW:             emit(mapNew()),
	instr.MAP_NEW_DEFAULT:     emit(mapNewDefault()),
	instr.MAP_SET:             emit(mapSet()),
	instr.NOP:                 emit(nop()),
	instr.REF_CAST:            emit(refCast()),
	instr.REF_EQ:              emit(refCompare("==")),
	instr.REF_GET:             emit(refGet()),
	instr.REF_IS_NULL:         refOp,
	instr.REF_NE:              emit(refCompare("!=")),
	instr.REF_NEW:             emit(refNew()),
	instr.REF_NULL:            refOp,
	instr.REF_SET:             emit(refSet()),
	instr.REF_TEST:            emit(refTest()),
	instr.RESUME:              emit(resume()),
	instr.RETURN:              emit(returnOp()),
	instr.RETURN_CALL:         call,
	instr.SELECT:              emit(selectOp()),
	instr.STRING_CONCAT:       emit(stringConcat()),
	instr.STRING_ENCODE_UTF32: emit(stringEncodeUtf32()),
	instr.STRING_EQ:           emit(stringCompare("==")),
	instr.STRING_GE:           emit(stringCompare(">=")),
	instr.STRING_GT:           emit(stringCompare(">")),
	instr.STRING_ITER:         emit(stringIter()),
	instr.STRING_LE:           emit(stringCompare("<=")),
	instr.STRING_LEN:          emit(stringLen()),
	instr.STRING_LT:           emit(stringCompare("<")),
	instr.STRING_NE:           emit(stringCompare("!=")),
	instr.STRING_NEW_UTF32:    emit(stringNewUtf32()),
	instr.STRUCT_GET:          containerGet,
	instr.STRUCT_NEW:          emit(structNew()),
	instr.STRUCT_NEW_DEFAULT:  emit(structNewDefault()),
	instr.STRUCT_SET:          emit(structSet()),
	instr.SWAP:                emit(swap()),
	instr.THROW:               emit(throw()),
	instr.UNREACHABLE:         emit(unreachable()),
	instr.UPVAL_GET:           slotRead,
	instr.UPVAL_SET:           emit(store(upvalSlot, false)),
	instr.YIELD:               emit(yield()),
}

// emit lowers an opcode that has one handler and no fusion form.
func emit(code jen.Code) lowerer {
	return func(_ *state, current step) (value, error) {
		return value{op: current.op, head: current.op, handler: code}, nil
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
		jen.Return(closure(body...)),
	)
	return threaderFunc(code...)
}

// handler wraps body as the one-byte opcode shape: the compile step skips the
// opcode and body runs per execution.
func handler(body ...jen.Code) jen.Code {
	return threaderFunc(
		jen.Id("c").Dot("ip").Op("++"),
		jen.Return(closure(body...)),
	)
}

// u8 and u16 bind name to the one- and two-byte operand that follows the
// opcode at code position pos.
func u8(name string, pos jen.Code) jen.Code {
	return jen.Id(name).Op(":=").Id("int").Call(jen.Id("c").Dot("code").Index(jen.Add(pos).Op("+").Lit(1)))
}

func u16(name string, pos jen.Code) jen.Code {
	return jen.Id(name).Op(":=").Id("int").Call(jen.Op("*").Parens(jen.Op("*").Id("uint16")).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Id("c").Dot("code").Index(jen.Add(pos).Op("+").Lit(1)))))
}

// typeAt wraps body as the handler of a three-byte opcode whose operand
// indexes c.types. An index out of range traps at run time.
func typeAt(body ...jen.Code) jen.Code {
	return threaderFunc(append([]jen.Code{
		u16("idx", jen.Id("c").Dot("ip")),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.If(jen.Id("idx").Op(">=").Id("len").Call(jen.Id("c").Dot("types"))).Block(jen.Return(closure(jen.Panic(jen.Id("ErrSegmentationFault"))))),
	}, body...)...)
}

// typed is typeAt for an operand that must name a *types.<typ>, bound as typ
// for body; any other type traps at run time.
func typed(typ string, body ...jen.Code) jen.Code {
	return typeAt(append([]jen.Code{
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Qual(typesPkg, typ)),
		jen.If(jen.Op("!").Id("ok")).Block(jen.Return(closure(jen.Panic(jen.Id("ErrTypeMismatch"))))),
	}, body...)...)
}

// next moves the running frame past the current instruction.
func next() jen.Code {
	return jen.Id("i").Dot("fr").Dot("ip").Op("++")
}

// top is the operand-stack slot k from the top; top(1) is the top.
func top(k int) *jen.Statement {
	return jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(k))
}

// underflow traps when fewer than n operands are on the stack.
func underflow(n int) jen.Code {
	cond := jen.Id("i").Dot("sp").Op("<").Lit(n)
	if n == 1 {
		cond = jen.Id("i").Dot("sp").Op("==").Lit(0)
	}
	return jen.If(cond).Block(jen.Panic(jen.Id("ErrStackUnderflow")))
}

// cool spends one unit of a dormant JIT's heat (Interpreter.heat) when cond
// holds; a nil cond always holds. Frame entries and taken back edges end
// with it. The unit that runs heat out parks the current frame at ip park
// (Interpreter.parked keeps its ip) so dispatch leaves its loop and wakes the
// JIT: the handler writes no pointer and makes no call, either of which
// would cost every handler a stack frame.
func cool(cond jen.Code) jen.Code {
	check := jen.Id("i").Dot("heat").Op(">").Lit(0)
	if cond != nil {
		check = jen.Add(cond).Op("&&").Add(check)
	}
	return jen.If(check).Block(
		jen.Id("i").Dot("heat").Op("--"),
		jen.If(jen.Id("i").Dot("heat").Op("==").Lit(0)).Block(
			jen.List(jen.Id("i").Dot("parked"), jen.Id("i").Dot("fr").Dot("ip")).Op("=").List(jen.Id("i").Dot("fr").Dot("ip"), jen.Id("park")),
		),
	)
}

// closure is the runtime handler a compile step returns.
func closure(body ...jen.Code) jen.Code {
	return jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)
}

// reference traps unless slot holds a heap reference.
func reference(slot jen.Code) jen.Code {
	return jen.If(jen.Add(slot).Dot("Kind").Call().Op("!=").Qual(typesPkg, "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch")))
}

// container binds the reference at stack slot k to ref and its heap address to
// addr, trapping when the slot holds no reference.
func container(k int) jen.Code {
	return jen.Id("ref").Op(":=").Add(top(k)).Line().
		Add(reference(jen.Id("ref"))).Line().
		Id("addr").Op(":=").Id("ref").Dot("Ref").Call()
}

// failure traps with the error call returns.
func failure(call jen.Code) jen.Code {
	return jen.If(jen.Id("err").Op(":=").Add(call), jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err")))
}

// view replaces the host view in source with the VM value method of the view
// returns, a copy of the Go value it addresses.
func view(host, method string) jen.Code {
	return jen.If(jen.List(jen.Id("view"), jen.Id("ok")).Op(":=").Id("source").Assert(jen.Op("*").Id(host)), jen.Id("ok")).Block(
		jen.List(jen.Id("value"), jen.Id("err")).Op(":=").Id("view").Dot(method).Call(jen.Id("i")),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
		jen.Id("source").Op("=").Id("value"),
	)
}

// typeSwitch dispatches on the dynamic type behind subject: one case per
// element of table, then rest, then a trap for any other type.
func typeSwitch[T any](subject jen.Code, table []T, instance func(T) jen.Code, body func(T) []jen.Code, rest ...jen.Code) jen.Code {
	cases := make([]jen.Code, 0, len(table)+len(rest)+1)
	for _, entry := range table {
		cases = append(cases, jen.Case(instance(entry)).Block(body(entry)...))
	}
	cases = append(cases, rest...)
	cases = append(cases, jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))))
	return jen.Switch(subject).Block(cases...)
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
