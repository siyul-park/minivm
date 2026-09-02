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
		return value{op: current.op, head: current.op, handler: standalone(current.op, compile, body)}, nil
	}
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(input.head)),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	return value{op: current.op, head: input.head, compile: compile}, nil
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
