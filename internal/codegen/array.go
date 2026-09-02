package codegen

import (
	"fmt"
	"reflect"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

func containerGet(state *state, current step) (value, error) {
	if state.standalone {
		body := []jen.Code{
			jen.If(jen.Id("i").Dot("sp").Op("<").Lit(2)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
			jen.Id("index").Op(":=").Int().Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)).Dot("I32").Call()),
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
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
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
			jen.If(jen.Add(container.boxed).Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("at").Op(":=").Int().Call(index.raw),
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
		// arrayGuard only proves the container's declared element kind, never
		// the runtime value's concrete representation: LOCAL_SET, GLOBAL_SET,
		// and a closure's captured upvalue all assign into a declared slot
		// without re-checking the assigned value's concrete representation
		// against it (e.g. ARRAY_NEW_DEFAULT's ref-element path stores
		// through a differently-declared alias, or a store from a source
		// whose static type verification could not pin down leaves a
		// concrete TypedArray[U] with U != T in the slot), so a miss on the
		// specialized TypedArray[T] assertion falls back to
		// (*Interpreter).arrayGet, the same generic reader the unfused
		// handler calls unconditionally — every other TypedArray[_]
		// representation and the generic *types.Array alike — instead of
		// trapping a case the unfused handler accepts. This arm still
		// returns on its own success path, so no variable is live across it
		// and the fallback below.
		body = append(body, jen.If(
			jen.List(jen.Id("array"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(container.raw).Assert(typeName(container.typ)),
			jen.Id("ok"),
		).Block(append([]jen.Code{
			indexGuard(jen.Id("at"), jen.Lit(1), jen.Len(jen.Id("array"))),
		}, tail(boxElem(current.kind, jen.Id("array"), jen.Id("at")))...)...))
		body = append(body, tail(jen.Id("i").Dot("arrayGet").Call(container.raw, jen.Id("at")))...)
		compile = append(compile,
			jen.Id("c").Dot("ip").Op("+=").Lit(width(container.head)),
			jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
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
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	state.stack = nil
	return value{op: current.op, head: index.head, compile: compile}, nil
}

// containerFallback emits ARRAY_GET and STRUCT_GET from a resolved index expression,
// delegating the per-representation read to (*Interpreter).arrayGet or
// (*Interpreter).structField so this generated handler and every fused
// fallback that reaches the same generic case share one runtime copy of the
// dispatch instead of duplicating it in generated code.
func containerFallback(op instr.Opcode, index jen.Code, advance int) []jen.Code {
	method := "arrayGet"
	if op != instr.ARRAY_GET {
		method = "structField"
	}
	return []jen.Code{
		jen.If(jen.Id("i").Dot("sp").Op("==").Lit(0)).Block(jen.Panic(jen.Id("ErrStackUnderflow"))),
		jen.Id("ref").Op(":=").Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Lit(1)),
		jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("addr").Op(":=").Id("ref").Dot("Ref").Call(),
		jen.Id("result").Op(":=").Id("i").Dot(method).Call(jen.Id("addr"), index),
		jen.Id("i").Dot("release").Call(jen.Id("addr")),
		jen.Id("i").Dot("sp").Op("--"),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Id("result"),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
	}
}

func indexGuard(offset, size, length jen.Code) jen.Code {
	return jen.If(jen.Add(offset).Op("<").Lit(0).Op("||").Add(offset).Op("+").Add(size).Op(">").Add(length)).Block(
		jen.Panic(jen.Id("ErrIndexOutOfRange")),
	)
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

	// tail emits the shared bounds-check/store/advance-ip/return sequence
	// once at, array, and raw are resolved; both acquisition arms below
	// return through it on their success path.
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
			jen.If(jen.Add(container.boxed).Dot("Kind").Call().Op("!=").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
			jen.Id("at").Op(":=").Int().Call(index.raw),
		)
		// A miss on the specialized TypedArray[T] assertion falls back to
		// (*Interpreter).arraySet, the same generic writer arraySet() calls
		// unconditionally: the declared type proves only what to specialize
		// for, not the runtime representation.
		//
		// The container is borrowed, never retained or released here,
		// unlike the standalone arraySet() handler, which pops an owned ref
		// and releases it after the write.
		//
		// A fused sequence pushes none of its three operands, so its net
		// stack effect is zero: i.sp is left unchanged in both arms.
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
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(body...)),
	)
	state.stack = nil
	return value{op: current.op, head: container.head, compile: compile}, nil
}

func storeElem(kind instr.Kind, array, index, raw jen.Code) jen.Code {
	elem := jen.Add(array).Index(index)
	switch kind {
	case instr.KindI1:
		return jen.List(elem).Op("=").List(jen.Add(raw).Op("!=").Lit(0))
	case instr.KindI8:
		return jen.List(elem).Op("=").List(jen.Int8().Call(raw))
	case instr.KindI32:
		return jen.List(elem).Op("=").List(jen.Int32().Call(raw))
	case instr.KindI64:
		return jen.List(elem).Op("=").List(raw)
	case instr.KindF32:
		return jen.List(elem).Op("=").List(jen.Float32().Call(raw))
	case instr.KindF64:
		return jen.List(elem).Op("=").List(jen.Float64().Call(raw))
	default:
		panic(fmt.Sprintf("unsupported array element kind %s", kind))
	}
}

func boxElem(kind instr.Kind, array, index jen.Code) jen.Code {
	elem := jen.Add(array).Index(index)
	switch kind {
	case instr.KindI1:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI1").Call(elem)
	case instr.KindI8:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI8").Call(elem)
	case instr.KindI32:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(elem)
	case instr.KindI64:
		return jen.Id("i").Dot("boxI64").Call(elem)
	case instr.KindF32:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(elem)
	case instr.KindF64:
		return jen.Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(elem)
	default:
		panic(fmt.Sprintf("unsupported array element kind %s", kind))
	}
}

func typeName(typ reflect.Type) jen.Code {
	return jen.Qual(typ.PkgPath(), typ.Name())
}

func arrayAppend() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("n")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.If(jen.Id("n").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("n").Op("+").Add(jen.Lit(2))))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("n")).Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.List(jen.Id("base")).Op(":=").List(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("n")).Op("-").Add(jen.Lit(1))),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("Bool").Call()))),
				jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("int8").Call(jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("I32").Call())))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("I32").Call()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))))))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("F32").Call()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr")).Op("=").List(jen.Id("append").Call(jen.Id("arr"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))).Dot("F64").Call()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr"))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("n")), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Dot("Elems")).Op("=").List(jen.Id("append").Call(jen.Id("arr").Dot("Elems"), jen.Id("i").Dot("stack").Index(jen.Id("base").Op("+").Add(jen.Id("k"))))))),
				jen.Case(jen.Op("*").Add(jen.Id("HostArray"))).Block(jen.If(jen.List(jen.Id("err")).Op(":=").List(jen.Id("arr").Dot("Append").Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("base"), jen.Id("base").Op("+").Add(jen.Id("n"))))), jen.Id("err").Op("!=").Add(jen.Id("nil"))).Block(jen.Id("panic").Call(jen.Id("err")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("n").Op("+").Add(jen.Lit(1))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayCopy() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(5))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.If(jen.Id("size").Op("<").Lit(0)).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
			jen.List(jen.Id("srcOffset")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))).Dot("I32").Call())),
			jen.List(jen.Id("srcRef")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.List(jen.Id("dstOffset")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(4))).Dot("I32").Call())),
			jen.List(jen.Id("dstRef")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(5)))),
			jen.If(jen.Id("srcRef").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef")).Op("||").Add(jen.Id("dstRef").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("srcAddr")).Op(":=").List(jen.Id("srcRef").Dot("Ref").Call()),
			jen.List(jen.Id("dstAddr")).Op(":=").List(jen.Id("dstRef").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("dst")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("dstAddr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool")))),
				jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
				jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Id("copy").Call(jen.Id("dst").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.List(jen.Id("src"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("srcAddr")).Assert(jen.Op("*").Add(jen.Id("types").Dot("Array")))),
					jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("srcOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("src").Dot("Elems"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("dstOffset")),
						jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
						jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("dst").Dot("Elems"))),
						jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(jen.Id("src").Dot("Elems").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("v"))),
					jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(jen.Id("dst").Dot("Elems").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("v"))),
					jen.Id("copy").Call(jen.Id("dst").Dot("Elems").Index(jen.Id("dstOffset").Op(":").Add(jen.Id("dstOffset").Op("+").Add(jen.Id("size")))), jen.Id("src").Dot("Elems").Index(jen.Id("srcOffset").Op(":").Add(jen.Id("srcOffset").Op("+").Add(jen.Id("size")))))),
				jen.Case(jen.Op("*").Add(jen.Id("HostArray"))).Block(jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Lit(0)), jen.Id("k").Op("<").Add(jen.Id("size")), jen.Id("k").Op("++")).Block(jen.If(jen.List(jen.Id("err")).Op(":=").List(jen.Id("dst").Dot("SetElement").Call(jen.Id("i"), jen.Id("dstOffset").Op("+").Add(jen.Id("k")), jen.Id("i").Dot("arrayGet").Call(jen.Id("srcAddr"), jen.Id("srcOffset").Op("+").Add(jen.Id("k"))))), jen.Id("err").Op("!=").Add(jen.Id("nil"))).Block(jen.Id("panic").Call(jen.Id("err"))))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("srcAddr")),
			jen.Id("i").Dot("release").Call(jen.Id("dstAddr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(5)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayDelete() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Var().Add(jen.List(jen.Id("val"))).Add(jen.Id("types").Dot("Boxed")),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
				jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
				jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
				jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("arr").Index(jen.Id("idx")))),
				jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
				jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxI8").Call(jen.Id("arr").Index(jen.Id("idx")))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("arr").Index(jen.Id("idx"))))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("arr").Index(jen.Id("idx"))))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("arr").Index(jen.Id("idx"))))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("arr").Index(jen.Id("idx"))))),
					jen.Id("copy").Call(jen.Id("arr").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))).Op("=").List(jen.Id("arr").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Lit(1)),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr").Dot("Elems"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("arr").Dot("Elems").Index(jen.Id("idx"))),
					jen.Id("copy").Call(jen.Id("arr").Dot("Elems").Index(jen.Id("idx").Op(":").Add(jen.Empty())), jen.Id("arr").Dot("Elems").Index(jen.Id("idx").Op("+").Add(jen.Lit(1)).Op(":").Add(jen.Empty()))),
					jen.List(jen.Id("arr").Dot("Elems").Index(jen.Id("len").Call(jen.Id("arr").Dot("Elems")).Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxedNull")),
					jen.List(jen.Id("arr").Dot("Elems")).Op("=").List(jen.Id("arr").Dot("Elems").Index(jen.Empty().Op(":").Add(jen.Id("len").Call(jen.Id("arr").Dot("Elems")).Op("-").Add(jen.Lit(1)))))),
				jen.Case(jen.Op("*").Add(jen.Id("HostArray"))).Block(jen.List(jen.Id("removed"), jen.Id("err")).Op(":=").List(jen.Id("arr").Dot("Delete").Call(jen.Id("i"), jen.Id("idx"))),
					jen.If(jen.Id("err").Op("!=").Add(jen.Id("nil"))).Block(jen.Id("panic").Call(jen.Id("err"))),
					jen.List(jen.Id("val")).Op("=").List(jen.Id("removed"))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayFill() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(4))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(4)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
				jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
				jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
				jen.List(jen.Id("v")).Op(":=").List(jen.Id("val").Dot("Bool").Call()),
				jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("int8").Call(jen.Id("val").Dot("I32").Call())),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("val").Dot("I32").Call()),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("val"))),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("val").Dot("F32").Call()),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.List(jen.Id("v")).Op(":=").List(jen.Id("val").Dot("F64").Call()),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("arr").Index(jen.Id("k"))).Op("=").List(jen.Id("v")))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.Block(jen.List(jen.Id("offset")).Op(":=").List(jen.Id("idx")),
					jen.List(jen.Id("size")).Op(":=").List(jen.Id("size")),
					jen.List(jen.Id("length")).Op(":=").List(jen.Id("len").Call(jen.Id("arr").Dot("Elems"))),
					jen.If(jen.Id("offset").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("offset").Op("+").Add(jen.Id("size")).Op(">").Add(jen.Id("length")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange")))),
					jen.If(jen.Id("val").Dot("Kind").Call().Op("==").Add(jen.Id("types").Dot("KindRef")).Op("&&").Add(jen.Id("size").Op(">").Add(jen.Lit(1)))).Block(jen.Id("i").Dot("retains").Call(jen.Id("val").Dot("Ref").Call(), jen.Id("size").Op("-").Add(jen.Lit(1)))),
					jen.For(jen.List(jen.Id("k")).Op(":=").List(jen.Id("idx")), jen.Id("k").Op("<").Add(jen.Id("idx").Op("+").Add(jen.Id("size"))), jen.Id("k").Op("++")).Block(jen.List(jen.Id("old")).Op(":=").List(jen.Id("arr").Dot("Elems").Index(jen.Id("k"))),
						jen.List(jen.Id("arr").Dot("Elems").Index(jen.Id("k"))).Op("=").List(jen.Id("val")),
						jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
					jen.If(jen.Id("size").Op("<=").Add(jen.Lit(0))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("val")))),
				jen.Case(jen.Op("*").Add(jen.Id("HostArray"))).Block(jen.If(jen.List(jen.Id("err")).Op(":=").List(jen.Id("arr").Dot("Fill").Call(jen.Id("i"), jen.Id("idx"), jen.Id("size"), jen.Id("val"))), jen.Id("err").Op("!=").Add(jen.Id("nil"))).Block(jen.Id("panic").Call(jen.Id("err")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(4)),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayLen() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.Var().Add(jen.List(jen.Id("n"))).Add(jen.Id("int32")),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("i").Dot("unbox").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr"))))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("arr").Dot("Elems"))))),
				jen.Case(jen.Op("*").Add(jen.Id("HostArray"))).Block(jen.List(jen.Id("n")).Op("=").List(jen.Id("int32").Call(jen.Id("arr").Dot("Len").Call()))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("n"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func arrayNew() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("ArrayType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.Switch(jen.Id("typ").Dot("ElemKind")).Block(jen.Case(jen.Id("types").Dot("KindI1")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool")), jen.Id("size"))),
			jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("Bool").Call())),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI8")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("int8").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("I32").Call()))),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("I32").Call())),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j")))))),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("F32").Call())),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64")), jen.Id("size"))),
				jen.For(jen.List(jen.Id("j")).Op(":=").List(jen.Lit(0)), jen.Id("j").Op("<").Add(jen.Id("size")), jen.Id("j").Op("++")).Block(jen.List(jen.Id("val").Index(jen.Id("j"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op("+").Add(jen.Id("j"))).Dot("F64").Call())),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Default().Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
				jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Id("size").Op("+").Add(jen.Lit(1)))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("newArraySized").Call(jen.Id("typ"), jen.Id("size"))),
				jen.Id("copy").Call(jen.Id("val").Dot("Elems"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Id("size")).Op("-").Add(jen.Lit(1)).Op(":").Add(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
				jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Id("size")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))))
}

func arrayNewDefault() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("c").Dot("types")))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))))),
		jen.List(jen.Id("typ"), jen.Id("ok")).Op(":=").List(jen.Id("c").Dot("types").Index(jen.Id("idx")).Assert(jen.Op("*").Add(jen.Id("types").Dot("ArrayType")))),
		jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))))),
		jen.Switch(jen.Id("typ").Dot("ElemKind")).Block(jen.Case(jen.Id("types").Dot("KindI1")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool")), jen.Id("size"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI8")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI32")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindI64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindF32")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Case(jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64")), jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))),
			jen.Default().Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(1))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
				jen.List(jen.Id("size")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
				jen.If(jen.Id("size").Op("<").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
				jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("newArraySized").Call(jen.Id("typ"), jen.Id("int").Call(jen.Id("size")))),
				jen.For(jen.List(jen.Id("j")).Op(":=").Range().Add(jen.Id("val").Dot("Elems"))).Block(jen.List(jen.Id("val").Dot("Elems").Index(jen.Id("j"))).Op("=").List(jen.Id("types").Dot("BoxedNull"))),
				jen.Id("i").Dot("retains").Call(jen.Lit(0), jen.Id("int").Call(jen.Id("size"))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val")))),
				jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))))
}

func arraySet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(
		jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(
			jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(3))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.Id("i").Dot("arraySet").Call(jen.Id("ref").Dot("Ref").Call(), jen.Id("idx"), jen.Id("val")),
			jen.Id("i").Dot("release").Call(jen.Id("ref").Dot("Ref").Call()),
			jen.Id("i").Dot("sp").Op("-=").Lit(3),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"),
		)))
}

func arraySlice() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(3))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("end")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call())),
			jen.List(jen.Id("start")).Op(":=").List(jen.Id("int").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))).Dot("I32").Call())),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(3)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.Var().Add(jen.List(jen.Id("out"))).Add(jen.Id("types").Dot("Value")),
			jen.List(jen.Id("source")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr"))),
			jen.If(jen.List(jen.Id("view"), jen.Id("ok")).Op(":=").List(jen.Id("source").Assert(jen.Op("*").Add(jen.Id("HostArray")))), jen.Id("ok")).Block(jen.List(jen.Id("value"), jen.Id("err")).Op(":=").List(jen.Id("view").Dot("Array").Call(jen.Id("i"))), jen.If(jen.Id("err").Op("!=").Add(jen.Id("nil"))).Block(jen.Id("panic").Call(jen.Id("err"))), jen.List(jen.Id("source")).Op("=").List(jen.Id("value"))),
			jen.Switch(jen.List(jen.Id("arr")).Op(":=").List(jen.Id("source").Assert(jen.Type()))).Block(jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
				jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("bool")), jen.Id("end").Op("-").Add(jen.Id("start")))),
				jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
				jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int8")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int64")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float32")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("dst")).Op(":=").List(jen.Id("make").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("float64")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("dst"), jen.Id("arr").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("dst"))),
				jen.Case(jen.Op("*").Add(jen.Id("types").Dot("Array"))).Block(jen.If(jen.Id("start").Op("<").Add(jen.Lit(0)).Op("||").Add(jen.Id("end").Op(">").Add(jen.Id("len").Call(jen.Id("arr").Dot("Elems")))).Op("||").Add(jen.Id("start").Op(">").Add(jen.Id("end")))).Block(jen.Id("panic").Call(jen.Id("ErrIndexOutOfRange"))),
					jen.List(jen.Id("elems")).Op(":=").List(jen.Id("make").Call(jen.Index().Add(jen.Id("types").Dot("Boxed")), jen.Id("end").Op("-").Add(jen.Id("start")))),
					jen.Id("copy").Call(jen.Id("elems"), jen.Id("arr").Dot("Elems").Index(jen.Id("start").Op(":").Add(jen.Id("end")))),
					jen.For(jen.List(jen.Id("_"), jen.Id("v")).Op(":=").Range().Add(jen.Id("elems"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("v"))),
					jen.List(jen.Id("out")).Op("=").List(jen.Id("i").Dot("newArray").Call(jen.Id("arr").Dot("Typ"), jen.Id("elems")))),
				jen.Default().Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch")))),
			jen.List(jen.Id("newAddr")).Op(":=").List(jen.Id("i").Dot("alloc").Call(jen.Id("out"))),
			jen.Id("i").Dot("release").Call(jen.Id("addr")),
			jen.List(jen.Id("i").Dot("sp")).Op("-=").List(jen.Lit(2)),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("newAddr"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}
