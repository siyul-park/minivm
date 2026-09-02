package codegen

import (
	"fmt"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

func branch(state *state, current step) (value, error) {
	if state.standalone {
		compile := []jen.Code{
			jen.Id("offset").Op(":=").Qual("github.com/siyul-park/minivm/instr", "ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("c").Dot("ip").Op("+").Lit(1)),
		}
		body := []jen.Code{
			jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		}
		condition := jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Dot("I32").Call().Op("!=").Lit(0)
		compile = append(compile, jen.Id("c").Dot("ip").Op("+=").Lit(width(current.op)))
		compile = append(compile, branchTail(condition, 1, width(current.op), body)...)
		return value{op: current.op, head: current.op, handler: threaderFunc(compile...)}, nil
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
	compile = append(compile, jen.Id("c").Dot("ip").Op("+=").Lit(width(consumer.head)))
	compile = append(compile, branchTail(condition, 0, state.width, body)...)
	state.stack = nil
	return value{op: current.op, head: consumer.head, compile: compile}, nil
}

func br() jen.Code {
	apply := []jen.Code{
		jen.Id("f").Op(":=").Id("i").Dot("fr"),
		jen.Id("f").Dot("ip").Op("+=").Id("offset").Op("+").Lit(3),
	}
	observe := append([]jen.Code(nil), apply...)
	observe = append(observe, backedge()...)
	return threaderFunc(
		jen.Id("offset").Op(":=").Id("instr").Dot("ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("c").Dot("ip").Op("+").Lit(1)),
		jen.Id("c").Dot("ip").Op("+=").Lit(3),
		jen.If(jen.Id("c").Dot("backedge").Op("==").Nil().Op("||").Id("offset").Op(">").Lit(-3)).Block(
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(apply...)),
		),
		jen.Id("hits").Op(":=").Lit(0),
		jen.Id("skew").Op(":=").Lit(0),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(observe...)),
	)
}

// backedge emits the tail shared by every backward-branch handler: bump this site's
// own iteration counter and report the header to the interpreter every
// loopWarmup iterations. The counter is closure state belonging to one branch
// site, so an iteration costs one increment and one compare with no lookup, and
// every header is reported on its own iteration backedge instead of on whichever
// instruction a sampling tick happened to stop at. The interval is fixed rather
// than derived from the configured threshold, because each report is one hot
// event and the threshold counts those.
//
// Each report restarts the backedge at a rotating skew instead of at zero. A fixed
// interval reports at the same iteration of every trip, so a loop whose trip
// backedge divides the interval - any short power-of-two loop - is only ever
// observed on the iteration that exits it, and the tracer then records the path
// out of the loop no matter how many times it retries. Rotating the phase costs
// nothing per iteration, because the per-iteration test is still against the
// constant.
//
// Emit it only after f already points at the branching frame with its new ip
// applied, because backedge reads f.ip as the header.
func backedge() []jen.Code {
	return []jen.Code{
		jen.Id("hits").Op("++"),
		jen.If(jen.Id("hits").Op("<").Id("loopWarmup")).Block(jen.Return()),
		jen.Id("hits").Op("=").Id("skew"),
		jen.Id("skew").Op("=").Parens(jen.Id("skew").Op("+").Lit(1)).Op("%").Id("loopWarmup"),
		jen.If(jen.Id("err").Op(":=").Id("c").Dot("backedge").Call(jen.Id("i"), jen.Id("f")), jen.Id("err").Op("!=").Nil()).Block(
			jen.Panic(jen.Id("err")),
		),
	}
}

// brTable threads a jump table. Every case offset is known here, so which cases
// are loop back edges is settled at threading time into one bool per case, and
// the taken arm decides by indexing it rather than by recomputing a direction.
// The counters are per case, so two loops sharing one table warm separately.
func brTable() jen.Code {
	body := []jen.Code{
		jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("i").Dot("sp").Op("--"),
		jen.Id("cond").Op(":=").Int().Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Dot("I32").Call()),
		jen.If(jen.Id("cond").Op("<").Lit(0).Op("||").Id("cond").Op(">=").Id("count")).Block(jen.Id("cond").Op("=").Id("count")),
		jen.Id("f").Op(":=").Id("i").Dot("fr"),
		jen.Id("f").Dot("ip").Op("+=").Id("offsets").Index(jen.Id("cond")).Op("+").Id("advance"),
		jen.If(jen.Op("!").Id("back").Index(jen.Id("cond"))).Block(jen.Return()),
		jen.Id("hits").Index(jen.Id("cond")).Op("++"),
		jen.If(jen.Id("hits").Index(jen.Id("cond")).Op("<").Id("loopWarmup")).Block(jen.Return()),
		jen.Id("hits").Index(jen.Id("cond")).Op("=").Id("skew").Index(jen.Id("cond")),
		jen.Id("skew").Index(jen.Id("cond")).Op("=").Parens(jen.Id("skew").Index(jen.Id("cond")).Op("+").Lit(1)).Op("%").Id("loopWarmup"),
		jen.If(jen.Id("err").Op(":=").Id("c").Dot("backedge").Call(jen.Id("i"), jen.Id("f")), jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
	}
	return jen.Func().Params(jen.Id("c").Op("*").Id("threader")).Params(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter"))).Block(
		jen.Id("count").Op(":=").Int().Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Lit(1))),
		jen.Id("advance").Op(":=").Id("count").Op("*").Lit(2).Op("+").Lit(4),
		jen.Id("offsets").Op(":=").Make(jen.Index().Int(), jen.Id("count").Op("+").Lit(1)),
		jen.Id("back").Op(":=").Make(jen.Index().Bool(), jen.Len(jen.Id("offsets"))),
		jen.Id("hits").Op(":=").Make(jen.Index().Int(), jen.Len(jen.Id("offsets"))),
		jen.Id("skew").Op(":=").Make(jen.Index().Int(), jen.Len(jen.Id("offsets"))),
		jen.For(jen.Id("i").Op(":=").Range().Id("offsets")).Block(
			jen.Id("offsets").Index(jen.Id("i")).Op("=").Id("instr").Dot("ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("c").Dot("ip").Op("+").Id("i").Op("*").Lit(2).Op("+").Lit(2)),
			jen.Id("back").Index(jen.Id("i")).Op("=").Id("c").Dot("backedge").Op("!=").Nil().Op("&&").Id("offsets").Index(jen.Id("i")).Op("+").Id("advance").Op("<=").Lit(0),
		),
		jen.Id("c").Dot("ip").Op("+=").Id("advance"),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
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

func nop() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("skip")).Op(":=").List(jen.Lit(0)),
		jen.For(jen.Op("!").Add(jen.Id("c").Dot("exact")).Op("&&").Add(jen.Id("c").Dot("ip").Op("+").Add(jen.Id("skip")).Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("code")))).Op("&&").Add(jen.Id("instr").Dot("Opcode").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Id("skip")))).Op("==").Add(jen.Id("instr").Dot("NOP")))).Block(jen.Id("skip").Op("++")),
		jen.If(jen.Id("c").Dot("exact")).Block(jen.List(jen.Id("skip")).Op("=").List(jen.Lit(1))),
		jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Id("skip")))))
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
