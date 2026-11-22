package codegen

import (
	"github.com/dave/jennifer/jen"
)

func mapNew() jen.Code {
	return assertType("MapType",
		jen.Return(closure(underflow(1),
			jen.Id("size").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
			jen.If(jen.Id("size").Op("<").Lit(0)).Block(jen.Panic(jen.Id("ErrIndexOutOfRange"))),
			jen.If(jen.Id("i").Dot("sp").Op("<").Id("size").Op("*").Lit(2).Op("+").Lit(1)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.Id("m").Op(":=").Qual(typesPkg, "NewMap").Call(jen.Op("&").Id("i").Dot("heap"), jen.Id("typ"), jen.Id("size")),
			jen.Id("base").Op(":=").Id("i").Dot("sp").Op("-").Lit(1).Op("-").Id("size").Op("*").Lit(2),
			jen.For(jen.Id("j").Op(":=").Lit(0), jen.Id("j").Op("<").Id("size"), jen.Id("j").Op("++")).Block(
				jen.If(jen.Op("!").Id("m").Dot("Accepts").Call(jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("j").Op("*").Lit(2)))).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			),
			// The map takes its place on the stack first, in the size slot, and each Store
			// overwrites the two slots it consumed, so a trap unwinds every reference once.
			jen.Id("addr").Op(":=").Id("i").Dot("heap").Dot("Add").Call(jen.Id("m")),
			top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("addr")),
			jen.For(jen.Id("j").Op(":=").Lit(0), jen.Id("j").Op("<").Id("size"), jen.Id("j").Op("++")).Block(
				jen.Id("m").Dot("Store").Call(
					jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("j").Op("*").Lit(2)),
					jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("j").Op("*").Lit(2).Op("+").Lit(1)),
				),
				jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("j").Op("*").Lit(2)).Op("=").Qual(typesPkg, "BoxI32").Call(jen.Lit(0)),
				jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("j").Op("*").Lit(2).Op("+").Lit(1)).Op("=").Qual(typesPkg, "BoxI32").Call(jen.Lit(0)),
			),
			jen.Id("i").Dot("sp").Op("=").Id("base").Op("+").Lit(1),
			jen.Id("i").Dot("stack").Index(jen.Id("base")).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("addr")),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3),
		)),
	)
}

func mapNewDefault() jen.Code {
	return assertType("MapType",
		jen.Return(closure(underflow(1),
			jen.Id("val").Op(":=").Id("i").Dot("newMapDefault").Call(jen.Id("typ"), top(1)),
			top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("heap").Dot("Add").Call(jen.Id("val"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3),
		)),
	)
}

func mapLen() jen.Code {
	return handler(underflow(1),
		container(1),
		jen.Id("n").Op(":=").Lit(0),
		mapHeap(
			mapCase(jen.Id("n").Op("=").Id("m").Dot("Len").Call()),
			hostMapCase(jen.Id("n").Op("=").Id("m").Dot("Len").Call()),
		),
		jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("addr")),
		top(1).Op("=").Qual(typesPkg, "BoxI32").Call(jen.Id("int32").Call(jen.Id("n"))),
		next(),
	)
}

func mapGet() jen.Code {
	return handler(underflow(2),
		jen.Id("key").Op(":=").Add(top(1)),
		container(2),
		jen.Var().Id("result").Qual(typesPkg, "Boxed"),
		mapHeap(
			mapCase(
				accept(),
				jen.Var().Id("ok").Id("bool"),
				jen.List(jen.Id("result"), jen.Id("ok")).Op("=").Id("m").Dot("Load").Call(jen.Id("key")),
				jen.If(jen.Op("!").Id("ok")).Block(jen.Id("result").Op("=").Id("m").Dot("Zero")),
				jen.Id("i").Dot("heap").Dot("Retain").Call(jen.Id("result")),
				jen.Id("i").Dot("heap").Dot("Release").Call(jen.Id("key")),
			),
			hostMapCase(
				jen.List(jen.Id("value"), jen.Id("_"), jen.Id("err")).Op(":=").Id("m").Dot("Get").Call(jen.Id("i"), jen.Id("key")),
				jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
				jen.Id("result").Op("=").Id("value"),
			),
		),
		jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("--"),
		top(1).Op("=").Id("result"),
		next(),
	)
}

func mapLookup() jen.Code {
	return handler(underflow(2),
		jen.Id("key").Op(":=").Add(top(1)),
		container(2),
		jen.Var().Id("result").Qual(typesPkg, "Boxed"),
		jen.Var().Id("found").Id("bool"),
		mapHeap(
			mapCase(
				accept(),
				jen.List(jen.Id("result"), jen.Id("found")).Op("=").Id("m").Dot("Load").Call(jen.Id("key")),
				jen.If(jen.Op("!").Id("found")).Block(jen.Id("result").Op("=").Id("m").Dot("Zero")),
				jen.Id("i").Dot("heap").Dot("Retain").Call(jen.Id("result")),
				jen.Id("i").Dot("heap").Dot("Release").Call(jen.Id("key")),
			),
			hostMapCase(
				jen.List(jen.Id("value"), jen.Id("present"), jen.Id("err")).Op(":=").Id("m").Dot("Get").Call(jen.Id("i"), jen.Id("key")),
				jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
				jen.List(jen.Id("result"), jen.Id("found")).Op("=").List(jen.Id("value"), jen.Id("present")),
			),
		),
		jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("addr")),
		top(2).Op("=").Id("result"),
		top(1).Op("=").Qual(typesPkg, "BoxI1").Call(jen.Id("found")),
		next(),
	)
}

func mapSet() jen.Code {
	return handler(underflow(3),
		jen.Id("value").Op(":=").Add(top(1)),
		jen.Id("key").Op(":=").Add(top(2)),
		container(3),
		mapHeap(
			mapCase(accept(), jen.Id("m").Dot("Store").Call(jen.Id("key"), jen.Id("value"))),
			hostMapCase(check(jen.Id("m").Dot("Set").Call(jen.Id("i"), jen.Id("key"), jen.Id("value")))),
		),
		jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("-=").Lit(3),
		next(),
	)
}

func mapDelete() jen.Code {
	return handler(underflow(2),
		jen.Id("key").Op(":=").Add(top(1)),
		container(2),
		mapHeap(
			mapCase(accept(), jen.Id("m").Dot("Delete").Call(jen.Id("key")), jen.Id("i").Dot("heap").Dot("Release").Call(jen.Id("key"))),
			hostMapCase(check(jen.Id("m").Dot("Delete").Call(jen.Id("i"), jen.Id("key")))),
		),
		jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("-=").Lit(2),
		next(),
	)
}

func mapClear() jen.Code {
	return handler(underflow(1),
		container(1),
		mapHeap(
			mapCase(jen.Id("m").Dot("Clear").Call()),
			hostMapCase(jen.Id("m").Dot("Clear").Call()),
		),
		jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("--"),
		next(),
	)
}

func mapKeys() jen.Code {
	return handler(underflow(1),
		container(1),
		jen.Var().Id("m").Op("*").Qual(typesPkg, "Map"),
		jen.Switch(jen.Id("source").Op(":=").Id("i").Dot("heap").Dot("At").Call(jen.Id("addr")).Assert(jen.Type())).Block(
			jen.Case(jen.Op("*").Qual(typesPkg, "Map")).Block(jen.Id("m").Op("=").Id("source")),
			hostMapCase(
				jen.List(jen.Id("copied"), jen.Id("err")).Op(":=").Id("source").Dot("Map").Call(jen.Id("i")),
				jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
				jen.Id("held").Op(":=").Id("addr"),
				jen.Id("addr").Op("=").Id("i").Dot("heap").Dot("Add").Call(jen.Id("copied")),
				top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("addr")),
				jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("held")),
				jen.Id("m").Op("=").Id("copied"),
			),
			jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		),
		// The result is in the heap before it is filled, so a trap while it fills frees it
		// with the keys it already holds; the map reference stays on the stack for unwinding.
		jen.Id("arr").Op(":=").Qual(typesPkg, "MakeArray").Call(jen.Op("&").Id("i").Dot("heap"), jen.Qual(typesPkg, "NewArrayType").Call(jen.Id("m").Dot("Typ").Dot("Key")), jen.Id("m").Dot("Len").Call()),
		jen.Id("out").Op(":=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("heap").Dot("Add").Call(jen.Id("arr"))),
		jen.Id("n").Op(":=").Lit(0),
		jen.Func().Params().Block(
			jen.Defer().Func().Params().Block(
				jen.If(jen.Id("r").Op(":=").Id("recover").Call(), jen.Id("r").Op("!=").Nil()).Block(
					jen.Id("i").Dot("heap").Dot("Release").Call(jen.Id("out")),
					jen.Panic(jen.Id("r")),
				),
			).Call(),
			jen.Id("m").Dot("Range").Call(jen.Func().Params(jen.Id("key"), jen.Id("_").Qual(typesPkg, "Boxed")).Bool().Block(
				jen.If(jen.Id("key").Dot("Kind").Call().Op("==").Qual(typesPkg, "KindRef")).Block(
					jen.If(jen.List(jen.Id("wide"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Dot("At").Call(jen.Id("key").Dot("Ref").Call()).Assert(jen.Qual(typesPkg, "I64")), jen.Id("ok")).Block(
						jen.Id("key").Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("heap").Dot("Add").Call(jen.Id("wide"))),
					).Else().Block(
						jen.Id("i").Dot("heap").Dot("Retain").Call(jen.Id("key")),
					),
				),
				jen.Id("arr").Dot("Elems").Index(jen.Id("n")).Op("=").Id("key"),
				jen.Id("n").Op("++"),
				jen.Return(jen.True()),
			)),
		).Call(),
		jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("addr")),
		top(1).Op("=").Id("out"),
		next(),
	)
}

func mapIter() jen.Code {
	return handler(underflow(1),
		container(1),
		jen.Var().Id("m").Op("*").Qual(typesPkg, "Map"),
		jen.Switch(jen.Id("source").Op(":=").Id("i").Dot("heap").Dot("At").Call(jen.Id("addr")).Assert(jen.Type())).Block(
			jen.Case(jen.Op("*").Qual(typesPkg, "Map")).Block(jen.Id("m").Op("=").Id("source")),
			hostMapCase(
				jen.List(jen.Id("copied"), jen.Id("err")).Op(":=").Id("source").Dot("Map").Call(jen.Id("i")),
				jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
				jen.Id("held").Op(":=").Id("addr"),
				jen.Id("addr").Op("=").Id("i").Dot("heap").Dot("Add").Call(jen.Id("copied")),
				top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("addr")),
				jen.Id("i").Dot("heap").Dot("Dec").Call(jen.Id("held")),
				jen.Id("m").Op("=").Id("copied"),
			),
			jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		),
		jen.Id("iter").Op(":=").Qual(typesPkg, "NewMapIterator").Call(jen.Qual(typesPkg, "Ref").Call(jen.Id("addr")), jen.Id("m")),
		jen.Id("iter").Dot("Next").Call(),
		jen.Id("out").Op(":=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("heap").Dot("Add").Call(jen.Id("iter"))),
		jen.If(jen.Op("!").Id("iter").Dot("Done").Call()).Block(
			jen.If(jen.List(jen.Id("current"), jen.Id("ok")).Op(":=").Id("iter").Dot("Current").Call().Assert(jen.Qual(typesPkg, "Boxed")), jen.Id("ok")).Block(
				jen.Id("i").Dot("heap").Dot("Retain").Call(jen.Id("current")),
			),
		),
		top(1).Op("=").Id("out"),
		next(),
	)
}

// mapHeap dispatches on the map at heap address addr.
func mapHeap(cases ...jen.Code) jen.Code {
	return jen.Switch(jen.Id("m").Op(":=").Id("i").Dot("heap").Dot("At").Call(jen.Id("addr")).Assert(jen.Type())).Block(
		append(cases, jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))))...,
	)
}

// mapCase is the type-switch case of a VM map.
func mapCase(body ...jen.Code) jen.Code {
	return jen.Case(jen.Op("*").Qual(typesPkg, "Map")).Block(body...)
}

// hostMapCase is the type-switch case of a host map view.
func hostMapCase(body ...jen.Code) jen.Code {
	return jen.Case(jen.Op("*").Id("HostMap")).Block(body...)
}

// accept traps unless m takes key.
func accept() jen.Code {
	return jen.If(jen.Op("!").Id("m").Dot("Accepts").Call(jen.Id("key"))).Block(jen.Panic(jen.Id("ErrTypeMismatch")))
}
