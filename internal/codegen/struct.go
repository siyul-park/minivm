package codegen

import (
	"fmt"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

// structGet fuses STRUCT_GET onto a LOCAL_GET, GLOBAL_GET, or UPVAL_GET
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
func structGet(state *state, current step) (value, error) {
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
		// never the runtime value's concrete representation, so a miss on the
		// specialized *types.Struct assertion falls back to
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

func structNew() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("StructType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.List(jen.Id("size")).Op(":=").List(jen.Id("len").Call(jen.Id("typ").Dot("Fields"))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size"))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("s")).Op(":=").List(jen.Id("i").Dot("newStruct").Call(jen.Id("typ"))),
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
			jen.List(jen.Id("s")).Op(":=").List(jen.Id("i").Dot("newStruct").Call(jen.Id("typ"))),
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
				jen.Case(jen.Op("*").Add(jen.Id("HostStruct"))).Block(jen.If(jen.List(jen.Id("err")).Op(":=").List(jen.Id("s").Dot("SetField").Call(jen.Id("i"), jen.Id("idx"), jen.Id("val"))), jen.Id("err").Op("!=").Add(jen.Id("nil"))).Block(jen.Id("panic").Call(jen.Id("err")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(3)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}
