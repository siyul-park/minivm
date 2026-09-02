package codegen

import (
	"fmt"
	"slices"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

func arithmetic(state *state, current step) (value, error) {
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

func localStore(state *state, current step) (value, error) {
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
		return trapping(consumer, inputs, kind)
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
			operands = append(operands, unbox(kind, jen.Id("i").Dot("sp").Op("-").Lit(index)))
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

	first := consumer
	if len(inputs) > 0 {
		first = inputs[0].op
	}

	result := temp(len(inputs))
	body = append(body, jen.Id(result).Op(":=").Add(compute(consumer, operands...)))
	if conditional {
		compile = append(compile, jen.Id("c").Dot("ip").Op("+=").Lit(width(first)))
		return append(compile, branchTail(jen.Id(result).Dot("Bool").Call(), missing, advance, body)...), nil
	}
	if local != nil {
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

	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(first)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return compile, nil
}

func branchTail(condition jen.Code, consume, advance int, body []jen.Code) []jen.Code {
	// Choose the handler at threading time. Forward branches retain the plain
	// path; only backward branches carry a counter.
	code := append([]jen.Code(nil), body...)
	if consume > 0 {
		code = append(code, jen.Id("i").Dot("sp").Op("-=").Lit(consume))
	}
	taken := func(backedge bool) []jen.Code {
		path := []jen.Code{
			jen.Id("f").Op(":=").Id("i").Dot("fr"),
			jen.Id("f").Dot("ip").Op("+=").Id("offset").Op("+").Lit(advance),
		}
		if backedge {
			path = append(path,
				jen.Id("hits").Op("++"),
				jen.If(jen.Id("hits").Op("<").Id("loopWarmup")).Block(jen.Return()),
				jen.Id("hits").Op("=").Id("skew"),
				jen.Id("skew").Op("=").Parens(jen.Id("skew").Op("+").Lit(1)).Op("%").Id("loopWarmup"),
				jen.If(jen.Id("err").Op(":=").Id("c").Dot("backedge").Call(jen.Id("i"), jen.Id("f")), jen.Id("err").Op("!=").Nil()).Block(
					jen.Panic(jen.Id("err")),
				),
			)
		}
		return append(path, jen.Return())
	}
	plain := append([]jen.Code(nil), code...)
	plain = append(plain, jen.If(condition).Block(taken(false)...), jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance))
	back := append([]jen.Code(nil), code...)
	back = append(back, jen.If(condition).Block(taken(true)...), jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance))
	return []jen.Code{
		jen.If(jen.Id("c").Dot("backedge").Op("!=").Nil().Op("&&").Id("offset").Op("+").Lit(advance).Op("<=").Lit(0)).Block(
			jen.Id("hits").Op(":=").Lit(0),
			jen.Id("skew").Op(":=").Lit(0),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(back...)),
		),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(plain...)),
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

func trapping(consumer instr.Opcode, inputs []value, kind instr.Kind) ([]jen.Code, error) {
	var compile, body []jen.Code
	for _, source := range inputs {
		compile = append(compile, source.compile...)
		body = append(body, source.push...)
	}
	body = append(body,
		jen.If(jen.Id("i").Dot("sp").Op("<").Lit(2)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("rhs").Op(":=").Add(unbox(kind, jen.Id("i").Dot("sp").Op("-").Lit(1))),
		jen.Id("lhs").Op(":=").Add(unbox(kind, jen.Id("i").Dot("sp").Op("-").Lit(2))),
		jen.If(jen.Id("rhs").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrDivideByZero"))),
		jen.Id("result").Op(":=").Add(compute(consumer, jen.Id("lhs"), jen.Id("rhs"))),
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

func compute(op instr.Opcode, operands ...jen.Code) jen.Code {
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

func unbox(kind instr.Kind, index jen.Code) jen.Code {
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

func mustKindName(kind instr.Kind) string {
	name, ok := kindName(kind)
	if !ok {
		panic(fmt.Sprintf("unsupported kind %s", kind))
	}
	return name
}
