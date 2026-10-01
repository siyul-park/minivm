package codegen

import (
	"fmt"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

func refOp(state *state, current step) (value, error) {
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
		result.boxed = jen.Qual(typesPkg, "BoxedNull")
		result.check = append(result.check, overflow())
		result.push = append(result.push, result.check...)
		result.push = append(result.push,
			jen.Id("i").Dot("retain").Call(jen.Lit(0)),
			jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(result.boxed),
			jen.Id("i").Dot("sp").Op("++"),
			next(),
		)
	case instr.DUP:
		result.boxed = jen.Id("value")
		result.check = append(result.check,
			underflow(1),
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
			next(),
		)
	}
	if state.standalone {
		result.handler = standalone(current.op, result.compile, result.push)
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
				underflow(1),
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
				top(1).Op("=").Qual(typesPkg, "BoxI1").Call(condition),
			)
			body = append(body, input.drop...)
		} else {
			body = append(body,
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Qual(typesPkg, "BoxI1").Call(condition),
				jen.Id("i").Dot("sp").Op("++"),
			)
			body = append(body, input.drop...)
		}
		body = append(body, jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width))
	}

	state.stack = nil
	if state.standalone {
		return value{op: current.op, head: current.op, handler: standalone(current.op, compile, body)}, nil
	}
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(input.head)),
		jen.Return(closure(body...)),
	)
	return value{op: current.op, head: input.head, compile: compile}, nil
}

func refCast() jen.Code {
	return typeAt(
		jen.Id("typ").Op(":=").Id("c").Dot("types").Index(jen.Id("idx")),
		jen.Return(closure(underflow(1),
			jen.Id("val").Op(":=").Add(top(1)),
			jen.Switch(jen.Id("kind").Op(":=").Id("val").Dot("Kind").Call(), jen.Id("kind")).Block(jen.Case(jen.Qual(typesPkg, "KindRef")).Block(jen.Id("ref").Op(":=").Id("i").Dot("heap").Index(jen.Id("val").Dot("Ref").Call()),
				jen.If(jen.Op("!").Id("typ").Dot("Cast").Call(jen.Id("ref").Dot("Type").Call())).Block(jen.Panic(jen.Id("ErrTypeMismatch")))),
				jen.Default().Block(jen.If(jen.Op("!").Id("typ").Dot("Cast").Call(jen.Id("val").Dot("Type").Call())).Block(jen.Panic(jen.Id("ErrTypeMismatch"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3))))
}

// refCompare lowers the identity comparison op of two references, which it
// releases.
func refCompare(op string) jen.Code {
	return handler(underflow(2),
		jen.Id("v1").Op(":=").Add(top(1)),
		jen.Id("v2").Op(":=").Add(top(2)),
		jen.Id("i").Dot("sp").Op("--"),
		top(1).Op("=").Qual(typesPkg, "BoxI1").Call(jen.Id("v2").Op(op).Id("v1")),
		jen.Id("i").Dot("releaseBox").Call(jen.Id("v1")),
		jen.Id("i").Dot("releaseBox").Call(jen.Id("v2")),
		next())
}

func refGet() jen.Code {
	return handler(jen.Var().Id("val").Qual(typesPkg, "Boxed"),
		jen.Block(underflow(1),
			container(1),
			jen.Switch(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(jen.Case(
				jen.Qual(typesPkg, "I1"), jen.Qual(typesPkg, "I8"),
				jen.Qual(typesPkg, "I32"), jen.Qual(typesPkg, "I64"),
				jen.Qual(typesPkg, "F32"), jen.Qual(typesPkg, "F64"),
			).Block(),
				jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch")))),
			jen.Id("result").Op(":=").Id("i").Dot("box").Call(jen.Id("i").Dot("heap").Index(jen.Id("addr"))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.Id("val").Op("=").Id("result")),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("val"),
		jen.Id("i").Dot("sp").Op("++"),
		next())
}

func refNew() jen.Code {
	return handler(underflow(1),
		jen.Id("v").Op(":=").Add(top(1)),
		jen.If(jen.Id("v").Dot("Kind").Call().Op("==").Qual(typesPkg, "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Qual(typesPkg, "Unbox").Call(jen.Id("v")))),
		next())
}

func refSet() jen.Code {
	return handler(underflow(2),
		jen.Id("value").Op(":=").Add(top(1)),
		jen.Id("ref").Op(":=").Add(top(2)),
		jen.If(jen.Id("value").Dot("Kind").Call().Op("==").Qual(typesPkg, "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		reference(jen.Id("ref")),
		jen.Id("addr").Op(":=").Id("ref").Dot("Ref").Call(),
		jen.Switch(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
			jen.Case(
				jen.Qual(typesPkg, "I1"), jen.Qual(typesPkg, "I8"),
				jen.Qual(typesPkg, "I32"), jen.Qual(typesPkg, "I64"),
				jen.Qual(typesPkg, "F32"), jen.Qual(typesPkg, "F64"),
			),
			jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		),
		jen.Id("i").Dot("heap").Index(jen.Id("addr")).Op("=").Qual(typesPkg, "Unbox").Call(jen.Id("value")),
		jen.Id("i").Dot("sp").Op("-=").Lit(2),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		next())
}

func refTest() jen.Code {
	return typeAt(
		jen.Id("typ").Op(":=").Id("c").Dot("types").Index(jen.Id("idx")),
		jen.Return(closure(underflow(1),
			jen.Id("val").Op(":=").Add(top(1)),
			jen.Var().Id("cond").Qual(typesPkg, "Boxed"),
			jen.Switch(jen.Id("kind").Op(":=").Id("val").Dot("Kind").Call(), jen.Id("kind")).Block(jen.Case(jen.Qual(typesPkg, "KindRef")).Block(jen.Id("ref").Op(":=").Id("i").Dot("heap").Index(jen.Id("val").Dot("Ref").Call()),
				jen.Id("cond").Op("=").Qual(typesPkg, "BoxI1").Call(jen.Id("typ").Dot("Equals").Call(jen.Id("ref").Dot("Type").Call()))),
				jen.Default().Block(jen.Id("cond").Op("=").Qual(typesPkg, "BoxI1").Call(jen.Id("typ").Dot("Kind").Call().Op("==").Id("kind")))),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("val")),
			top(1).Op("=").Id("cond"),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3))))
}

func errorCode() jen.Code {
	return handler(underflow(1),
		jen.Id("box").Op(":=").Add(top(1)),
		reference(jen.Id("box")),
		jen.List(jen.Id("e"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("box").Dot("Ref").Call()).Assert(jen.Op("*").Qual(typesPkg, "Error")),
		jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("code").Op(":=").Id("e").Dot("Code").Call(),
		jen.Id("i").Dot("releaseBox").Call(jen.Id("box")),
		top(1).Op("=").Qual(typesPkg, "BoxI32").Call(jen.Id("int32").Call(jen.Id("code"))),
		next())
}

func errorGet() jen.Code {
	return handler(underflow(1),
		jen.Id("box").Op(":=").Add(top(1)),
		reference(jen.Id("box")),
		jen.List(jen.Id("e"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("box").Dot("Ref").Call()).Assert(jen.Op("*").Qual(typesPkg, "Error")),
		jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("val").Op(":=").Id("e").Dot("Value").Call(),
		jen.Id("i").Dot("retainBox").Call(jen.Id("val")),
		jen.Id("i").Dot("releaseBox").Call(jen.Id("box")),
		top(1).Op("=").Id("val"),
		next())
}

func errorNew() jen.Code {
	return handler(underflow(2),
		jen.Id("code").Op(":=").Add(top(1)),
		jen.If(jen.Id("code").Dot("Kind").Call().Op("!=").Qual(typesPkg, "KindI32")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("payload").Op(":=").Add(top(2)),
		jen.Id("addr").Op(":=").Id("i").Dot("alloc").Call(jen.Qual(typesPkg, "NewError").Call(jen.Qual(typesPkg, "ErrorCode").Call(jen.Id("code").Dot("I32").Call()), jen.Id("i").Dot("message").Call(jen.Id("payload")), jen.Id("payload"))),
		jen.Id("i").Dot("sp").Op("--"),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("addr")),
		next())
}
