package codegen

import (
	"fmt"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

// structGet specializes a constant field from the declared StructType.
// Runtime checks still validate ref kind, concrete type, bounds, and field kind;
// the declaration proves specialization, not runtime representation.
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
			reference(container.boxed),
		)

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
			jen.If(jen.Id("value").Dot("Typ").Dot("Fields").Index(jen.Id("at")).Dot("Kind").Op("!=").Qual(typesPkg, "Kind"+name)).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("result").Op(":=").Add(boxField(kind, jen.Id("value").Dot("Data").Index(jen.Id("at")))),
		}
		if kind == instr.KindRef {
			structBody = append(structBody, jen.Id("i").Dot("retainBox").Call(jen.Id("result")))
		}
		structBody = append(structBody, tail(jen.Id("result"))...)

		body = append(body, jen.If(
			jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.raw).Assert(jen.Op("*").Qual(typesPkg, "Struct")),
			jen.Id("ok"),
		).Block(structBody...))
		body = append(body, tail(jen.Id("i").Dot("structGet").Call(container.raw, jen.Id("at")))...)
		cases = append(cases, jen.Case(jen.Qual(typesPkg, "Kind"+name)).Block(
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(closure(body...)),
		))
	}
	cases = append(cases, jen.Default().Block(reject(state.label)))

	compile = append(compile,
		jen.Switch(jen.Add(container.declared).Dot("Fields").Index(jen.Id("at")).Dot("Kind")).Block(cases...),
	)
	state.stack = nil
	return value{op: current.op, head: container.head, compile: compile}, nil
}

// boxField returns the boxed expression for a struct field's raw
// 64-bit data slot once kind is known, mirroring (*types.Struct).Field's
// per-kind decoding without its runtime switch over the field's Kind.
func boxField(kind instr.Kind, data jen.Code) jen.Code {
	switch kind {
	case instr.KindI1:
		return jen.Qual(typesPkg, "BoxI1").Call(jen.Add(data).Op("!=").Lit(0))
	case instr.KindI8:
		return jen.Qual(typesPkg, "BoxI8").Call(jen.Int8().Call(jen.Uint32().Call(data)))
	case instr.KindI32:
		return jen.Qual(typesPkg, "BoxI32").Call(jen.Int32().Call(jen.Uint32().Call(data)))
	case instr.KindI64:
		return jen.Id("i").Dot("boxI64").Call(jen.Int64().Call(data))
	case instr.KindF32:
		return jen.Qual(typesPkg, "BoxF32").Call(jen.Qual("math", "Float32frombits").Call(jen.Uint32().Call(data)))
	case instr.KindF64:
		return jen.Qual(typesPkg, "BoxF64").Call(jen.Qual("math", "Float64frombits").Call(data))
	case instr.KindRef:
		return jen.Qual(typesPkg, "Boxed").Call(data)
	default:
		panic(fmt.Sprintf("unsupported struct field kind %s", kind))
	}
}

func structNew() jen.Code {
	return assertType("StructType",
		jen.Id("size").Op(":=").Id("len").Call(jen.Id("typ").Dot("Fields")),
		jen.Return(closure(jen.If(jen.Id("i").Dot("sp").Op("<").Id("size")).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.Id("s").Op(":=").Id("i").Dot("newStruct").Call(jen.Id("typ")),
			jen.For(jen.List(jen.Id("j"), jen.Id("f")).Op(":=").Range().Id("typ").Dot("Fields")).Block(jen.Id("val").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("size").Op("+").Id("j")),
				jen.Switch(jen.Id("f").Dot("Kind")).Block(jen.Case(jen.Qual(typesPkg, "KindI32"), jen.Qual(typesPkg, "KindI8"), jen.Qual(typesPkg, "KindI1"), jen.Qual(typesPkg, "KindF32"), jen.Qual(typesPkg, "KindF64"), jen.Qual(typesPkg, "KindRef")).Block(jen.Id("s").Dot("SetField").Call(jen.Id("j"), jen.Id("val"))),
					jen.Case(jen.Qual(typesPkg, "KindI64")).Block(jen.Id("s").Dot("SetRaw").Call(jen.Id("j"), jen.Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("val"))))),
					jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))))),
			jen.Id("i").Dot("sp").Op("-=").Id("size").Op("-").Lit(1),
			top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("s"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3))))
}

func structNewDefault() jen.Code {
	return assertType("StructType",
		jen.Return(closure(jen.If(jen.Id("i").Dot("sp").Op("==").Id("len").Call(jen.Id("i").Dot("stack"))).Block(jen.Panic(jen.Id("ErrStackOverflow"))),
			jen.Id("s").Op(":=").Id("i").Dot("newStruct").Call(jen.Id("typ")),
			jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("s"))),
			jen.Id("i").Dot("sp").Op("++"),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3))))
}

func structSet() jen.Code {
	return handler(underflow(3),
		jen.Id("val").Op(":=").Add(top(1)),
		jen.Id("idx").Op(":=").Id("int").Call(top(2).Dot("I32").Call()),
		container(3),
		jen.Switch(jen.Id("s").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type())).Block(jen.Case(jen.Op("*").Qual(typesPkg, "Struct")).Block(jen.Id("typ").Op(":=").Id("s").Dot("Typ"),
			jen.If(jen.Id("idx").Op("<").Lit(0).Op("||").Id("idx").Op(">=").Id("len").Call(jen.Id("typ").Dot("Fields"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
			jen.Id("field").Op(":=").Id("typ").Dot("Fields").Index(jen.Id("idx")),
			jen.Switch(jen.Id("field").Dot("Kind")).Block(jen.Case(jen.Qual(typesPkg, "KindI32")).Block(jen.Id("s").Dot("Data").Index(jen.Id("idx")).Op("=").Id("uint64").Call(jen.Id("uint32").Call(jen.Id("val").Dot("I32").Call()))),
				jen.Case(jen.Qual(typesPkg, "KindI8")).Block(jen.Id("s").Dot("Data").Index(jen.Id("idx")).Op("=").Id("uint64").Call(jen.Id("uint32").Call(jen.Id("int32").Call(jen.Id("val").Dot("I8").Call())))),
				jen.Case(jen.Qual(typesPkg, "KindI1")).Block(jen.If(jen.Id("val").Dot("Bool").Call()).Block(jen.Id("s").Dot("Data").Index(jen.Id("idx")).Op("=").Lit(1)).Else().Block(jen.Id("s").Dot("Data").Index(jen.Id("idx")).Op("=").Lit(0))),
				jen.Case(jen.Qual(typesPkg, "KindI64")).Block(jen.Id("s").Dot("Data").Index(jen.Id("idx")).Op("=").Id("uint64").Call(jen.Id("i").Dot("unboxI64").Call(jen.Id("val")))),
				jen.Case(jen.Qual(typesPkg, "KindF32")).Block(jen.Id("s").Dot("Data").Index(jen.Id("idx")).Op("=").Id("uint64").Call(jen.Qual("math", "Float32bits").Call(jen.Id("val").Dot("F32").Call()))),
				jen.Case(jen.Qual(typesPkg, "KindF64")).Block(jen.Id("s").Dot("Data").Index(jen.Id("idx")).Op("=").Qual("math", "Float64bits").Call(jen.Id("val").Dot("F64").Call())),
				jen.Case(jen.Qual(typesPkg, "KindRef")).Block(jen.Id("old").Op(":=").Qual(typesPkg, "Boxed").Call(jen.Id("s").Dot("Data").Index(jen.Id("idx"))),
					jen.Id("i").Dot("releaseBox").Call(jen.Id("old")),
					jen.Id("s").Dot("Data").Index(jen.Id("idx")).Op("=").Id("uint64").Call(jen.Id("val"))),
				jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))))),
			jen.Case(jen.Op("*").Id("HostStruct")).Block(check(jen.Id("s").Dot("SetField").Call(jen.Id("i"), jen.Id("idx"), jen.Id("val")))),
			jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch")))),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("-=").Lit(3),
		next())
}
