package codegen

import (
	"github.com/dave/jennifer/jen"
)

// stringConcat joins the two operand strings. Results share one append-only
// byte buffer: when the left operand's text ends exactly where the buffer ends,
// the right operand is appended past that end and the join is published as a new
// cell viewing the longer prefix. Bytes below any published length are never
// rewritten, so every existing string keeps its own content whatever its
// reference count, and an accumulating join costs no prefix copy. Any other left
// operand starts a fresh buffer from a copy.
func stringConcat() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(
			jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("right"), jen.Id("left")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2)))),
			jen.If(jen.Id("left").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef")).Op("||").Id("right").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("leftAddr"), jen.Id("rightAddr")).Op(":=").List(jen.Id("left").Dot("Ref").Call(), jen.Id("right").Dot("Ref").Call()),
			jen.List(jen.Id("leftText"), jen.Id("leftOK")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("leftAddr")).Assert(jen.Id("types").Dot("String"))),
			jen.If(jen.Op("!").Add(jen.Id("leftOK"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("rightText"), jen.Id("rightOK")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("rightAddr")).Assert(jen.Id("types").Dot("String"))),
			jen.If(jen.Op("!").Add(jen.Id("rightOK"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.If(jen.Len(jen.Id("i").Dot("tail")).Op("!=").Len(jen.Id("leftText")).Op("||").Qual("unsafe", "SliceData").Call(jen.Id("i").Dot("tail")).Op("!=").Qual("unsafe", "StringData").Call(jen.String().Call(jen.Id("leftText")))).Block(
				jen.List(jen.Id("i").Dot("tail")).Op("=").List(jen.Id("append").Call(jen.Id("make").Call(jen.Index().Add(jen.Id("byte")), jen.Lit(0), jen.Len(jen.Id("leftText")).Op("+").Add(jen.Len(jen.Id("rightText")))), jen.Id("leftText").Op("..."))),
			),
			jen.List(jen.Id("i").Dot("tail")).Op("=").List(jen.Id("append").Call(jen.Id("i").Dot("tail"), jen.Id("rightText").Op("..."))),
			jen.List(jen.Id("text")).Op(":=").List(jen.Id("types").Dot("String").Call(jen.Qual("unsafe", "String").Call(jen.Qual("unsafe", "SliceData").Call(jen.Id("i").Dot("tail")), jen.Len(jen.Id("i").Dot("tail"))))),
			jen.Id("i").Dot("release").Call(jen.Id("rightAddr")),
			jen.Id("i").Dot("release").Call(jen.Id("leftAddr")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("text")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"),
		)))
}

func stringEncodeUtf32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32")).Call(jen.Id("val"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringEq() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("==").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringGe() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op(">=").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringGt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op(">").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringIter() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("ref")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.If(jen.Id("ref").Dot("Kind").Call().Op("!=").Add(jen.Id("types").Dot("KindRef"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("ref").Dot("Ref").Call()),
			jen.List(jen.Id("val"), jen.Id("ok")).Op(":=").List(jen.Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Id("types").Dot("String"))),
			jen.If(jen.Op("!").Add(jen.Id("ok"))).Block(jen.Id("panic").Call(jen.Id("ErrTypeMismatch"))),
			jen.List(jen.Id("iter")).Op(":=").List(jen.Id("types").Dot("NewStringIterator").Call(jen.Id("types").Dot("Ref").Call(jen.Id("addr")), jen.Id("val"))),
			jen.Id("iter").Dot("Next").Call(),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("iter")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringLe() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("<=").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringLen() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringLt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("<").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringNe() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("<").Add(jen.Lit(2))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v1")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("v2")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("String")).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(2))))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI1").Call(jen.Id("v2").Op("!=").Add(jen.Id("v1")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func stringNewUtf32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("unboxRef").Index(jen.Id("types").Dot("TypedArray").Index(jen.Id("int32"))).Call(jen.Id("i"), jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("types").Dot("String").Call(jen.String().Call(jen.Id("val")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}
