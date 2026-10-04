package codegen

import (
	"github.com/dave/jennifer/jen"
)

// typedKey is one types.TypedMap instantiation: the Go type its keys have, how
// a boxed key operand reads as that type, and how a key of that type boxes.
type typedKey struct {
	typ  string
	read func() jen.Code
	box  func(key jen.Code) jen.Code
}

// typedKeys lists the instantiations in dispatch order.
var typedKeys = []typedKey{
	{"int8", func() jen.Code { return jen.Id("key").Dot("I8").Call() }, func(key jen.Code) jen.Code { return jen.Qual(typesPkg, "BoxI8").Call(key) }},
	{"bool", func() jen.Code { return jen.Id("key").Dot("Bool").Call() }, func(key jen.Code) jen.Code { return jen.Qual(typesPkg, "BoxI1").Call(key) }},
	{"int32", func() jen.Code { return jen.Id("key").Dot("I32").Call() }, func(key jen.Code) jen.Code { return jen.Qual(typesPkg, "BoxI32").Call(key) }},
	{"int64", func() jen.Code { return jen.Id("i").Dot("unboxI64").Call(jen.Id("key")) }, func(key jen.Code) jen.Code { return jen.Id("i").Dot("boxI64").Call(key) }},
	{"float32", func() jen.Code { return jen.Id("key").Dot("F32").Call() }, func(key jen.Code) jen.Code { return jen.Qual(typesPkg, "BoxF32").Call(key) }},
	{"float64", func() jen.Code { return jen.Id("key").Dot("F64").Call() }, func(key jen.Code) jen.Code { return jen.Qual(typesPkg, "BoxF64").Call(key) }},
	{
		"string",
		func() jen.Code {
			return jen.String().Call(jen.Id("unboxRef").Index(jen.Qual(typesPkg, "String")).Call(jen.Id("i"), jen.Id("key")))
		},
		func(key jen.Code) jen.Code {
			return jen.Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Qual(typesPkg, "String").Call(key)))
		},
	},
}

func mapNew() jen.Code {
	return assertType("MapType",
		jen.Return(closure(underflow(1),
			jen.Id("size").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
			jen.If(jen.Id("size").Op("<").Lit(0)).Block(jen.Panic(jen.Id("ErrIndexOutOfRange"))),
			jen.If(jen.Id("i").Dot("sp").Op("<").Id("size").Op("*").Lit(2).Op("+").Lit(1)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.Id("m").Op(":=").Qual(typesPkg, "NewMapForType").Call(jen.Id("typ"), jen.Id("size")),
			jen.Id("base").Op(":=").Id("i").Dot("sp").Op("-").Lit(1).Op("-").Id("size").Op("*").Lit(2),
			jen.For(jen.Id("j").Op(":=").Lit(0), jen.Id("j").Op("<").Id("size"), jen.Id("j").Op("++")).Block(
				jen.Id("key").Op(":=").Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("j").Op("*").Lit(2)),
				jen.Id("value").Op(":=").Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("j").Op("*").Lit(2).Op("+").Lit(1)),
				mapSwitch(jen.Id("m").Op(":=").Id("m").Assert(jen.Type()), setTyped, mapEntrySet()),
			),
			jen.Id("addr").Op(":=").Id("i").Dot("alloc").Call(jen.Id("m")),
			jen.Id("i").Dot("sp").Op("=").Id("base").Op("+").Lit(1),
			jen.Id("i").Dot("stack").Index(jen.Id("base")).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("addr")),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3),
		)),
	)
}

func mapNewDefault() jen.Code {
	return assertType("MapType",
		jen.Return(closure(underflow(1),
			jen.Id("capacity").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
			jen.If(jen.Id("capacity").Op("<").Lit(0)).Block(jen.Panic(jen.Id("ErrIndexOutOfRange"))),
			top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Qual(typesPkg, "NewMapForType").Call(jen.Id("typ"), jen.Id("capacity")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3),
		)),
	)
}

func mapLen() jen.Code {
	length := []jen.Code{jen.Id("n").Op("=").Id("m").Dot("Len").Call()}
	return handler(underflow(1),
		container(1),
		jen.Id("n").Op(":=").Lit(0),
		mapHeap(func(typedKey) []jen.Code { return length },
			genericMapCase(length...),
			hostMapCase(length...),
		),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		top(1).Op("=").Qual(typesPkg, "BoxI32").Call(jen.Id("int32").Call(jen.Id("n"))),
		next(),
	)
}

func mapGet() jen.Code {
	return handler(underflow(2),
		jen.Id("key").Op(":=").Add(top(1)),
		container(2),
		jen.Var().Id("result").Qual(typesPkg, "Boxed"),
		jen.Var().Id("owned").Id("bool"),
		mapHeap(getTyped,
			genericMapCase(
				jen.List(jen.Id("k"), jen.Id("entryKey")).Op(":=").Id("i").Dot("mapKey").Call(jen.Id("key")),
				jen.List(jen.Id("entry"), jen.Id("ok")).Op(":=").Id("m").Dot("Get").Call(jen.Id("k")),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("entryKey")),
				jen.If(jen.Id("ok")).Block(jen.Id("result").Op("=").Id("entry").Dot("Value")).Else().Block(jen.Id("result").Op("=").Id("m").Dot("Zero")),
			),
			hostMapCase(
				jen.List(jen.Id("value"), jen.Id("present"), jen.Id("err")).Op(":=").Id("m").Dot("Get").Call(jen.Id("i"), jen.Id("key")),
				jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
				jen.List(jen.Id("result"), jen.Id("owned")).Op("=").List(jen.Id("value"), jen.Lit(true)),
				jen.Id("_").Op("=").Id("present"),
			),
		),
		jen.If(jen.Op("!").Id("owned")).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("result"))),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
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
		jen.Var().Id("owned").Id("bool"),
		jen.Var().Id("found").Id("bool"),
		mapHeap(lookupTyped,
			genericMapCase(
				jen.List(jen.Id("k"), jen.Id("entryKey")).Op(":=").Id("i").Dot("mapKey").Call(jen.Id("key")),
				jen.List(jen.Id("entry"), jen.Id("ok")).Op(":=").Id("m").Dot("Get").Call(jen.Id("k")),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("entryKey")),
				jen.Id("found").Op("=").Id("ok"),
				jen.If(jen.Id("ok")).Block(jen.Id("result").Op("=").Id("entry").Dot("Value")).Else().Block(jen.Id("result").Op("=").Id("m").Dot("Zero")),
			),
			hostMapCase(
				jen.List(jen.Id("value"), jen.Id("present"), jen.Id("err")).Op(":=").Id("m").Dot("Get").Call(jen.Id("i"), jen.Id("key")),
				jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
				jen.List(jen.Id("result"), jen.Id("owned")).Op("=").List(jen.Id("value"), jen.Lit(true)),
				jen.Id("found").Op("=").Id("present"),
			),
		),
		jen.If(jen.Op("!").Id("owned")).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("result"))),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
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
		mapHeap(setTyped,
			mapEntrySet(),
			hostMapCase(check(jen.Id("m").Dot("Set").Call(jen.Id("i"), jen.Id("key"), jen.Id("value")))),
		),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("-=").Lit(3),
		next(),
	)
}

func mapDelete() jen.Code {
	return handler(underflow(2),
		jen.Id("key").Op(":=").Add(top(1)),
		container(2),
		mapHeap(
			func(k typedKey) []jen.Code {
				return []jen.Code{
					jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").Id("m").Dot("Delete").Call(k.read()),
					releaseOld(),
				}
			},
			genericMapCase(
				jen.List(jen.Id("k"), jen.Id("entryKey")).Op(":=").Id("i").Dot("mapKey").Call(jen.Id("key")),
				jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").Id("m").Dot("Delete").Call(jen.Id("k")),
				releaseEntry(),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("entryKey")),
			),
			hostMapCase(check(jen.Id("m").Dot("Delete").Call(jen.Id("i"), jen.Id("key")))),
		),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("-=").Lit(2),
		next(),
	)
}

func mapClear() jen.Code {
	clearing := func(param jen.Code, body ...jen.Code) jen.Code {
		return jen.Id("m").Dot("Clear").Call(jen.Func().Params(param).Block(body...))
	}
	return handler(underflow(1),
		container(1),
		mapHeap(
			func(typedKey) []jen.Code {
				return []jen.Code{clearing(jen.Id("value").Qual(typesPkg, "Boxed"), jen.Id("i").Dot("releaseBox").Call(jen.Id("value")))}
			},
			genericMapCase(clearing(
				jen.Id("entry").Qual(typesPkg, "MapEntry"),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("entry").Dot("Key")),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("entry").Dot("Value")),
			)),
			hostMapCase(jen.Id("m").Dot("Clear").Call()),
		),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("--"),
		next(),
	)
}

func mapKeys() jen.Code {
	collect := func(params []jen.Code, body ...jen.Code) []jen.Code {
		return []jen.Code{
			jen.Id("keyType").Op("=").Id("m").Dot("Typ").Dot("Key"),
			jen.Id("elems").Op("=").Id("make").Call(jen.Index().Qual(typesPkg, "Boxed"), jen.Lit(0), jen.Id("m").Dot("Len").Call()),
			jen.Id("m").Dot("Range").Call(jen.Func().Params(params...).Block(body...)),
		}
	}
	return handler(underflow(1),
		container(1),
		jen.Var().Id("keyType").Qual(typesPkg, "Type"),
		jen.Var().Id("elems").Index().Qual(typesPkg, "Boxed"),
		jen.Id("source").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")),
		view("HostMap", "Map"),
		mapSwitch(jen.Id("m").Op(":=").Id("source").Assert(jen.Type()),
			func(k typedKey) []jen.Code {
				return collect(
					[]jen.Code{jen.Id("k").Id(k.typ), jen.Id("_").Qual(typesPkg, "Boxed")},
					jen.Id("elems").Op("=").Id("append").Call(jen.Id("elems"), k.box(jen.Id("k"))),
				)
			},
			genericMapCase(collect(
				[]jen.Code{jen.Id("_").Qual(typesPkg, "MapKey"), jen.Id("entry").Qual(typesPkg, "MapEntry")},
				jen.Id("i").Dot("retainBox").Call(jen.Id("entry").Dot("Key")),
				jen.Id("elems").Op("=").Id("append").Call(jen.Id("elems"), jen.Id("entry").Dot("Key")),
			)...),
		),
		jen.Id("arr").Op(":=").Id("i").Dot("newArraySized").Call(jen.Qual(typesPkg, "NewArrayType").Call(jen.Id("keyType")), jen.Id("len").Call(jen.Id("elems"))),
		jen.Id("copy").Call(elems("arr"), jen.Id("elems")),
		jen.Id("out").Op(":=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("arr"))),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		top(1).Op("=").Id("out"),
		next(),
	)
}

func mapIter() jen.Code {
	instances := make([]jen.Code, 0, len(typedKeys)+1)
	for _, k := range typedKeys {
		instances = append(instances, k.instance())
	}
	instances = append(instances, jen.Op("*").Qual(typesPkg, "Map"))
	return handler(underflow(1),
		container(1),
		jen.Id("source").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")),
		view("HostMap", "Map"),
		jen.Switch(jen.Id("source").Assert(jen.Type())).Block(
			jen.Case(instances...).Block(),
			jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		),
		jen.Id("iter").Op(":=").Qual(typesPkg, "NewMapIterator").Call(jen.Qual(typesPkg, "Ref").Call(jen.Id("addr")), jen.Id("source")),
		jen.Id("iter").Dot("Next").Call(),
		jen.Id("out").Op(":=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("iter"))),
		jen.If(jen.Op("!").Id("iter").Dot("Done").Call()).Block(
			jen.Id("current").Op(":=").Id("iter").Dot("Current").Call(),
			jen.Switch(jen.Id("current").Op(":=").Id("current").Assert(jen.Type())).Block(
				jen.Case(jen.Qual(typesPkg, "Boxed")).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("current"))),
				jen.Case(jen.Qual(typesPkg, "Ref")).Block(jen.Id("i").Dot("retain").Call(jen.Id("int").Call(jen.Id("current")))),
			),
		),
		top(1).Op("=").Id("out"),
		next(),
	)
}

// mapHeap dispatches on the map at heap address addr.
func mapHeap(body func(typedKey) []jen.Code, rest ...jen.Code) jen.Code {
	return mapSwitch(jen.Id("m").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()), body, rest...)
}

// mapSwitch dispatches on the map behind subject: each typed instantiation
// lowers through typed, and rest handles the others.
func mapSwitch(subject jen.Code, body func(typedKey) []jen.Code, rest ...jen.Code) jen.Code {
	return typeSwitch(subject, typedKeys, typedKey.instance, body, rest...)
}

func (k typedKey) instance() jen.Code {
	return jen.Op("*").Qual(typesPkg, "TypedMap").Index(jen.Id(k.typ))
}

// hostMapCase is the type-switch case of a host map view.
func hostMapCase(body ...jen.Code) jen.Code {
	return jen.Case(jen.Op("*").Id("HostMap")).Block(body...)
}

// setTyped stores value under the typed key of k, releasing the value it
// replaced.
func setTyped(k typedKey) []jen.Code {
	return []jen.Code{
		jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").Id("m").Dot("Set").Call(k.read(), jen.Id("value")),
		releaseOld(),
	}
}

// getTyped reads the value under the typed key of k into result, or the map's
// zero when the key is absent.
func getTyped(k typedKey) []jen.Code {
	return []jen.Code{
		jen.List(jen.Id("value"), jen.Id("ok")).Op(":=").Id("m").Dot("Get").Call(k.read()),
		jen.If(jen.Id("ok")).Block(jen.Id("result").Op("=").Id("value")).Else().Block(jen.Id("result").Op("=").Id("m").Dot("Zero")),
	}
}

// lookupTyped is getTyped reporting presence in found.
func lookupTyped(k typedKey) []jen.Code {
	return []jen.Code{
		jen.List(jen.Id("result"), jen.Id("found")).Op("=").Id("m").Dot("Get").Call(k.read()),
		jen.If(jen.Op("!").Id("found")).Block(jen.Id("result").Op("=").Id("m").Dot("Zero")),
	}
}

// mapEntrySet stores value under key in a generic map, releasing the entry it
// replaced.
func mapEntrySet() jen.Code {
	return genericMapCase(
		jen.List(jen.Id("k"), jen.Id("entryKey")).Op(":=").Id("i").Dot("mapKey").Call(jen.Id("key")),
		jen.Id("entry").Op(":=").Qual(typesPkg, "MapEntry").Values(jen.Dict{jen.Id("Key"): jen.Id("entryKey"), jen.Id("Value"): jen.Id("value")}),
		jen.List(jen.Id("old"), jen.Id("ok")).Op(":=").Id("m").Dot("Set").Call(jen.Id("k"), jen.Id("entry")),
		releaseEntry(),
	)
}

// genericMapCase is the type-switch case of a boxed-key map.
func genericMapCase(body ...jen.Code) jen.Code {
	return jen.Case(jen.Op("*").Qual(typesPkg, "Map")).Block(body...)
}

// releaseOld releases the value a typed Delete or Set displaced.
func releaseOld() jen.Code {
	return jen.If(jen.Id("ok")).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old")))
}

// releaseEntry releases the key and value of the entry a generic Delete or Set
// displaced.
func releaseEntry() jen.Code {
	return jen.If(jen.Id("ok")).Block(
		jen.Id("i").Dot("releaseBox").Call(jen.Id("old").Dot("Key")),
		jen.Id("i").Dot("releaseBox").Call(jen.Id("old").Dot("Value")),
	)
}
