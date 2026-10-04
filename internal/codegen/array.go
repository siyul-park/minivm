package codegen

import (
	"fmt"
	"reflect"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

// typedElem is one types.TypedArray instantiation: the Go type its elements
// have, the instr.Kind it stores, and how a boxed operand reads as an element.
type typedElem struct {
	typ  string
	kind instr.Kind
	read func(word jen.Code) jen.Code
}

// typedElems lists the instantiations in dispatch order.
var typedElems = []typedElem{
	{"bool", instr.KindI1, func(word jen.Code) jen.Code { return jen.Add(word).Dot("Bool").Call() }},
	{"int8", instr.KindI8, func(word jen.Code) jen.Code { return jen.Id("int8").Call(jen.Add(word).Dot("I32").Call()) }},
	{"int32", instr.KindI32, func(word jen.Code) jen.Code { return jen.Add(word).Dot("I32").Call() }},
	{"int64", instr.KindI64, func(word jen.Code) jen.Code { return jen.Id("i").Dot("unboxI64").Call(word) }},
	{"float32", instr.KindF32, func(word jen.Code) jen.Code { return jen.Add(word).Dot("F32").Call() }},
	{"float64", instr.KindF64, func(word jen.Code) jen.Code { return jen.Add(word).Dot("F64").Call() }},
}

func containerGet(state *state, current step) (value, error) {
	if state.standalone {
		body := []jen.Code{
			underflow(2),
			jen.Id("index").Op(":=").Int().Call(top(1).Dot("I32").Call()),
			jen.Id("i").Dot("sp").Op("--"),
		}
		body = append(body, containerFallback(current.op, jen.Id("index"), width(current.op))...)
		return value{op: current.op, head: current.op, handler: standalone(current.op, nil, body)}, nil
	}
	if len(state.stack) == 2 && current.op == instr.ARRAY_GET && state.stack[0].object != nil {
		container := state.stack[0]
		index := state.stack[1]
		compile := append([]jen.Code(nil), container.compile...)
		compile = append(compile, index.compile...)
		body := []jen.Code{overflow()}
		body = append(body, index.check...)
		body = append(body, index.body...)
		body = append(body,
			jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.object).Assert(typeName(container.typ)),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("at").Op(":=").Int().Call(index.raw),
			indexGuard(jen.Id("at"), jen.Lit(1), jen.Len(jen.Id("array"))),
			jen.Id("result").Op(":=").Add(boxElem(current.kind, jen.Id("array"), jen.Id("at"))),
			jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("result"),
			jen.Id("i").Dot("sp").Op("++"),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
		)
		compile = append(compile,
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(closure(body...)),
		)
		state.stack = nil
		return value{op: current.op, head: container.head, compile: compile}, nil
	}
	if len(state.stack) == 2 && current.op == instr.ARRAY_GET && isContainerSource(state.stack[0].op) && state.stack[0].typ != nil {
		container := state.stack[0]
		index := state.stack[1]
		compile := append([]jen.Code(nil), container.compile...)
		compile = append(compile, index.compile...)
		body := []jen.Code{overflow()}
		body = append(body, container.check...)
		body = append(body, container.body...)
		body = append(body, index.check...)
		body = append(body, index.body...)
		body = append(body,
			reference(container.boxed),
			jen.Id("at").Op(":=").Int().Call(index.raw),
		)

		tail := func(result jen.Code) []jen.Code {
			return []jen.Code{
				jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(result),
				jen.Id("i").Dot("sp").Op("++"),
				jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
				jen.Return(),
			}
		}

		body = append(body, jen.If(
			jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.raw).Assert(typeName(container.typ)),
			jen.Id("ok"),
		).Block(append([]jen.Code{
			indexGuard(jen.Id("at"), jen.Lit(1), jen.Len(jen.Id("array"))),
		}, tail(boxElem(current.kind, jen.Id("array"), jen.Id("at")))...)...))
		body = append(body, tail(jen.Id("i").Dot("arrayGet").Call(container.raw, jen.Id("at")))...)
		compile = append(compile,
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(closure(body...)),
		)
		state.stack = nil
		return value{op: current.op, head: container.head, compile: compile}, nil
	}
	if len(state.stack) == 2 && current.op == instr.STRUCT_GET && isContainerSource(state.stack[0].op) && state.stack[0].declared != nil {
		return structGet(state, current)
	}
	if len(state.stack) != 1 {
		return value{}, fmt.Errorf("%s needs one constant index", instr.TypeOf(current.op).Mnemonic)
	}
	index := state.stack[0]
	compile := append([]jen.Code(nil), index.compile...)
	body := append([]jen.Code(nil), index.check...)
	body = append(body, index.body...)
	body = append(body, containerFallback(current.op, jen.Int().Call(index.raw), state.width)...)
	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(index.head)),
		jen.Return(closure(body...)),
	)
	state.stack = nil
	return value{op: current.op, head: index.head, compile: compile}, nil
}

// containerFallback emits ARRAY_GET and STRUCT_GET from a resolved index expression,
// delegating the per-representation read to (*Interpreter).arrayGet or
// (*Interpreter).structGet so this generated handler and every fused
// fallback that reaches the same generic case share one runtime copy of the
// dispatch instead of duplicating it in generated code.
func containerFallback(op instr.Opcode, index jen.Code, advance int) []jen.Code {
	method := "arrayGet"
	if op != instr.ARRAY_GET {
		method = "structGet"
	}
	return []jen.Code{
		underflow(1),
		jen.Id("ref").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
		reference(jen.Id("ref")),
		jen.Id("addr").Op(":=").Id("ref").Dot("Ref").Call(),
		jen.Id("result").Op(":=").Id("i").Dot(method).Call(jen.Id("addr"), index),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("--"),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("result"),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
	}
}

func arrayStore(state *state, current step) (value, error) {
	if state.standalone {
		return value{op: current.op, head: current.op, handler: arraySet()}, nil
	}
	if len(state.stack) != 3 {
		return value{}, fmt.Errorf("array.set needs three pending values")
	}

	container, index, val := state.stack[0], state.stack[1], state.stack[2]
	kind, ok := arrayKind(container.typ)
	if !ok {
		return value{}, fmt.Errorf("no fusion lowering for %s", instr.TypeOf(current.op).Mnemonic)
	}
	raw := val.raw
	if raw == nil {
		raw = val.boxed
	}

	compile := append(append(append([]jen.Code(nil), container.compile...), index.compile...), val.compile...)
	body := []jen.Code{overflow()}

	tail := func(array jen.Code) []jen.Code {
		return []jen.Code{
			indexGuard(jen.Id("at"), jen.Lit(1), jen.Len(array)),
			storeElem(kind, array, jen.Id("at"), raw),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
			jen.Return(),
		}
	}

	switch {
	case container.object != nil:
		body = append(body, index.check...)
		body = append(body, index.body...)
		body = append(body, val.check...)
		body = append(body, val.body...)
		body = append(body,
			jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.object).Assert(typeName(container.typ)),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("at").Op(":=").Int().Call(index.raw),
		)
		body = append(body, tail(jen.Id("array"))...)

	case isContainerSource(container.op) && container.typ != nil:
		body = append(body, container.check...)
		body = append(body, container.body...)
		body = append(body, index.check...)
		body = append(body, index.body...)
		body = append(body, val.check...)
		body = append(body, val.body...)
		body = append(body,
			reference(container.boxed),
			jen.Id("at").Op(":=").Int().Call(index.raw),
		)

		body = append(body, jen.If(
			jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.raw).Assert(typeName(container.typ)),
			jen.Id("ok"),
		).Block(tail(jen.Id("array"))...))
		body = append(body,
			jen.Id("i").Dot("arraySet").Call(container.raw, jen.Id("at"), val.boxed),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(state.width),
			jen.Return(),
		)

	default:
		return value{}, fmt.Errorf("no fusion lowering for %s", instr.TypeOf(current.op).Mnemonic)
	}

	compile = append(compile,
		jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
		jen.Return(closure(body...)),
	)
	state.stack = nil
	return value{op: current.op, head: container.head, compile: compile}, nil
}

func arraySet() jen.Code {
	return handler(
		underflow(3),
		jen.Id("val").Op(":=").Add(top(1)),
		jen.Id("idx").Op(":=").Id("int").Call(top(2).Dot("I32").Call()),
		jen.Id("ref").Op(":=").Add(top(3)),
		reference(jen.Id("ref")),
		jen.Id("i").Dot("arraySet").Call(jen.Id("ref").Dot("Ref").Call(), jen.Id("idx"), jen.Id("val")),
		jen.Id("i").Dot("release").Call(jen.Id("ref").Dot("Ref").Call()),
		jen.Id("i").Dot("sp").Op("-=").Lit(3),
		next(),
	)
}

func arrayNew() jen.Code {
	return assertType("ArrayType", arrayKindCases(
		func(e typedElem) jen.Code {
			return closure(underflow(1),
				jen.Id("size").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
				jen.If(jen.Id("i").Dot("sp").Op("<").Id("size").Op("+").Lit(1)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
				jen.Id("val").Op(":=").Id("make").Call(e.array(), jen.Id("size")),
				jen.For(jen.Id("j").Op(":=").Lit(0), jen.Id("j").Op("<").Id("size"), jen.Id("j").Op("++")).Block(
					jen.Id("val").Index(jen.Id("j")).Op("=").Add(e.read(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("size").Op("-").Lit(1).Op("+").Id("j")))),
				),
				jen.Id("i").Dot("sp").Op("-=").Id("size"),
				top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val"))),
				jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3),
			)
		},
		closure(underflow(1),
			jen.Id("size").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
			jen.If(jen.Id("i").Dot("sp").Op("<").Id("size").Op("+").Lit(1)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.Id("val").Op(":=").Id("i").Dot("newArraySized").Call(jen.Id("typ"), jen.Id("size")),
			jen.Id("copy").Call(jen.Id("val").Dot("Elems"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("size").Op("-").Lit(1).Op(":").Id("i").Dot("sp").Op("-").Lit(1))),
			jen.Id("i").Dot("sp").Op("-=").Id("size"),
			top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3),
		),
	))
}

func arrayNewDefault() jen.Code {
	return assertType("ArrayType", jen.Return(closure(underflow(1),
		jen.Id("val").Op(":=").Id("i").Dot("newArrayDefault").Call(jen.Id("typ"), top(1)),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val"))),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(3),
	)))
}

func arrayLen() jen.Code {
	length := func(arg jen.Code) []jen.Code {
		return []jen.Code{jen.Id("n").Op("=").Id("int32").Call(arg)}
	}
	return handler(underflow(1),
		jen.Var().Id("n").Id("int32"),
		arraySwitch(jen.Id("arr").Op(":=").Id("i").Dot("unbox").Call(top(1)).Assert(jen.Type()),
			func(typedElem) []jen.Code { return length(jen.Id("len").Call(jen.Id("arr"))) },
			genericArrayCase(length(jen.Id("len").Call(elems("arr")))...),
			hostArrayCase(length(jen.Id("arr").Dot("Len").Call())...),
		),
		top(1).Op("=").Qual(typesPkg, "BoxI32").Call(jen.Id("n")),
		next(),
	)
}

func arrayFill() jen.Code {
	return handler(underflow(4),
		jen.Id("size").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
		jen.Id("val").Op(":=").Add(top(2)),
		jen.Id("idx").Op(":=").Id("int").Call(top(3).Dot("I32").Call()),
		container(4),
		arrayHeap(func(e typedElem) []jen.Code {
			return []jen.Code{
				bounds(jen.Id("idx"), jen.Id("size"), jen.Id("arr")),
				jen.Id("v").Op(":=").Add(e.read(jen.Id("val"))),
				jen.For(jen.Id("k").Op(":=").Id("idx"), jen.Id("k").Op("<").Id("idx").Op("+").Id("size"), jen.Id("k").Op("++")).Block(jen.Id("arr").Index(jen.Id("k")).Op("=").Id("v")),
			}
		},
			genericArrayCase(
				bounds(jen.Id("idx"), jen.Id("size"), elems("arr")),
				jen.If(jen.Id("val").Dot("Kind").Call().Op("==").Qual(typesPkg, "KindRef").Op("&&").Id("size").Op(">").Lit(1)).Block(jen.Id("i").Dot("retains").Call(jen.Id("val").Dot("Ref").Call(), jen.Id("size").Op("-").Lit(1))),
				jen.For(jen.Id("k").Op(":=").Id("idx"), jen.Id("k").Op("<").Id("idx").Op("+").Id("size"), jen.Id("k").Op("++")).Block(
					jen.Id("old").Op(":=").Add(elems("arr")).Index(jen.Id("k")),
					elems("arr").Index(jen.Id("k")).Op("=").Id("val"),
					jen.Id("i").Dot("releaseBox").Call(jen.Id("old")),
				),
				jen.If(jen.Id("size").Op("<=").Lit(0)).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("val"))),
			),
			hostArrayCase(check(jen.Id("arr").Dot("Fill").Call(jen.Id("i"), jen.Id("idx"), jen.Id("size"), jen.Id("val")))),
		),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("-=").Lit(4),
		next(),
	)
}

func arrayCopy() jen.Code {
	return handler(underflow(5),
		jen.Id("size").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
		jen.If(jen.Id("size").Op("<").Lit(0)).Block(jen.Panic(jen.Id("ErrIndexOutOfRange"))),
		jen.Id("srcOffset").Op(":=").Id("int").Call(top(2).Dot("I32").Call()),
		jen.Id("srcRef").Op(":=").Add(top(3)),
		jen.Id("dstOffset").Op(":=").Id("int").Call(top(4).Dot("I32").Call()),
		jen.Id("dstRef").Op(":=").Add(top(5)),
		jen.If(jen.Id("srcRef").Dot("Kind").Call().Op("!=").Qual(typesPkg, "KindRef").Op("||").Id("dstRef").Dot("Kind").Call().Op("!=").Qual(typesPkg, "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("srcAddr").Op(":=").Id("srcRef").Dot("Ref").Call(),
		jen.Id("dstAddr").Op(":=").Id("dstRef").Dot("Ref").Call(),
		jen.Switch(jen.Id("dst").Op(":=").Id("i").Dot("heap").Index(jen.Id("dstAddr")).Assert(jen.Type())).Block(
			append(
				copyCases(),
				genericArrayCase(
					jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Op("*").Qual(typesPkg, "Array")),
					jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
					bounds(jen.Id("srcOffset"), jen.Id("size"), elems("src")),
					bounds(jen.Id("dstOffset"), jen.Id("size"), elems("dst")),
					jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(window(elems("src"), "srcOffset", "size"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("v"))),
					jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(window(elems("dst"), "dstOffset", "size"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("v"))),
					jen.Id("copy").Call(window(elems("dst"), "dstOffset", "size"), window(elems("src"), "srcOffset", "size")),
				),
				hostArrayCase(jen.For(jen.Id("k").Op(":=").Lit(0), jen.Id("k").Op("<").Id("size"), jen.Id("k").Op("++")).Block(
					check(jen.Id("dst").Dot("SetElement").Call(jen.Id("i"), jen.Id("dstOffset").Op("+").Id("k"), jen.Id("i").Dot("arrayGet").Call(jen.Id("srcAddr"), jen.Id("srcOffset").Op("+").Id("k")))),
				)),
				jen.Default().Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			)...,
		),
		jen.Id("i").Dot("release").Call(jen.Id("srcAddr")),
		jen.Id("i").Dot("release").Call(jen.Id("dstAddr")),
		jen.Id("i").Dot("sp").Op("-=").Lit(5),
		next(),
	)
}

func arrayAppend() jen.Code {
	return handler(underflow(1),
		jen.Id("n").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
		jen.If(jen.Id("n").Op("<").Lit(0).Op("||").Id("i").Dot("sp").Op("<").Id("n").Op("+").Lit(2)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("ref").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Id("n").Op("-").Lit(2)),
		reference(jen.Id("ref")),
		jen.Id("addr").Op(":=").Id("ref").Dot("Ref").Call(),
		jen.Id("base").Op(":=").Id("i").Dot("sp").Op("-").Id("n").Op("-").Lit(1),
		arrayHeap(func(e typedElem) []jen.Code {
			return []jen.Code{
				jen.For(jen.Id("k").Op(":=").Lit(0), jen.Id("k").Op("<").Id("n"), jen.Id("k").Op("++")).Block(
					jen.Id("arr").Op("=").Id("append").Call(jen.Id("arr"), e.read(jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("k")))),
				),
				jen.Id("i").Dot("heap").Index(jen.Id("addr")).Op("=").Id("arr"),
			}
		},
			genericArrayCase(jen.For(jen.Id("k").Op(":=").Lit(0), jen.Id("k").Op("<").Id("n"), jen.Id("k").Op("++")).Block(
				elems("arr").Op("=").Id("append").Call(elems("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Id("k"))),
			)),
			hostArrayCase(check(jen.Id("arr").Dot("Append").Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("base"), jen.Id("base").Op("+").Id("n"))))),
		),
		jen.Id("i").Dot("sp").Op("-=").Id("n").Op("+").Lit(1),
		next(),
	)
}

func arrayDelete() jen.Code {
	return handler(underflow(2),
		jen.Id("idx").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
		container(2),
		jen.Var().Id("val").Qual(typesPkg, "Boxed"),
		arrayHeap(func(e typedElem) []jen.Code {
			return []jen.Code{
				bounds(jen.Id("idx"), jen.Lit(1), jen.Id("arr")),
				jen.Id("val").Op("=").Add(boxElem(e.kind, jen.Id("arr"), jen.Id("idx"))),
				jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":")), jen.Id("arr").Index(jen.Id("idx").Op("+").Lit(1).Op(":"))),
				jen.Id("i").Dot("heap").Index(jen.Id("addr")).Op("=").Id("arr").Index(jen.Op(":").Id("len").Call(jen.Id("arr")).Op("-").Lit(1)),
			}
		},
			genericArrayCase(
				bounds(jen.Id("idx"), jen.Lit(1), elems("arr")),
				jen.Id("val").Op("=").Add(elems("arr")).Index(jen.Id("idx")),
				jen.Id("copy").Call(elems("arr").Index(jen.Id("idx").Op(":")), elems("arr").Index(jen.Id("idx").Op("+").Lit(1).Op(":"))),
				elems("arr").Index(jen.Id("len").Call(elems("arr")).Op("-").Lit(1)).Op("=").Qual(typesPkg, "BoxedNull"),
				elems("arr").Op("=").Add(elems("arr")).Index(jen.Op(":").Id("len").Call(elems("arr")).Op("-").Lit(1)),
			),
			hostArrayCase(
				jen.List(jen.Id("removed"), jen.Id("err")).Op(":=").Id("arr").Dot("Delete").Call(jen.Id("i"), jen.Id("idx")),
				jen.If(jen.Id("err").Op("!=").Nil()).Block(jen.Panic(jen.Id("err"))),
				jen.Id("val").Op("=").Id("removed"),
			),
		),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("--"),
		top(1).Op("=").Id("val"),
		next(),
	)
}

func arraySlice() jen.Code {
	return handler(underflow(3),
		jen.Id("end").Op(":=").Id("int").Call(top(1).Dot("I32").Call()),
		jen.Id("start").Op(":=").Id("int").Call(top(2).Dot("I32").Call()),
		container(3),
		jen.Var().Id("out").Qual(typesPkg, "Value"),
		jen.Id("source").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")),
		view("HostArray", "Array"),
		arraySwitch(jen.Id("arr").Op(":=").Id("source").Assert(jen.Type()),
			func(e typedElem) []jen.Code {
				return []jen.Code{
					sliceGuard(jen.Id("arr")),
					jen.Id("dst").Op(":=").Id("make").Call(e.array(), jen.Id("end").Op("-").Id("start")),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Id("end"))),
					jen.Id("out").Op("=").Id("dst"),
				}
			},
			genericArrayCase(
				sliceGuard(elems("arr")),
				jen.Id("dst").Op(":=").Id("i").Dot("newArraySized").Call(jen.Id("arr").Dot("Typ"), jen.Id("end").Op("-").Id("start")),
				jen.Id("copy").Call(elems("dst"), elems("arr").Index(jen.Id("start").Op(":").Id("end"))),
				jen.Id("out").Op("=").Id("dst"),
			),
		),
		jen.Id("newAddr").Op(":=").Id("i").Dot("alloc").Call(jen.Id("out")),
		jen.If(jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("out").Assert(jen.Op("*").Qual(typesPkg, "Array")), jen.Id("ok")).Block(
			jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(elems("array"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("v"))),
		),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("-=").Lit(2),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("newAddr")),
		next(),
	)
}

func storeElem(kind instr.Kind, array, index, raw jen.Code) jen.Code {
	elem := jen.Add(array).Index(index)
	switch kind {
	case instr.KindI1:
		return elem.Op("=").Add(jen.Add(raw).Op("!=").Lit(0))
	case instr.KindI8:
		return elem.Op("=").Add(jen.Int8().Call(raw))
	case instr.KindI32:
		return elem.Op("=").Add(jen.Int32().Call(raw))
	case instr.KindI64:
		return elem.Op("=").Add(raw)
	case instr.KindF32:
		return elem.Op("=").Add(jen.Float32().Call(raw))
	case instr.KindF64:
		return elem.Op("=").Add(jen.Float64().Call(raw))
	default:
		panic(fmt.Sprintf("unsupported array element kind %s", kind))
	}
}

func boxElem(kind instr.Kind, array, index jen.Code) jen.Code {
	elem := jen.Add(array).Index(index)
	switch kind {
	case instr.KindI1:
		return jen.Qual(typesPkg, "BoxI1").Call(elem)
	case instr.KindI8:
		return jen.Qual(typesPkg, "BoxI8").Call(elem)
	case instr.KindI32:
		return jen.Qual(typesPkg, "BoxI32").Call(elem)
	case instr.KindI64:
		return jen.Id("i").Dot("boxI64").Call(elem)
	case instr.KindF32:
		return jen.Qual(typesPkg, "BoxF32").Call(elem)
	case instr.KindF64:
		return jen.Qual(typesPkg, "BoxF64").Call(elem)
	default:
		panic(fmt.Sprintf("unsupported array element kind %s", kind))
	}
}

func typeName(typ reflect.Type) jen.Code {
	return jen.Qual(typ.PkgPath(), typ.Name())
}

// arrayHeap dispatches on the array at heap address addr.
func arrayHeap(body func(typedElem) []jen.Code, rest ...jen.Code) jen.Code {
	return arraySwitch(jen.Id("arr").Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()), body, rest...)
}

// arraySwitch dispatches on the array behind subject: each typed instantiation
// lowers through typed, and rest handles the others.
func arraySwitch(subject jen.Code, body func(typedElem) []jen.Code, rest ...jen.Code) jen.Code {
	return typeSwitch(subject, typedElems, typedElem.instance, body, rest...)
}

// instance is the type-switch case of a typed array of e.
func (e typedElem) instance() jen.Code { return e.array() }

// hostArrayCase is the type-switch case of a host array view.
func hostArrayCase(body ...jen.Code) jen.Code {
	return jen.Case(jen.Op("*").Id("HostArray")).Block(body...)
}

// genericArrayCase is the type-switch case of a boxed array.
func genericArrayCase(body ...jen.Code) jen.Code {
	return jen.Case(jen.Op("*").Qual(typesPkg, "Array")).Block(body...)
}

// copyCases are the typed destination cases of arrayCopy: the source must be
// the same instantiation.
func copyCases() []jen.Code {
	cases := make([]jen.Code, 0, len(typedElems)+3)
	for _, e := range typedElems {
		cases = append(cases, jen.Case(e.array()).Block(
			jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(e.array()),
			jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			bounds(jen.Id("srcOffset"), jen.Id("size"), jen.Id("src")),
			bounds(jen.Id("dstOffset"), jen.Id("size"), jen.Id("dst")),
			jen.Id("copy").Call(window(jen.Id("dst"), "dstOffset", "size"), window(jen.Id("src"), "srcOffset", "size")),
		))
	}
	return cases
}

// array is the type of a typed array of e.
func (e typedElem) array() *jen.Statement {
	return jen.Qual(typesPkg, "TypedArray").Index(jen.Id(e.typ))
}

// arrayKindCases dispatches on the element kind of typ: each typed kind lowers
// through typed, and generic handles every other.
func arrayKindCases(body func(typedElem) jen.Code, generic jen.Code) jen.Code {
	cases := make([]jen.Code, 0, len(typedElems)+1)
	for _, e := range typedElems {
		cases = append(cases, jen.Case(jen.Qual(typesPkg, e.kindConst())).Block(jen.Return(body(e))))
	}
	cases = append(cases, jen.Default().Block(jen.Return(generic)))
	return jen.Switch(jen.Id("typ").Dot("ElemKind")).Block(cases...)
}

// kindConst is the types.Kind constant name of e.
func (e typedElem) kindConst() string {
	name, _ := fieldKindName(e.kind)
	return "Kind" + name
}

// bounds traps unless [offset, offset+size) lies within slice.
func bounds(offset, size, slice jen.Code) jen.Code {
	return jen.Block(
		jen.Id("offset").Op(":=").Add(offset),
		jen.Id("size").Op(":=").Add(size),
		jen.Id("length").Op(":=").Id("len").Call(slice),
		indexGuard(jen.Id("offset"), jen.Id("size"), jen.Id("length")),
	)
}

func indexGuard(offset, size, length jen.Code) jen.Code {
	return jen.If(jen.Add(offset).Op("<").Lit(0).Op("||").Add(offset).Op("+").Add(size).Op(">").Add(length)).Block(
		jen.Panic(jen.Id("ErrIndexOutOfRange")),
	)
}

// elems is the boxed element slice of the array named name.
func elems(name string) *jen.Statement {
	return jen.Id(name).Dot("Elems")
}

// window is the n-element subslice of slice from offset.
func window(slice jen.Code, offset, n string) *jen.Statement {
	return jen.Add(slice).Index(jen.Id(offset).Op(":").Id(offset).Op("+").Id(n))
}

// sliceGuard traps unless start..end is a valid range of slice.
func sliceGuard(slice jen.Code) jen.Code {
	return jen.If(jen.Id("start").Op("<").Lit(0).Op("||").Id("end").Op(">").Id("len").Call(slice).Op("||").Id("start").Op(">").Id("end")).Block(jen.Panic(jen.Id("ErrIndexOutOfRange")))
}
