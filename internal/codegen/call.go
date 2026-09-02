package codegen

import (
	"fmt"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

func call(state *state, current step) (value, error) {
	if state.standalone {
		body, err := dynamicCall(current.op)
		if err != nil {
			return value{}, err
		}
		return value{op: current.op, head: current.op, handler: standalone(current.op, nil, body)}, nil
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
		body = append(body, allocClosure(1, false, 1)...)
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
		functionBody = append(functionBody, replaceFrame(
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
		closureBody = append(closureBody, replaceFrame(
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
		function = append(function, pushFrame(functionTarget, 1, true, 1, jen.Id("addr"))...)
		closure = []jen.Code{
			frameOverflow(),
			jen.List(jen.Id("tmpl"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("fn").Dot("Fn")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("params").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Params")),
			jen.Id("returns").Op(":=").Len(jen.Id("fn").Dot("Typ").Dot("Returns")),
			jen.Id("locals").Op(":=").Len(jen.Id("tmpl").Dot("Locals")),
		}
		closure = append(closure, pushFrame(closureTarget, 1, true, 1, jen.Int().Parens(jen.Id("fn").Dot("Fn")))...)
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

func dispatch(tail bool, label string, advance int) jen.Code {
	return jen.Switch(jen.Id("fn").Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")).Block(callDirect(tail, label, advance)...),
		jen.Case(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Closure")).Block(callClosure(tail, label, advance)...),
		jen.Case(jen.Op("*").Id("HostFunction")).Block(callHost(tail, advance)...),
		jen.Default().Block(reject(label)),
	)
}

func callDirect(tail bool, label string, advance int) []jen.Code {
	guard := jen.If(jen.Id("addr").Op("<").Len(jen.Id("c").Dot("coros")).Op("&&").Id("c").Dot("coros").Index(jen.Id("addr"))).Block(reject(label))
	callee := target{code: jen.Id("addr"), addr: jen.Id("addr"), upvals: jen.Nil(), ref: jen.Id("addr")}
	if tail {
		return append([]jen.Code{guard}, reuseFrame(callee, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("fn").Dot("Locals")), advance)...)
	}
	return append([]jen.Code{guard}, enterFrame(callee, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("fn").Dot("Locals")), advance)...)
}

func callClosure(tail bool, label string, advance int) []jen.Code {
	preflight := []jen.Code{
		jen.Id("tmpl").Op(",").Id("ok").Op(":=").Id("c").Dot("heap").Index(jen.Id("fn").Dot("Fn")).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "Function")),
		jen.If(jen.Op("!").Id("ok")).Block(reject(label)),
		jen.If(jen.Int().Parens(jen.Id("fn").Dot("Fn")).Op("<").Len(jen.Id("c").Dot("coros")).Op("&&").Id("c").Dot("coros").Index(jen.Id("fn").Dot("Fn"))).Block(reject(label)),
	}
	callee := target{code: jen.Id("fn").Dot("Fn"), addr: jen.Int().Parens(jen.Id("fn").Dot("Fn")), upvals: jen.Id("fn").Dot("Upvals"), ref: jen.Id("addr")}
	if tail {
		return append(preflight, reuseFrame(callee, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("tmpl").Dot("Locals")), advance)...)
	}
	return append(preflight, enterFrame(callee, jen.Id("fn").Dot("Typ"), jen.Len(jen.Id("tmpl").Dot("Locals")), advance)...)
}

func enterFrame(callee target, typ, locals jen.Code, advance int) []jen.Code {
	return []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Add(typ).Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Add(typ).Dot("Returns")),
		jen.Id("locals").Op(":=").Add(locals),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(pushFrame(callee, 0, false, advance, nil)...)),
	}
}

func pushFrame(callee target, targetSlots int, releaseTarget bool, advance int, coroutine jen.Code) []jen.Code {
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
		frameEntered(),
	)
	return body
}

// frameEntered emits the callee-entry hook after the new frame becomes current.
func frameEntered() jen.Code {
	return jen.If(jen.Id("i").Dot("trigger").Op("!=").Lit(0)).Block(
		jen.Id("c").Dot("entry").Call(jen.Id("i")),
	)
}

func reuseFrame(callee target, typ, locals jen.Code, advance int) []jen.Code {
	return []jen.Code{
		jen.Id("params").Op(":=").Len(jen.Add(typ).Dot("Params")),
		jen.Id("returns").Op(":=").Len(jen.Add(typ).Dot("Returns")),
		jen.Id("locals").Op(":=").Add(locals),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(replaceFrame(callee, 0, false, advance, "")...)),
	}
}

func replaceFrame(callee target, targetSlots int, releaseTarget bool, advance int, label string) []jen.Code {
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
			jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("i").Dot("stack").Index(
				jen.Id("f").Dot("bp").Op(":").Id("i").Dot("sp").Op("-").Id("params").Op("-").Lit(1),
			)).Block(
				jen.If(jen.Id("value").Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Continue()),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("value")),
			),
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
			frameEntered(),
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
			frameEntered(),
			jen.Return(),
		),
		jen.Id("f").Op(":=").Id("i").Dot("fr"),
		jen.Id("base").Op(":=").Id("f").Dot("bp"),
		jen.If(jen.Id("base").Op("+").Id("params").Op("+").Id("locals").Op(">").Len(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
		jen.For(jen.List(jen.Id("_"), jen.Id("value")).Op(":=").Range().Id("i").Dot("stack").Index(
			jen.Id("f").Dot("bp").Op(":").Id("i").Dot("sp").Op("-").Id("params"),
		)).Block(
			jen.If(jen.Id("value").Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Continue()),
			jen.Id("i").Dot("releaseBox").Call(jen.Id("value")),
		),
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
		frameEntered(),
	)
	return body
}

func frameOverflow() jen.Code {
	return jen.If(jen.Id("i").Dot("fp").Op("==").Len(jen.Id("i").Dot("frames"))).Block(jen.Panic(jen.Id("ErrFrameOverflow")))
}

func callHost(tail bool, advance int) []jen.Code {
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
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(allocClosure(0, true, advance)...)),
		),
		jen.Default().Block(reject(label)),
	)
}

func allocClosure(targetSlots int, borrowed bool, advance int) []jen.Code {
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
