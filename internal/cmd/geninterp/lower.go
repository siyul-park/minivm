package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

type lowering struct {
	source   step
	compile  []jen.Code
	check    []jen.Code
	body     []jen.Code
	discard  []jen.Code
	push     []jen.Code
	raw      jen.Code
	boxed    jen.Code
	resident bool
	handler  jen.Code
}

type fact struct {
	step
	kind   instr.Kind
	boxed  bool
	commit bool
}

type loweringState struct {
	pending    []lowering
	offset     int
	total      int
	label      string
	standalone bool
}

type callTarget struct {
	addr   jen.Code
	upvals jen.Code
	ref    jen.Code
}

type lowerer func(*loweringState, fact) (lowering, error)

var lowerers = [256]lowerer{
	instr.ARRAY_APPEND:        wrap(arrayAppend),
	instr.ARRAY_COPY:          wrap(arrayCopy),
	instr.ARRAY_DELETE:        wrap(arrayDelete),
	instr.ARRAY_FILL:          wrap(arrayFill),
	instr.ARRAY_GET:           indexLower,
	instr.ARRAY_LEN:           wrap(arrayLen),
	instr.ARRAY_NEW:           wrap(arrayNew),
	instr.ARRAY_NEW_DEFAULT:   wrap(arrayNewDefault),
	instr.ARRAY_SET:           wrap(arraySet),
	instr.ARRAY_SLICE:         wrap(arraySlice),
	instr.BR:                  wrap(br),
	instr.BR_IF:               branchLower,
	instr.BR_TABLE:            wrap(brTable),
	instr.CALL:                callLower,
	instr.CLOSURE_NEW:         callLower,
	instr.CONST_GET:           sourceLower,
	instr.CORO_DONE:           wrap(coroDone),
	instr.CORO_VALUE:          wrap(coroValue),
	instr.DROP:                refLower,
	instr.DUP:                 refSource,
	instr.ERROR_CODE:          wrap(errorCode),
	instr.ERROR_GET:           wrap(errorGet),
	instr.ERROR_NEW:           wrap(errorNew),
	instr.F32_ABS:             wrap(f32Abs),
	instr.F32_ADD:             numericLower,
	instr.F32_CEIL:            wrap(f32Ceil),
	instr.F32_CONST:           sourceLower,
	instr.F32_COPYSIGN:        numericLower,
	instr.F32_DIV:             numericLower,
	instr.F32_EQ:              numericLower,
	instr.F32_FLOOR:           wrap(f32Floor),
	instr.F32_GE:              numericLower,
	instr.F32_GT:              numericLower,
	instr.F32_LE:              numericLower,
	instr.F32_LT:              numericLower,
	instr.F32_MAX:             numericLower,
	instr.F32_MIN:             numericLower,
	instr.F32_MOD:             numericLower,
	instr.F32_MUL:             numericLower,
	instr.F32_NE:              numericLower,
	instr.F32_NEAREST:         wrap(f32Nearest),
	instr.F32_NEG:             wrap(f32Neg),
	instr.F32_REINTERPRET_I32: wrap(f32ReinterpretI32),
	instr.F32_REM:             numericLower,
	instr.F32_SQRT:            wrap(f32Sqrt),
	instr.F32_SUB:             numericLower,
	instr.F32_TO_F64:          wrap(f32ToF64),
	instr.F32_TO_I32_S:        wrap(f32ToI32S),
	instr.F32_TO_I32_U:        wrap(f32ToI32U),
	instr.F32_TO_I64_S:        wrap(f32ToI64S),
	instr.F32_TO_I64_U:        wrap(f32ToI64U),
	instr.F32_TRUNC:           wrap(f32Trunc),
	instr.F64_ABS:             wrap(f64Abs),
	instr.F64_ADD:             numericLower,
	instr.F64_CEIL:            wrap(f64Ceil),
	instr.F64_CONST:           sourceLower,
	instr.F64_COPYSIGN:        numericLower,
	instr.F64_DIV:             numericLower,
	instr.F64_EQ:              numericLower,
	instr.F64_FLOOR:           wrap(f64Floor),
	instr.F64_GE:              numericLower,
	instr.F64_GT:              numericLower,
	instr.F64_LE:              numericLower,
	instr.F64_LT:              numericLower,
	instr.F64_MAX:             numericLower,
	instr.F64_MIN:             numericLower,
	instr.F64_MOD:             numericLower,
	instr.F64_MUL:             numericLower,
	instr.F64_NE:              numericLower,
	instr.F64_NEAREST:         wrap(f64Nearest),
	instr.F64_NEG:             wrap(f64Neg),
	instr.F64_REINTERPRET_I64: wrap(f64ReinterpretI64),
	instr.F64_REM:             numericLower,
	instr.F64_SQRT:            wrap(f64Sqrt),
	instr.F64_SUB:             numericLower,
	instr.F64_TO_F32:          wrap(f64ToF32),
	instr.F64_TO_I32_S:        wrap(f64ToI32S),
	instr.F64_TO_I32_U:        wrap(f64ToI32U),
	instr.F64_TO_I64_S:        wrap(f64ToI64S),
	instr.F64_TO_I64_U:        wrap(f64ToI64U),
	instr.F64_TRUNC:           wrap(f64Trunc),
	instr.GLOBAL_GET:          sourceLower,
	instr.GLOBAL_SET:          wrap(globalSet),
	instr.GLOBAL_TEE:          wrap(globalTee),
	instr.I32_ADD:             numericLower,
	instr.I32_AND:             numericLower,
	instr.I32_CLZ:             wrap(i32Clz),
	instr.I32_CONST:           sourceLower,
	instr.I32_CTZ:             wrap(i32Ctz),
	instr.I32_DIV_S:           numericLower,
	instr.I32_DIV_U:           numericLower,
	instr.I32_EQ:              numericLower,
	instr.I32_EQZ:             numericLower,
	instr.I32_EXTEND16_S:      wrap(i32Extend16S),
	instr.I32_EXTEND8_S:       wrap(i32Extend8S),
	instr.I32_GE_S:            numericLower,
	instr.I32_GE_U:            numericLower,
	instr.I32_GT_S:            numericLower,
	instr.I32_GT_U:            numericLower,
	instr.I32_LE_S:            numericLower,
	instr.I32_LE_U:            numericLower,
	instr.I32_LT_S:            numericLower,
	instr.I32_LT_U:            numericLower,
	instr.I32_MUL:             numericLower,
	instr.I32_NE:              numericLower,
	instr.I32_OR:              numericLower,
	instr.I32_POPCNT:          wrap(i32Popcnt),
	instr.I32_REINTERPRET_F32: wrap(i32ReinterpretF32),
	instr.I32_REM_S:           numericLower,
	instr.I32_REM_U:           numericLower,
	instr.I32_ROTL:            numericLower,
	instr.I32_ROTR:            numericLower,
	instr.I32_SHL:             numericLower,
	instr.I32_SHR_S:           numericLower,
	instr.I32_SHR_U:           numericLower,
	instr.I32_SUB:             numericLower,
	instr.I32_TO_F32_S:        wrap(i32ToF32S),
	instr.I32_TO_F32_U:        wrap(i32ToF32U),
	instr.I32_TO_F64_S:        wrap(i32ToF64S),
	instr.I32_TO_F64_U:        wrap(i32ToF64U),
	instr.I32_TO_I64_S:        wrap(i32ToI64S),
	instr.I32_TO_I64_U:        wrap(i32ToI64U),
	instr.I32_XOR:             numericLower,
	instr.I64_ADD:             numericLower,
	instr.I64_AND:             numericLower,
	instr.I64_CLZ:             wrap(i64Clz),
	instr.I64_CONST:           sourceLower,
	instr.I64_CTZ:             wrap(i64Ctz),
	instr.I64_DIV_S:           numericLower,
	instr.I64_DIV_U:           numericLower,
	instr.I64_EQ:              numericLower,
	instr.I64_EQZ:             numericLower,
	instr.I64_EXTEND16_S:      wrap(i64Extend16S),
	instr.I64_EXTEND32_S:      wrap(i64Extend32S),
	instr.I64_EXTEND8_S:       wrap(i64Extend8S),
	instr.I64_GE_S:            numericLower,
	instr.I64_GE_U:            numericLower,
	instr.I64_GT_S:            numericLower,
	instr.I64_GT_U:            numericLower,
	instr.I64_LE_S:            numericLower,
	instr.I64_LE_U:            numericLower,
	instr.I64_LT_S:            numericLower,
	instr.I64_LT_U:            numericLower,
	instr.I64_MUL:             numericLower,
	instr.I64_NE:              numericLower,
	instr.I64_OR:              numericLower,
	instr.I64_POPCNT:          wrap(i64Popcnt),
	instr.I64_REINTERPRET_F64: wrap(i64ReinterpretF64),
	instr.I64_REM_S:           numericLower,
	instr.I64_REM_U:           numericLower,
	instr.I64_ROTL:            numericLower,
	instr.I64_ROTR:            numericLower,
	instr.I64_SHL:             numericLower,
	instr.I64_SHR_S:           numericLower,
	instr.I64_SHR_U:           numericLower,
	instr.I64_SUB:             numericLower,
	instr.I64_TO_F32_S:        wrap(i64ToF32S),
	instr.I64_TO_F32_U:        wrap(i64ToF32U),
	instr.I64_TO_F64_S:        wrap(i64ToF64S),
	instr.I64_TO_F64_U:        wrap(i64ToF64U),
	instr.I64_TO_I32:          wrap(i64ToI32),
	instr.I64_XOR:             numericLower,
	instr.LOCAL_GET:           sourceLower,
	instr.LOCAL_SET:           wrap(localSet),
	instr.LOCAL_TEE:           wrap(localTee),
	instr.MAP_CLEAR:           wrap(mapClear),
	instr.MAP_DELETE:          wrap(mapDelete),
	instr.MAP_GET:             wrap(mapGet),
	instr.MAP_ITER:            wrap(mapIter),
	instr.MAP_KEYS:            wrap(mapKeys),
	instr.MAP_LEN:             wrap(mapLen),
	instr.MAP_LOOKUP:          wrap(mapLookup),
	instr.MAP_NEW:             wrap(mapNew),
	instr.MAP_NEW_DEFAULT:     wrap(mapNewDefault),
	instr.MAP_SET:             wrap(mapSet),
	instr.NOP:                 wrap(nop),
	instr.REF_CAST:            wrap(refCast),
	instr.REF_EQ:              wrap(refEq),
	instr.REF_GET:             wrap(refGet),
	instr.REF_IS_NULL:         refLower,
	instr.REF_NE:              wrap(refNe),
	instr.REF_NEW:             wrap(refNew),
	instr.REF_NULL:            refSource,
	instr.REF_SET:             wrap(refSet),
	instr.REF_TEST:            wrap(refTest),
	instr.RESUME:              wrap(resume),
	instr.RETURN:              wrap(returnOp),
	instr.RETURN_CALL:         callLower,
	instr.SELECT:              wrap(selectOp),
	instr.STRING_CONCAT:       wrap(stringConcat),
	instr.STRING_ENCODE_UTF32: wrap(stringEncodeUtf32),
	instr.STRING_EQ:           wrap(stringEq),
	instr.STRING_GE:           wrap(stringGe),
	instr.STRING_GT:           wrap(stringGt),
	instr.STRING_ITER:         wrap(stringIter),
	instr.STRING_LE:           wrap(stringLe),
	instr.STRING_LEN:          wrap(stringLen),
	instr.STRING_LT:           wrap(stringLt),
	instr.STRING_NE:           wrap(stringNe),
	instr.STRING_NEW_UTF32:    wrap(stringNewUtf32),
	instr.STRUCT_GET:          indexLower,
	instr.STRUCT_NEW:          wrap(structNew),
	instr.STRUCT_NEW_DEFAULT:  wrap(structNewDefault),
	instr.STRUCT_SET:          wrap(structSet),
	instr.SWAP:                wrap(swap),
	instr.THROW:               wrap(throw),
	instr.UNREACHABLE:         wrap(unreachable),
	instr.UPVAL_GET:           sourceLower,
	instr.UPVAL_SET:           wrap(upvalSet),
	instr.YIELD:               wrap(yield),
}

func wrap(handler func() jen.Code) lowerer {
	return func(_ *loweringState, source fact) (lowering, error) {
		return lowering{source: source.step, handler: handler()}, nil
	}
}

func standaloneCode(source step, compile, body []jen.Code) jen.Code {
	code := append([]jen.Code(nil), compile...)
	code = append(code,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(source.op)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return jen.Func().Params(jen.Id("c").Op("*").Id("threader")).Params(
		jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")),
	).Block(code...)
}

func standalone(op instr.Opcode) jen.Code {
	state := loweringState{total: width(op), standalone: true}
	lowered, err := lowerers[op](&state, fact{step: step{op: op}, kind: instr.KindAny})
	if err != nil {
		panic(err)
	}
	if lowered.handler != nil {
		return lowered.handler
	}
	if len(lowered.compile) == 0 {
		panic(fmt.Sprintf("no standalone lowering for %s", instr.TypeOf(op).Mnemonic))
	}
	return jen.Func().Params(jen.Id("c").Op("*").Id("threader")).Params(
		jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")),
	).Block(lowered.compile...)
}

func compose(pattern pattern, total int, label string) ([]jen.Code, error) {
	facts, err := prepare(pattern)
	if err != nil {
		return nil, err
	}

	state := loweringState{total: total, label: label}
	var lowered lowering
	for _, source := range facts {
		lower := lowerers[source.op]
		if lower == nil {
			return nil, fmt.Errorf("no lowering for %s", instr.TypeOf(source.op).Mnemonic)
		}
		lowered, err = lower(&state, source)
		if err != nil {
			return nil, err
		}
		state.offset += width(source.op)
	}
	if lowered.handler != nil || len(lowered.compile) == 0 {
		consumer := facts[len(facts)-1].op
		return nil, fmt.Errorf("no fusion lowering for %s", instr.TypeOf(consumer).Mnemonic)
	}
	return lowered.compile, nil
}

func prepare(source pattern) ([]fact, error) {
	facts := make([]fact, len(source))
	for index, current := range source {
		facts[index] = fact{step: current, kind: instr.KindAny}
	}
	if len(facts) == 0 {
		return nil, fmt.Errorf("empty fusion pattern")
	}

	consumerAt := len(facts) - 1
	branch := facts[consumerAt].op == instr.BR_IF
	if branch {
		consumerAt--
	}
	if consumerAt < 0 {
		return nil, fmt.Errorf("fusion pattern has no consumer")
	}
	consumer := facts[consumerAt].op
	if consumerAt == 0 {
		if !branch {
			return nil, fmt.Errorf("fusion pattern has no source")
		}
		push := instr.TypeOf(consumer).Push
		if len(push) == 0 || push[len(push)-1].Repr() != instr.KindI32 {
			return nil, fmt.Errorf("%s cannot feed br_if", instr.TypeOf(consumer).Mnemonic)
		}
		facts[0].kind = push[len(push)-1].Repr()
		return facts, nil
	}

	kind, count, ok := fusionInput(consumer)
	if !ok {
		return nil, fmt.Errorf("no fusion lowering for %s", instr.TypeOf(consumer).Mnemonic)
	}
	if consumerAt > count {
		return nil, fmt.Errorf("%s accepts at most %d fused sources", instr.TypeOf(consumer).Mnemonic, count)
	}
	boxed := kind.Repr() == instr.KindRef || consumer == instr.I32_XOR || consumer == instr.I32_AND || consumer == instr.I32_OR
	for index := range facts[:consumerAt] {
		facts[index].kind = kind.Repr()
		facts[index].boxed = boxed
		facts[index].commit = traps(consumer)
	}
	return facts, nil
}

func fusionInput(op instr.Opcode) (instr.Kind, int, bool) {
	pop := instr.TypeOf(op).Pop
	if len(pop) > 0 {
		return pop[0], len(pop), true
	}
	switch op {
	case instr.ARRAY_GET, instr.STRUCT_GET:
		return instr.KindI32, 1, true
	case instr.CALL, instr.RETURN_CALL, instr.CLOSURE_NEW:
		return instr.KindRef, 1, true
	default:
		return instr.KindAny, 0, false
	}
}

func sourceLower(state *loweringState, source fact) (lowering, error) {
	if state.standalone {
		switch source.op {
		case instr.I32_CONST:
			source.kind = instr.KindI32
		case instr.I64_CONST:
			source.kind = instr.KindI64
		case instr.F32_CONST:
			source.kind = instr.KindF32
		case instr.F64_CONST:
			source.kind = instr.KindF64
		}
	}
	lowered, err := sourceAccess(source, len(state.pending), state.offset, state.label, state.standalone)
	if err != nil {
		return lowering{}, err
	}
	if state.standalone {
		lowered.handler = standaloneCode(source.step, lowered.compile, lowered.push)
		return lowered, nil
	}
	state.pending = append(state.pending, lowered)
	return lowered, nil
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

func refSource(state *loweringState, source fact) (lowering, error) {
	lowered := lowering{source: source.step}
	switch source.op {
	case instr.REF_NULL:
		lowered.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxedNull")
		lowered.check = append(lowered.check, overflow())
		lowered.push = append(lowered.push, lowered.check...)
		lowered.push = append(lowered.push,
			jen.Id("i").Dot("retain").Call(jen.Lit(0)),
			jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(lowered.boxed),
			jen.Id("i").Dot("sp").Op("++"),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"),
		)
	case instr.DUP:
		lowered.boxed = jen.Id("value")
		lowered.check = append(lowered.check,
			jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			overflow(),
		)
		lowered.body = append(lowered.body,
			jen.Id("value").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
		)
		lowered.push = append(lowered.push, lowered.check...)
		lowered.push = append(lowered.push, lowered.body...)
		lowered.push = append(lowered.push,
			jen.Id("i").Dot("retainBox").Call(jen.Id("value")),
			jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("value"),
			jen.Id("i").Dot("sp").Op("++"),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"),
		)
	default:
		return lowering{}, fmt.Errorf("unsupported ref source %s", instr.TypeOf(source.op).Mnemonic)
	}
	if state.standalone {
		lowered.handler = standaloneCode(source.step, lowered.compile, lowered.push)
		return lowered, nil
	}
	state.pending = append(state.pending, lowered)
	return lowered, nil
}

func refLower(state *loweringState, source fact) (lowering, error) {
	if state.standalone {
		state.pending = []lowering{{
			source:   source.step,
			boxed:    jen.Id("value"),
			resident: true,
			check: []jen.Code{
				jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			},
			body: []jen.Code{
				jen.Id("value").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
			},
			discard: []jen.Code{
				jen.Id("i").Dot("releaseBox").Call(jen.Id("value")),
			},
		}}
	}
	if len(state.pending) == 0 {
		return lowering{}, fmt.Errorf("%s needs one pending value", instr.TypeOf(source.op).Mnemonic)
	}
	value := state.pending[len(state.pending)-1]
	compile := append([]jen.Code(nil), value.compile...)
	body := append([]jen.Code(nil), value.check...)

	switch source.op {
	case instr.DROP:
		if value.resident {
			body = append(body, value.body...)
			body = append(body, value.discard...)
			body = append(body, jen.Id("i").Dot("sp").Op("--"))
		}
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.total))
	case instr.REF_IS_NULL:
		body = append(body, value.body...)
		condition := jen.Add(value.boxed).Dot("Ref").Call().Op("==").Lit(0)
		if !state.standalone && state.offset+width(source.op) < state.total {
			lowered := lowering{source: source.step, compile: compile, check: value.check, body: value.body, raw: condition}
			state.pending = []lowering{lowered}
			return lowered, nil
		}
		body = append(body, value.discard...)
		if value.resident {
			body = append(body,
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(condition),
			)
		} else {
			body = append(body,
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(condition),
				jen.Id("i").Dot("sp").Op("++"),
			)
		}
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.total))
	default:
		return lowering{}, fmt.Errorf("unsupported ref consumer %s", instr.TypeOf(source.op).Mnemonic)
	}

	if state.standalone {
		return lowering{source: source.step, handler: standaloneCode(source.step, compile, body)}, nil
	}
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(value.source.op)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return lowering{source: source.step, compile: compile}, nil
}

func indexLower(state *loweringState, source fact) (lowering, error) {
	if state.standalone {
		body := []jen.Code{
			jen.If(jen.Id("i").Dot("sp").Op("<").Lit(2)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.Id("index").Op(":=").Int().Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Dot("I32").Call()),
			jen.Id("i").Dot("sp").Op("--"),
		}
		body = append(body, indexCode(source.op, jen.Id("index"), width(source.op))...)
		return lowering{source: source.step, handler: standaloneCode(source.step, nil, body)}, nil
	}
	if len(state.pending) != 1 {
		return lowering{}, fmt.Errorf("%s needs one constant index", instr.TypeOf(source.op).Mnemonic)
	}
	index := state.pending[0]
	compile := append([]jen.Code(nil), index.compile...)
	body := append([]jen.Code(nil), index.check...)
	body = append(body, index.body...)
	body = append(body, indexCode(source.op, jen.Int().Call(index.raw), state.total)...)
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(index.source.op)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return lowering{source: source.step, compile: compile}, nil
}

func callLower(state *loweringState, source fact) (lowering, error) {
	if state.standalone {
		body, err := dynamicCallCode(source.op)
		if err != nil {
			return lowering{}, err
		}
		return lowering{source: source.step, handler: standaloneCode(source.step, nil, body)}, nil
	}
	if len(state.pending) != 1 || state.pending[0].source.op != instr.CONST_GET {
		return lowering{}, fmt.Errorf("%s needs one constant target", instr.TypeOf(source.op).Mnemonic)
	}
	target := state.pending[0]
	compile := append([]jen.Code(nil), target.compile...)
	compile = append(compile,
		jen.Id("addr").Op(":=").Add(target.boxed).Dot("Ref").Call(),
		jen.If(jen.Id("addr").Op("<").Lit(0).Op("||").Id("addr").Op(">=").Len(jen.Id("c").Dot("heap"))).Block(reject(state.label)),
	)
	switch source.op {
	case instr.CALL:
		compile = append(compile, dispatch(false, state.label, state.total))
	case instr.RETURN_CALL:
		compile = append(compile, dispatch(true, state.label, state.total))
	case instr.CLOSURE_NEW:
		compile = append(compile, create(state.label, state.total))
	default:
		return lowering{}, fmt.Errorf("unsupported call opcode %s", instr.TypeOf(source.op).Mnemonic)
	}
	return lowering{source: source.step, compile: compile}, nil
}

func dynamicCallCode(op instr.Opcode) ([]jen.Code, error) {
	prefix := []jen.Code{
		jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("target").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
		jen.If(jen.Id("target").Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("addr").Op(":=").Id("target").Dot("Ref").Call(),
	}
	if op == instr.CLOSURE_NEW {
		body := append(prefix,
			jen.List(jen.Id("fn"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("captures").Op(":=").Len(jen.Id("fn").Dot("Captures")),
		)
		body = append(body, createBody(1, false, 1)...)
		return body, nil
	}
	if op != instr.CALL && op != instr.RETURN_CALL {
		return nil, fmt.Errorf("unsupported call opcode %s", instr.TypeOf(op).Mnemonic)
	}
	tail := op == instr.RETURN_CALL
	function := []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
		jen.Id("locals").Op(":=").Len(jen.Id("fn").Dot("Locals")),
	}
	target := callTarget{addr: jen.Id("addr"), upvals: jen.Nil(), ref: jen.Id("addr")}
	if tail {
		function = append(function, replaceBody(target, 1, true, 1)...)
	} else {
		function = append(function, frameBody(target, 1, true, 1, jen.Id("addr"))...)
	}
	closure := []jen.Code{
		jen.List(jen.Id("tmpl"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("fn").Dot("Fn")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
		jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
		jen.Id("locals").Op(":=").Len(jen.Id("tmpl").Dot("Locals")),
	}
	target = callTarget{addr: jen.Int().Parens(jen.Id("fn").Dot("Fn")), upvals: jen.Id("fn").Dot("Upvals"), ref: jen.Id("addr")}
	if tail {
		closure = append(closure, replaceBody(target, 1, true, 1)...)
	} else {
		closure = append(closure, frameBody(target, 1, true, 1, jen.Int().Parens(jen.Id("fn").Dot("Fn")))...)
	}
	host := []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
	}
	host = append(host, hostBody(1, 1, tail)...)
	body := append(prefix, jen.Switch(jen.Id("fn").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")).Block(function...),
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Closure")).Block(closure...),
		jen.Case(jen.Op("*").Id("HostFunction")).Block(host...),
		jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
	))
	return body, nil
}

func numericLower(state *loweringState, source fact) (lowering, error) {
	if !state.standalone && state.offset+width(source.op) < state.total {
		lowered := lowering{source: source.step}
		state.pending = append(state.pending, lowered)
		return lowered, nil
	}
	body, err := numericCode(source.step, state.pending, state.total, state.label, false)
	return lowering{source: source.step, compile: body}, err
}

func branchLower(state *loweringState, source fact) (lowering, error) {
	if len(state.pending) > 0 {
		consumer := state.pending[len(state.pending)-1]
		if _, ok := arity(consumer.source.op); ok {
			body, err := numericCode(consumer.source, state.pending[:len(state.pending)-1], state.total, state.label, true)
			return lowering{source: source.step, compile: body}, err
		}
		if consumer.raw != nil {
			condition := consumer.raw
			if consumer.source.op == instr.I32_CONST {
				condition = jen.Add(condition).Op("!=").Lit(0)
			}
			compile := append([]jen.Code(nil), consumer.compile...)
			body := append([]jen.Code(nil), consumer.check...)
			body = append(body, consumer.body...)
			body = append(body,
				jen.If(condition).Block(jen.Id("i").Dot("fr").Dot("ip").Op("+=").Id("offset")),
				jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.total),
			)
			compile = append(compile,
				jen.Id("c").Dot("ip").Op("+=").Lit(width(consumer.source.op)),
				jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
			)
			return lowering{source: source.step, compile: compile}, nil
		}
	}
	return lowering{source: source.step, handler: brIf()}, nil
}

func dispatch(tail bool, label string, total int) jen.Code {
	return jen.Switch(jen.Id("fn").Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")).Block(direct(tail, label, total)...),
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Closure")).Block(closure(tail, label, total)...),
		jen.Case(jen.Op("*").Id("HostFunction")).Block(host(tail, total)...),
		jen.Default().Block(reject(label)),
	)
}

func direct(tail bool, label string, total int) []jen.Code {
	guard := jen.If(jen.Id("addr").Op("<").Len(jen.Id("c").Dot("coros")).Op("&&").Id("c").Dot("coros").Index(jen.Id("addr"))).Block(reject(label))
	target := callTarget{addr: jen.Id("addr"), upvals: jen.Nil(), ref: jen.Id("addr")}
	if tail {
		return append([]jen.Code{guard}, replace(target, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("fn").Dot("Locals")), total)...)
	}
	return append([]jen.Code{guard}, frame(target, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("fn").Dot("Locals")), total)...)
}

func closure(tail bool, label string, total int) []jen.Code {
	preflight := []jen.Code{
		jen.Id("tmpl").Op(",").Id("ok").Op(":=").Id("c").Dot("heap").Index(jen.Id("fn").Dot("Fn")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
		jen.If(jen.Op("!").Id("ok")).Block(reject(label)),
		jen.If(jen.Int().Parens(jen.Id("fn").Dot("Fn")).Op("<").Len(jen.Id("c").Dot("coros")).Op("&&").Id("c").Dot("coros").Index(jen.Id("fn").Dot("Fn"))).Block(reject(label)),
	}
	target := callTarget{addr: jen.Int().Parens(jen.Id("fn").Dot("Fn")), upvals: jen.Id("fn").Dot("Upvals"), ref: jen.Id("addr")}
	if tail {
		return append(preflight, replace(target, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("tmpl").Dot("Locals")), total)...)
	}
	return append(preflight, frame(target, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("tmpl").Dot("Locals")), total)...)
}

func frame(target callTarget, typ, locals jen.Code, total int) []jen.Code {
	return []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Add(typ).Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Add(typ).Dot("Returns")),
		jen.Id("locals").Op(":=").Add(locals),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(frameBody(target, 0, false, total, nil)...)),
	}
}

func frameBody(target callTarget, targetSlots int, releaseTarget bool, advance int, coroutine jen.Code) []jen.Code {
	body := []jen.Code{}
	if targetSlots == 0 {
		body = append(body, overflow())
	}
	body = append(body,
		jen.If(jen.Id("i").Dot("fp").Op("==").Len(jen.Id("i").Dot("frames"))).Block(jen.Panic(jen.Id("ErrFrameOverflow"))),
		jen.If(jen.Id("i").Dot("sp").Op("<").Lit(targetSlots).Op("+").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.If(adjust(jen.Id("i").Dot("sp").Op("+").Id("locals"), -targetSlots).Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
	)
	start := func() jen.Code { return adjust(jen.Id("i").Dot("sp"), -targetSlots) }
	body = append(body,
		jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(start(), jen.Add(start()).Op("+").Id("locals"))),
		jen.Id("f").Op(":=").Op("&").Id("i").Dot("frames").Index(jen.Id("i").Dot("fp")),
		jen.Id("f").Dot("code").Op("=").Id("i").Dot("code").Index(jen.Add(target.addr)),
		jen.Id("f").Dot("upvals").Op("=").Add(target.upvals),
		jen.Id("f").Dot("addr").Op("=").Add(target.addr),
		jen.Id("f").Dot("ref").Op("=").Add(target.ref),
		jen.Id("f").Dot("ip").Op("=").Lit(0),
		jen.Id("f").Dot("bp").Op("=").Id("i").Dot("sp").Op("-").Id("params").Op("-").Lit(targetSlots),
		jen.Id("f").Dot("returns").Op("=").Id("returns"),
		jen.Id("f").Dot("release").Op("=").Add(boolean(releaseTarget)),
		jen.Id("f").Dot("coro").Op("=").Lit(0),
	)
	if coroutine != nil {
		body = append(body, jen.If(jen.Add(coroutine).Op("<").Len(jen.Id("i").Dot("coros")).Op("&&").Id("i").Dot("coros").Index(jen.Add(coroutine))).Block(
			jen.Id("f").Dot("coro").Op("=").Id("i").Dot("alloc").Call(jen.Op("&").Id("Coroutine").Values(jen.Dict{jen.Id("typ"): jen.Id("fn").Dot("Typ")})),
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

func replace(target callTarget, typ, locals jen.Code, total int) []jen.Code {
	return []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Add(typ).Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Add(typ).Dot("Returns")),
		jen.Id("locals").Op(":=").Add(locals),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(replaceBody(target, 0, false, total)...)),
	}
}

func replaceBody(target callTarget, targetSlots int, releaseTarget bool, advance int) []jen.Code {
	body := []jen.Code{}
	if targetSlots == 0 {
		body = append(body, overflow())
	}
	body = append(body,
		jen.If(jen.Id("i").Dot("sp").Op("<").Lit(targetSlots).Op("+").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.If(jen.Id("i").Dot("fp").Op("==").Lit(1)).Block(
			jen.If(jen.Id("i").Dot("fp").Op("==").Len(jen.Id("i").Dot("frames"))).Block(jen.Panic(jen.Id("ErrFrameOverflow"))),
			jen.If(adjust(jen.Id("i").Dot("sp").Op("+").Id("locals"), -targetSlots).Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
			jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(adjust(jen.Id("i").Dot("sp"), -targetSlots), adjust(jen.Id("i").Dot("sp").Op("+").Id("locals"), -targetSlots))),
			jen.Id("f").Op(":=").Op("&").Id("i").Dot("frames").Index(jen.Id("i").Dot("fp")),
			jen.Id("f").Dot("code").Op("=").Id("i").Dot("code").Index(jen.Add(target.addr)),
			jen.Id("f").Dot("upvals").Op("=").Add(target.upvals),
			jen.Id("f").Dot("addr").Op("=").Add(target.addr),
			jen.Id("f").Dot("ref").Op("=").Add(target.ref),
			jen.Id("f").Dot("ip").Op("=").Lit(0),
			jen.Id("f").Dot("bp").Op("=").Id("i").Dot("sp").Op("-").Id("params").Op("-").Lit(targetSlots),
			jen.Id("f").Dot("returns").Op("=").Id("returns"),
			jen.Id("f").Dot("release").Op("=").Add(boolean(releaseTarget)),
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
		jen.Copy(jen.Id("i").Dot("stack").Index(jen.Id("base").Op(":").Id("base").Op("+").Id("params")), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("params").Op("-").Lit(targetSlots).Op(":").Id("i").Dot("sp").Op("-").Lit(targetSlots))),
		jen.If(jen.Id("f").Dot("release")).Block(jen.Id("i").Dot("release").Call(jen.Id("f").Dot("ref"))),
		jen.If(jen.Id("locals").Op(">").Lit(0)).Block(clearRange(jen.Id("base").Op("+").Id("params"), jen.Id("base").Op("+").Id("params").Op("+").Id("locals"))),
		jen.Id("f").Dot("code").Op("=").Id("i").Dot("code").Index(jen.Add(target.addr)),
		jen.Id("f").Dot("upvals").Op("=").Add(target.upvals),
		jen.Id("f").Dot("addr").Op("=").Add(target.addr),
		jen.Id("f").Dot("ref").Op("=").Add(target.ref),
		jen.Id("f").Dot("ip").Op("=").Lit(0),
		jen.Id("f").Dot("returns").Op("=").Id("returns"),
		jen.Id("f").Dot("release").Op("=").Add(boolean(releaseTarget)),
		jen.Id("f").Dot("coro").Op("=").Lit(0),
		jen.Id("i").Dot("sp").Op("=").Id("base").Op("+").Id("params").Op("+").Id("locals"),
	)
	return body
}

func host(tail bool, total int) []jen.Code {
	return []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(hostBody(0, total, tail)...)),
	}
}

func hostBody(targetSlots, advance int, tail bool) []jen.Code {
	body := []jen.Code{}
	if targetSlots == 0 {
		body = append(body, overflow())
	}
	body = append(body,
		jen.If(jen.Id("i").Dot("sp").Op("<").Lit(targetSlots).Op("+").Id("params")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.If(adjust(jen.Id("i").Dot("sp").Op("+").Id("returns").Op("-").Id("params"), -targetSlots).Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
		jen.Id("args").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("params").Op("-").Lit(targetSlots).Op(":").Id("i").Dot("sp").Op("-").Lit(targetSlots)),
		jen.Id("out").Op(",").Id("err").Op(":=").Id("fn").Dot("Fn").Call(jen.Id("i"), jen.Id("args")),
		jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
		release(jen.Id("args"), jen.Id("out")),
	)
	if targetSlots > 0 {
		body = append(body, release(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(targetSlots).Op(":").Id("i").Dot("sp")), jen.Id("out")))
	}
	body = append(body,
		jen.Id("i").Dot("sp").Op("+=").Id("returns").Op("-").Id("params").Op("-").Lit(targetSlots),
		jen.Copy(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("returns").Op(":").Id("i").Dot("sp")), jen.Id("out")),
	)
	if tail {
		body = append(body, jen.If(jen.Id("i").Dot("fp").Op(">").Lit(1)).Block(retire()...))
	} else {
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance))
	}
	return body
}

func create(label string, total int) jen.Code {
	return jen.Switch(jen.Id("fn").Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")).Block(
			jen.Id("captures").Op(":=").Len(jen.Id("fn").Dot("Captures")),
			jen.Id("c").Dot("ip").Op("+=").Lit(3),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(createBody(0, true, total)...)),
		),
		jen.Default().Block(reject(label)),
	)
}

func createBody(targetSlots int, borrowed bool, advance int) []jen.Code {
	body := []jen.Code{}
	if targetSlots == 0 {
		body = append(body, overflow())
	}
	body = append(body,
		jen.If(jen.Id("i").Dot("sp").Op("<").Id("captures").Op("+").Lit(targetSlots)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("base").Op(":=").Id("i").Dot("sp").Op("-").Id("captures").Op("-").Lit(targetSlots),
		jen.Id("upvals").Op(":=").Append(jen.Index().Qual("github.com/siyul-park/minivm/types", "Boxed").Values(), jen.Id("i").Dot("stack").Index(jen.Id("base").Op(":").Id("base").Op("+").Id("captures")).Op("...")),
	)
	if borrowed {
		body = append(body, jen.Id("i").Dot("retain").Call(jen.Id("addr")))
	}
	body = append(body,
		jen.Id("closure").Op(":=").Qual("github.com/siyul-park/minivm/types", "NewClosure").Call(jen.Id("fn").Dot("Typ"), jen.Qual("github.com/siyul-park/minivm/types", "Ref").Parens(jen.Id("addr")), jen.Id("upvals")),
		jen.Id("i").Dot("sp").Op("=").Id("base"),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxRef").Call(jen.Id("i").Dot("keep").Call(jen.Id("closure"))),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
	)
	return body
}

func boolean(value bool) jen.Code {
	if value {
		return jen.True()
	}
	return jen.False()
}

func clearRange(start, end jen.Code) jen.Code {
	return jen.Clear(jen.Id("i").Dot("stack").Index(jen.Add(start).Op(":").Add(end)))
}

func adjust(value jen.Code, delta int) *jen.Statement {
	if delta < 0 {
		return jen.Add(value).Op("-").Lit(-delta)
	}
	if delta > 0 {
		return jen.Add(value).Op("+").Lit(delta)
	}
	return jen.Add(value)
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

func retire() []jen.Code {
	return []jen.Code{
		jen.Id("f").Op(":=").Id("i").Dot("fr"),
		jen.If(jen.Id("i").Dot("sp").Op("<").Id("f").Dot("returns")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
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

func bounds(offset, size, length jen.Code) jen.Code {
	return jen.If(jen.Add(offset).Op("<").Lit(0).Op("||").Add(offset).Op("+").Add(size).Op(">").Add(length)).Block(
		jen.Panic(jen.Id("ErrIndexOutOfRange")),
	)
}

func overflow() jen.Code {
	return jen.If(jen.Id("i").Dot("sp").Op("==").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow")))
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

// index emits direct constant-index ARRAY_GET and STRUCT_GET handlers.
func indexCode(op instr.Opcode, index jen.Code, total int) []jen.Code {
	body := []jen.Code{
		jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("ref").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
		jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("addr").Op(":=").Id("ref").Dot("Ref").Call(),
		jen.Var().Id("result").Qual("github.com/siyul-park/minivm/types", "Boxed"),
	}
	if op == instr.ARRAY_GET {
		body = append(body, jen.Switch(jen.Id("array").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
			jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "TypedArray").Types(jen.Bool())).Block(
				bounds(index, jen.Lit(1), jen.Len(jen.Id("array"))),
				jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(jen.Id("array").Index(index)),
			),
			jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "TypedArray").Types(jen.Int8())).Block(
				bounds(index, jen.Lit(1), jen.Len(jen.Id("array"))),
				jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI8").Call(jen.Id("array").Index(index)),
			),
			jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "TypedArray").Types(jen.Int32())).Block(
				bounds(index, jen.Lit(1), jen.Len(jen.Id("array"))),
				jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(jen.Id("array").Index(index)),
			),
			jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "TypedArray").Types(jen.Int64())).Block(
				bounds(index, jen.Lit(1), jen.Len(jen.Id("array"))),
				jen.Id("result").Op("=").Id("i").Dot("boxI64").Call(jen.Id("array").Index(index)),
			),
			jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "TypedArray").Types(jen.Float32())).Block(
				bounds(index, jen.Lit(1), jen.Len(jen.Id("array"))),
				jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(jen.Id("array").Index(index)),
			),
			jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "TypedArray").Types(jen.Float64())).Block(
				bounds(index, jen.Lit(1), jen.Len(jen.Id("array"))),
				jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(jen.Id("array").Index(index)),
			),
			jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Array")).Block(
				bounds(index, jen.Lit(1), jen.Len(jen.Id("array").Dot("Elems"))),
				jen.Id("result").Op("=").Id("array").Dot("Elems").Index(index),
				jen.Id("i").Dot("retainBox").Call(jen.Id("result")),
			),
			jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		))
	} else {
		field := func() jen.Code { return jen.Id("value").Dot("Typ").Dot("Fields").Index(index) }
		data := func() jen.Code { return jen.Id("value").Dot("Data").Index(index) }
		body = append(body, jen.Switch(jen.Id("value").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
			jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Struct")).Block(
				jen.If(jen.Add(index).Op("<").Lit(0).Op("||").Add(index).Op(">=").Len(jen.Id("value").Dot("Typ").Dot("Fields"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
				jen.Switch(jen.Add(field()).Dot("Kind")).Block(
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindI32")).Block(jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(jen.Int32().Call(jen.Uint32().Call(data())))),
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindI8")).Block(jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI8").Call(jen.Int8().Call(jen.Uint32().Call(data())))),
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindI1")).Block(jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(jen.Add(data()).Op("!=").Lit(0))),
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindI64")).Block(jen.Id("result").Op("=").Id("i").Dot("boxI64").Call(jen.Int64().Call(data()))),
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindF32")).Block(jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(jen.Qual("math", "Float32frombits").Call(jen.Uint32().Call(data())))),
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindF64")).Block(jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(jen.Qual("math", "Float64frombits").Call(data()))),
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(
						jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "Boxed").Call(data()),
						jen.Id("i").Dot("retainBox").Call(jen.Id("result")),
					),
					jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
				),
			),
			jen.Case(jen.Op("*").Id("HostObject")).Block(
				jen.If(jen.Add(index).Op("<").Lit(0).Op("||").Add(index).Op(">=").Len(jen.Id("value").Dot("Typ").Dot("Fields"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
				jen.Switch(jen.Id("value").Dot("Typ").Dot("Fields").Index(index).Dot("Kind")).Block(
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindI32"), jen.Qual("github.com/siyul-park/minivm/types", "KindI8"), jen.Qual("github.com/siyul-park/minivm/types", "KindI1"), jen.Qual("github.com/siyul-park/minivm/types", "KindF32"), jen.Qual("github.com/siyul-park/minivm/types", "KindF64")).Block(jen.Id("result").Op("=").Id("value").Dot("Field").Call(index)),
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindI64")).Block(jen.Id("result").Op("=").Id("i").Dot("boxI64").Call(jen.Int64().Call(jen.Id("value").Dot("Raw").Call(index)))),
					jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(
						jen.Id("result").Op("=").Qual("github.com/siyul-park/minivm/types", "Boxed").Call(jen.Id("value").Dot("Raw").Call(index)),
						jen.Id("i").Dot("retainBox").Call(jen.Id("result")),
					),
					jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
				),
			),
			jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		))
	}
	return append(body,
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("--"),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("result"),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(total),
	)
}

// arithmetic emits preflight plus one concrete runtime handler for a
// normalized pattern. Every source step is inline in that handler.
func numericCode(consumer step, sources []lowering, total int, label string, branch bool) ([]jen.Code, error) {
	arity, ok := arity(consumer.op)
	if !ok {
		return nil, fmt.Errorf("unsupported numeric consumer %s", instr.TypeOf(consumer.op).Mnemonic)
	}
	if len(sources) > arity {
		return nil, fmt.Errorf("numeric pattern has %d sources", len(sources))
	}
	kind, _ := number(consumer.op)
	if traps(consumer.op) {
		return trappedNumericCode(consumer, sources, kind)
	}

	var compile, body []jen.Code
	for _, source := range sources {
		compile = append(compile, source.compile...)
		body = append(body, source.check...)
		body = append(body, source.body...)
	}

	operands := make([]jen.Code, 0, arity)
	missing := arity - len(sources)
	if missing > 0 {
		body = append(body, jen.If(jen.Id("i").Dot("sp").Op("<").Lit(missing)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))))
		for index := missing; index > 0; index-- {
			operands = append(operands, take(kind, jen.Id("i").Dot("sp").Op("-").Lit(index)))
		}
	}
	boxed := consumer.op == instr.I32_XOR || consumer.op == instr.I32_AND || consumer.op == instr.I32_OR
	for _, source := range sources {
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

	result := slot(len(sources))
	body = append(body, jen.Id(result).Op(":=").Add(apply(consumer.op, operands...)))
	if branch {
		delta := len(sources) - arity
		if delta > 0 {
			body = append(body, jen.Id("i").Dot("sp").Op("+=").Lit(delta))
		} else if delta < 0 {
			body = append(body, jen.Id("i").Dot("sp").Op("-=").Lit(-delta))
		}
		body = append(body,
			jen.If(jen.Id(result).Dot("Bool").Call()).Block(jen.Id("i").Dot("fr").Dot("ip").Op("+=").Id("offset")),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(total),
		)
	} else {
		delta := len(sources) - arity + 1
		if delta > 0 {
			body = append(body, jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id(result), jen.Id("i").Dot("sp").Op("++"))
		} else {
			if delta < 0 {
				body = append(body, jen.Id("i").Dot("sp").Op("-=").Lit(-delta))
			}
			body = append(body, jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Op("=").Id(result))
		}
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(total))
	}

	first := consumer.op
	if len(sources) > 0 {
		first = sources[0].source.op
	}
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(first)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return compile, nil
}

func trappedNumericCode(consumer step, sources []lowering, kind string) ([]jen.Code, error) {
	var compile, body []jen.Code
	for _, source := range sources {
		compile = append(compile, source.compile...)
		body = append(body, source.push...)
	}
	body = append(body,
		jen.If(jen.Id("i").Dot("sp").Op("<").Lit(2)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("rhs").Op(":=").Add(take(kind, jen.Id("i").Dot("sp").Op("-").Lit(1))),
		jen.Id("lhs").Op(":=").Add(take(kind, jen.Id("i").Dot("sp").Op("-").Lit(2))),
		jen.If(jen.Id("rhs").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrDivideByZero"))),
		jen.Id("result").Op(":=").Add(apply(consumer.op, jen.Id("lhs"), jen.Id("rhs"))),
		jen.Id("i").Dot("sp").Op("--"),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Op("=").Id("result"),
		jen.Id("i").Dot("fr").Dot("ip").Op("++"),
	)
	first := consumer.op
	if len(sources) > 0 {
		first = sources[0].source.op
	}
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(first)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return compile, nil
}

func apply(op instr.Opcode, values ...jen.Code) jen.Code {
	lhs := values[0]
	if len(values) == 1 {
		switch op {
		case instr.I32_EQZ, instr.I64_EQZ:
			return jen.Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(jen.Add(lhs).Op("==").Lit(0))
		}
	}
	rhs := values[1]
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

func sourceAccess(source fact, number, offset int, label string, standalone bool) (lowering, error) {
	op := source.op
	width := width(op)
	name := slot(number)
	boxed := box(name)
	idx := fmt.Sprintf("i%d", number)
	addr := fmt.Sprintf("i%d", 2+number)
	at := add(jen.Id("start"), offset)
	if standalone {
		at = jen.Id("c").Dot("ip")
	}
	result := lowering{source: source.step, boxed: jen.Id(boxed)}
	result.check = append(result.check, overflow())

	rejectBounds := func(field string) jen.Code {
		condition := jen.Id(idx).Op(">=").Len(jen.Id("c").Dot(field))
		if source.kind == instr.KindAny {
			return jen.If(condition).Block(
				jen.Id("c").Dot("ip").Op("+=").Lit(width),
				jen.Return(jen.Func().Params(jen.Op("*").Id("Interpreter")).Block(jen.Panic(jen.Id("ErrSegmentationFault")))),
			)
		}
		kind, ok := kindName(source.kind)
		if !ok {
			return jen.Null()
		}
		expected := jen.Qual("github.com/siyul-park/minivm/types", "Kind"+kind)
		return check(field, idx, expected, label)
	}

	switch op {
	case instr.LOCAL_GET, instr.UPVAL_GET:
		result.compile = append(result.compile,
			jen.Id(idx).Op(":=").Int().Call(jen.Id("c").Dot("code").Index(jen.Add(at).Op("+").Lit(1))),
		)
	case instr.GLOBAL_GET, instr.CONST_GET:
		result.compile = append(result.compile,
			jen.Id(idx).Op(":=").Qual("github.com/siyul-park/minivm/instr", "ParseU16").Call(jen.Id("c").Dot("code"), jen.Add(at).Op("+").Lit(1)),
		)
	}

	switch op {
	case instr.LOCAL_GET:
		result.compile = append(result.compile, rejectBounds("locals"))
		result.check = append(result.check,
			jen.Id(addr).Op(":=").Id("i").Dot("fr").Dot("bp").Op("+").Id(idx),
			jen.If(jen.Id(addr).Op(">=").Id("i").Dot("sp")).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
		)
		result.body = append(result.body, jen.Id(boxed).Op(":=").Id("i").Dot("stack").Index(jen.Id(addr)))
	case instr.GLOBAL_GET:
		result.compile = append(result.compile, rejectBounds("globals"))
		result.check = append(result.check,
			jen.If(jen.Id(idx).Op(">=").Len(jen.Id("i").Dot("globals"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
		)
		result.body = append(result.body, jen.Id(boxed).Op(":=").Id("i").Dot("globals").Index(jen.Id(idx)))
	case instr.UPVAL_GET:
		result.compile = append(result.compile, rejectBounds("captures"))
		result.check = append(result.check,
			jen.If(jen.Id(idx).Op(">=").Len(jen.Id("i").Dot("fr").Dot("upvals"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
		)
		result.body = append(result.body, jen.Id(boxed).Op(":=").Id("i").Dot("fr").Dot("upvals").Index(jen.Id(idx)))
	case instr.CONST_GET:
		condition := jen.Id(idx).Op(">=").Len(jen.Id("c").Dot("constants"))
		if source.kind == instr.KindAny {
			result.compile = append(result.compile,
				jen.If(condition).Block(
					jen.Id("c").Dot("ip").Op("+=").Lit(width),
					jen.Return(jen.Func().Params(jen.Op("*").Id("Interpreter")).Block(jen.Panic(jen.Id("ErrSegmentationFault")))),
				),
			)
		} else {
			result.compile = append(result.compile, jen.If(condition).Block(reject(label)))
		}
		result.compile = append(result.compile, jen.Id(boxed).Op(":=").Id("c").Dot("constants").Index(jen.Id(idx)))
		if source.kind != instr.KindAny {
			kind, ok := kindName(source.kind)
			if !ok {
				return lowering{}, fmt.Errorf("unsupported source kind %s", source.kind)
			}
			expected := jen.Qual("github.com/siyul-park/minivm/types", "Kind"+kind)
			if kind == "I64" {
				result.compile = append(result.compile,
					jen.Switch(jen.Id(boxed).Dot("Kind").Call()).Block(
						jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindI64")),
						jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(
							jen.Id("constantRef").Op(":=").Id(boxed).Dot("Ref").Call(),
							jen.If(jen.Id("constantRef").Op("<").Lit(0).Op("||").Id("constantRef").Op(">=").Len(jen.Id("c").Dot("heap"))).Block(reject(label)),
							jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id("constantRef")).Assert(jen.Qual("github.com/siyul-park/minivm/types", "I64")),
							jen.If(jen.Op("!").Id("ok")).Block(reject(label)),
						),
						jen.Default().Block(reject(label)),
					),
				)
			} else {
				result.compile = append(result.compile,
					jen.If(jen.Id(boxed).Dot("Kind").Call().Op("!=").Add(expected)).Block(reject(label)),
				)
			}
			if kind == "Ref" && source.typ != nil {
				name := source.typ.Name()
				if name == "" {
					return lowering{}, fmt.Errorf("unsupported constant guard %s", source.typ)
				}
				ref := fmt.Sprintf("c%d", number)
				result.compile = append(result.compile,
					jen.Id(ref).Op(":=").Id(boxed).Dot("Ref").Call(),
					jen.If(jen.Id(ref).Op("<").Lit(0).Op("||").Id(ref).Op(">=").Len(jen.Id("c").Dot("heap"))).Block(reject(label)),
				)
				guard := jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id(ref)).Assert(jen.Qual("github.com/siyul-park/minivm/types", name))
				if source.not {
					result.compile = append(result.compile, jen.If(guard, jen.Id("ok")).Block(reject(label)))
				} else {
					result.compile = append(result.compile, guard, jen.If(jen.Op("!").Id("ok")).Block(reject(label)))
				}
			}
		}
	case instr.I32_CONST, instr.I64_CONST, instr.F32_CONST, instr.F64_CONST:
		kind, ok := kindName(source.kind)
		if !ok {
			return lowering{}, fmt.Errorf("unsupported source kind %s", source.kind)
		}
		if source.boxed {
			result.boxed = jen.Id(boxed)
			result.compile = append(result.compile,
				jen.Id(boxed).Op(":=").Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(immediate(kind, at)),
			)
			break
		}
		result.raw = jen.Id(name)
		result.compile = append(result.compile, jen.Id(name).Op(":=").Add(immediate(kind, at)))
		switch op {
		case instr.I32_CONST:
			result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(jen.Id(name))
		case instr.I64_CONST:
			result.boxed = jen.Id("i").Dot("boxI64").Call(jen.Id(name))
		case instr.F32_CONST:
			result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(jen.Id(name))
		case instr.F64_CONST:
			result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(jen.Id(name))
		}
	default:
		return lowering{}, fmt.Errorf("unsupported source opcode %s", instr.TypeOf(op).Mnemonic)
	}

	if source.kind != instr.KindAny && !source.boxed && !source.commit && result.raw == nil {
		kind, _ := kindName(source.kind)
		result.raw = jen.Id(name)
		result.body = append(result.body, jen.Id(name).Op(":=").Add(borrow(kind, result.boxed)))
	}

	result.push = append(result.push, result.check...)
	result.push = append(result.push, result.body...)
	if op == instr.CONST_GET && source.kind == instr.KindAny {
		result.push = append(result.push,
			jen.If(jen.Id(boxed).Dot("Kind").Call().Op("==").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(
				jen.Id("addr").Op(":=").Id(boxed).Dot("Ref").Call(),
				jen.If(jen.List(jen.Id("str"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Qual("github.com/siyul-park/minivm/types", "String")), jen.Id("ok")).Block(
					jen.Id(boxed).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxRef").Call(jen.Int().Call(jen.Id("i").Dot("intern").Call(jen.String().Call(jen.Id("str"))))),
				).Else().Block(jen.Id("i").Dot("retain").Call(jen.Id("addr"))),
			),
		)
	} else if op == instr.LOCAL_GET || op == instr.GLOBAL_GET || op == instr.UPVAL_GET || op == instr.CONST_GET {
		result.push = append(result.push, jen.Id("i").Dot("retainBox").Call(result.boxed))
	}
	result.push = append(result.push,
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(result.boxed),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(width),
	)
	return result, nil
}

func slot(number int) string {
	return fmt.Sprintf("v%d", number)
}

func box(source string) string {
	return "r" + strings.TrimPrefix(source, "v")
}

func check(field, idx string, expected jen.Code, label string) jen.Code {
	return jen.If(jen.Id(idx).Op(">=").Len(jen.Id("c").Dot(field)).Op("||").Id("c").Dot(field).Index(jen.Id(idx)).Dot("Repr").Call().Op("!=").Add(expected)).Block(reject(label))
}

func reject(label string) jen.Code {
	if label != "" {
		return jen.Goto().Id(label)
	}
	return jen.Return(jen.Nil())
}

func number(op instr.Opcode) (string, bool) {
	prefix, _, _ := strings.Cut(instr.TypeOf(op).Mnemonic, ".")
	switch prefix {
	case "i32":
		return "I32", true
	case "i64":
		return "I64", true
	case "f32":
		return "F32", true
	case "f64":
		return "F64", true
	}
	return "", false
}

func borrow(kind string, value jen.Code) jen.Code {
	if kind == "I64" {
		return jen.Id("i").Dot("borrowI64").Call(value)
	}
	return jen.Add(value).Dot(kind).Call()
}

func peek(kind string, index jen.Code) jen.Code {
	return borrow(kind, jen.Id("i").Dot("stack").Index(index))
}

func take(kind string, index jen.Code) jen.Code {
	value := jen.Id("i").Dot("stack").Index(index)
	if kind == "I64" {
		return jen.Id("i").Dot("unboxI64").Call(value)
	}
	return value.Dot(kind).Call()
}

func immediate(kind string, at jen.Code) jen.Code {
	operand := jen.Qual("github.com/siyul-park/minivm/instr", "Instruction").Call(jen.Id("c").Dot("code").Index(jen.Add(at).Op(":"))).Dot("Operand").Call(jen.Lit(0))
	switch kind {
	case "I32":
		return jen.Int32().Call(operand)
	case "I64":
		return jen.Int64().Call(operand)
	case "F32":
		return jen.Qual("github.com/siyul-park/minivm/types", "Box").Call(jen.Uint64().Call(jen.Uint32().Call(operand)), jen.Qual("github.com/siyul-park/minivm/types", "KindF32")).Dot("F32").Call()
	default:
		return jen.Qual("github.com/siyul-park/minivm/types", "Boxed").Call(operand).Dot("F64").Call()
	}
}

func width(op instr.Opcode) int {
	width := 1
	for _, operand := range instr.TypeOf(op).Widths {
		width += operand
	}
	return width
}

func add(value jen.Code, offset int) *jen.Statement {
	if offset == 0 {
		return jen.Add(value)
	}
	return jen.Add(value).Op("+").Lit(offset)
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
				jen.List(jen.Id("val")).Op(":=").List(jen.Op("&").Add(jen.Id("types").Dot("Array").Values(jen.Dict{jen.Id("Typ"): jen.Id("typ"), jen.Id("Elems"): jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Id("size"))}))),
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
				jen.List(jen.Id("val")).Op(":=").List(jen.Op("&").Add(jen.Id("types").Dot("Array").Values(jen.Dict{jen.Id("Typ"): jen.Id("typ"), jen.Id("Elems"): jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Id("size"))}))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))))
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
					jen.List(jen.Id("out")).Op("=").List(jen.Op("&").Add(jen.Id("types").Dot("Array").Values(jen.Dict{jen.Id("Typ"): jen.Id("arr").Dot("Typ"), jen.Id("Elems"): jen.Id("elems")})))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("newAddr")).Op(":=").List(jen.Id("i").Dot("alloc").Call(jen.Id("out"))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(2)),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("newAddr"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func br() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("instr").Dot("ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1)))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.List(jen.Id("f")).Op(":=").List(jen.Id("i").Dot("fr")),
			jen.List(jen.Id("f").Dot("ip")).Op("+=").List(jen.Id("offset").Op("+").Add(jen.Lit(3))))))
}

func brIf() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("instr").Dot("ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1)))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("cond")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Dot("I32").Call()),
			jen.If(jen.Id("cond").Op("!=").Add(jen.Lit(0))).Block(jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Id("offset"))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
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
			jen.Switch(jen.List(jen.Id("co")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("ref").Dot("Ref").Call()).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("Coroutine"))).Block(jen.If(jen.Id("co").Dot("done")).Block(jen.List(jen.Id("done")).Op("=").List(jen.Lit(1)))),
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
			jen.Switch(jen.List(jen.Id("co")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("box").Dot("Ref").Call()).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("Coroutine"))).Block(jen.List(jen.Id("val")).Op("=").List(jen.Id("co").Dot("value")),
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
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("keep").Call(jen.Id("types").Dot("NewError").Call(jen.Id("types").Dot("ErrorCode").Call(jen.Id("code").Dot("I32").Call()), jen.Id("i").Dot("message").Call(jen.Id("payload")), jen.Id("payload")))),
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
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("keep").Call(jen.Id("iter")))),
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
			jen.List(jen.Id("arr")).Op(":=").List(jen.Op("&").Add(jen.Id("types").Dot("Array").Values(jen.Dict{jen.Id("Typ"): jen.Id("types").Dot("NewArrayType").Call(jen.Id("keyType")), jen.Id("Elems"): jen.Id("elems")}))),
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
			jen.If(jen.Id("typ").Dot("TraceKeys").Op("||").Add(jen.Id("typ").Dot("TraceValues"))).Block(jen.List(jen.Id("addr")).Op("=").List(jen.Id("i").Dot("keep").Call(jen.Id("m")))).Else().Block(jen.List(jen.Id("addr")).Op("=").List(jen.Id("i").Dot("alloc").Call(jen.Id("m")))),
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
				jen.Switch(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(jen.Case(jen.Id("types").Dot("I32"), jen.Id("types").Dot("I64"), jen.Id("types").Dot("F32"), jen.Id("types").Dot("F64")).Block(),
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
			jen.Switch(jen.List(jen.Id("co")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("coAddr")).Assert(jen.Type()))).Block(jen.Case(jen.Op("*").Add(jen.Id("Coroutine"))).Block(jen.Block(jen.List(jen.Id("coAddr")).Op(":=").List(jen.Id("coAddr")),
				jen.List(jen.Id("co")).Op(":=").List(jen.Id("co")),
				jen.List(jen.Id("in")).Op(":=").List(jen.Id("in")),
				jen.If(jen.Id("co").Dot("done")).Block(jen.Id("panic").Call(jen.Id("ErrCoroutineDone"))),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(2)),
				jen.List(jen.Id("base")).Op(":=").List(jen.Id("i").Dot("sp")),
				jen.If(jen.Id("base").Op("+").Add(jen.Id("len").Call(jen.Id("co").Dot("image"))).Op("+").Add(jen.Lit(1)).Op(">").Add(jen.Id("len").Call(jen.Id("i").Dot("stack")))).Block(jen.Id("panic").Call(jen.Id("ErrStackOverflow"))),
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
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("fp").Op("==").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrFrameUnderflow"))),
			jen.If(jen.Id("i").Dot("fr").Dot("coro").Op("!=").Add(jen.Lit(0))).Block(jen.Block(jen.List(jen.Id("f")).Op(":=").List(jen.Id("i").Dot("fr")),
				jen.List(jen.Id("coAddr")).Op(":=").List(jen.Id("f").Dot("coro")),
				jen.List(jen.Id("co"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("coAddr")).Assert(jen.Op("*").Add(jen.Id("Coroutine")))),
				jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("f").Dot("returns"))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.If(jen.Id("f").Dot("returns").Op(">").Add(jen.Lit(0))).Block(jen.List(jen.Id("co").Dot("value")).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))).Else().Block(jen.List(jen.Id("co").Dot("value")).Op("=").List(jen.Id("types").Dot("BoxedNull"))),
				jen.List(jen.Id("co").Dot("done")).Op("=").List(jen.Id("true")),
				jen.List(jen.Id("co").Dot("image")).Op("=").List(jen.Id("co").Dot("image").Index(jen.Empty().Op(":").Add(jen.Lit(0)))),
				jen.List(jen.Id("co").Dot("upvals")).Op("=").List(jen.Id("nil")),
				jen.If(jen.Id("f").Dot("release")).Block(jen.Id("i").Dot("release").Call(jen.Id("f").Dot("ref"))),
				jen.List(jen.Id("co").Dot("ref")).Op("=").List(jen.Lit(0)),
				jen.List(jen.Id("co").Dot("release")).Op("=").List(jen.Id("false")),
				jen.List(jen.Id("bp")).Op(":=").List(jen.Id("f").Dot("bp")),
				jen.List(jen.Id("f").Dot("code")).Op("=").List(jen.Id("nil")),
				jen.List(jen.Id("f").Dot("upvals")).Op("=").List(jen.Id("nil")),
				jen.List(jen.Id("f").Dot("coro")).Op("=").List(jen.Lit(0)),
				jen.Id("i").Dot("fp").Op("--"),
				jen.List(jen.Id("i").Dot("fr")).Op("=").List(jen.Op("&").Add(jen.Id("i").Dot("frames").Index(jen.Id("i").Dot("fp").Op("-").Add(jen.Lit(1))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("bp"))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("coAddr"))),
				jen.List(jen.Id("i").Dot("sp")).Op("=").List(jen.Id("bp").Op("+").Add(jen.Lit(1)))),
				jen.Return()),
			jen.Block(jen.List(jen.Id("f")).Op(":=").List(jen.Id("i").Dot("fr")),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("f").Dot("returns"))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.Switch(jen.Id("f").Dot("returns")).Block(jen.Case(jen.Lit(0)).Block(),
					jen.Case(jen.Lit(1)).Block(jen.List(jen.Id("i").Dot("stack").Index(jen.Id("f").Dot("bp"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
					jen.Default().Block(jen.Id("copy").Call(jen.Id("i").Dot("stack").Index(jen.Id("f").Dot("bp").Op(":").Add(jen.Id("f").Dot("bp").Op("+").Add(jen.Id("f").Dot("returns")))), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("f").Dot("returns")).Op(":").Add(jen.Id("i").Dot("sp")))))),
				jen.List(jen.Id("i").Dot("sp")).Op("=").List(jen.Id("f").Dot("bp").Op("+").Add(jen.Id("f").Dot("returns"))),
				jen.If(jen.Id("f").Dot("release")).Block(jen.Id("i").Dot("release").Call(jen.Id("f").Dot("ref"))),
				jen.List(jen.Id("f").Dot("code")).Op("=").List(jen.Id("nil")),
				jen.Id("i").Dot("fp").Op("--"),
				jen.List(jen.Id("i").Dot("fr")).Op("=").List(jen.Op("&").Add(jen.Id("i").Dot("frames").Index(jen.Id("i").Dot("fp").Op("-").Add(jen.Lit(1)))))))))
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
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("keep").Call(jen.Id("iter")))),
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
				jen.Case(jen.Op("*").Add(jen.Id("HostObject"))).Block(jen.List(jen.Id("typ")).Op(":=").List(jen.Id("s").Dot("Typ")),
					jen.If(jen.Id("idx").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("typ").Dot("Fields"))))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
					jen.List(jen.Id("field")).Op(":=").List(jen.Id("typ").Dot("Fields").Index(jen.Id("idx"))),
					jen.Switch(jen.Id("field").Dot("Kind")).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindI8"), jen.Id("types").Dot("KindI1"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Id("s").Dot("SetField").Call(jen.Id("idx"), jen.Id("val"))),
						jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.Id("s").Dot("SetRaw").Call(jen.Id("idx"), jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("val"))))),
						jen.Case(jen.Id("types").Dot("KindRef")).Block(jen.List(jen.Id("old")).Op(":=").List(jen.Id("types").Dot("Boxed").Call(jen.Id("s").Dot("Raw").Call(jen.Id("idx")))),
							jen.Id("i").Dot("releaseBox").Call(jen.Id("old")),
							jen.Id("s").Dot("SetRaw").Call(jen.Id("idx"), jen.Id("uint64").Call(jen.Id("val")))),
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
				jen.List(jen.Id("co"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("coAddr")).Assert(jen.Op("*").Add(jen.Id("Coroutine")))),
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
