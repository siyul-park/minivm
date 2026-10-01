package codegen

import (
	"github.com/dave/jennifer/jen"
)

// stringConcat reuses the left buffer only when its published text reaches the
// buffer end. Published prefixes are immutable; otherwise it copies to a new buffer.
func stringConcat() jen.Code {
	return handler(
		underflow(2),
		jen.List(jen.Id("right"), jen.Id("left")).Op(":=").List(top(1), top(2)),
		jen.If(jen.Id("left").Dot("Kind").Call().Op("!=").Qual(typesPkg, "KindRef").Op("||").Id("right").Dot("Kind").Call().Op("!=").Qual(typesPkg, "KindRef")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.List(jen.Id("leftAddr"), jen.Id("rightAddr")).Op(":=").List(jen.Id("left").Dot("Ref").Call(), jen.Id("right").Dot("Ref").Call()),
		jen.List(jen.Id("leftText"), jen.Id("leftOK")).Op(":=").Id("i").Dot("heap").Index(jen.Id("leftAddr")).Assert(jen.Qual(typesPkg, "String")),
		jen.If(jen.Op("!").Id("leftOK")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.List(jen.Id("rightText"), jen.Id("rightOK")).Op(":=").Id("i").Dot("heap").Index(jen.Id("rightAddr")).Assert(jen.Qual(typesPkg, "String")),
		jen.If(jen.Op("!").Id("rightOK")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.If(jen.Len(jen.Id("i").Dot("tail")).Op("!=").Len(jen.Id("leftText")).Op("||").Qual("unsafe", "SliceData").Call(jen.Id("i").Dot("tail")).Op("!=").Qual("unsafe", "StringData").Call(jen.String().Call(jen.Id("leftText")))).Block(
			jen.Id("i").Dot("tail").Op("=").Id("append").Call(jen.Id("make").Call(jen.Index().Id("byte"), jen.Lit(0), jen.Len(jen.Id("leftText")).Op("+").Add(jen.Len(jen.Id("rightText")))), jen.Id("leftText").Op("...")),
		),
		jen.Id("i").Dot("tail").Op("=").Id("append").Call(jen.Id("i").Dot("tail"), jen.Id("rightText").Op("...")),
		jen.Id("text").Op(":=").Qual(typesPkg, "String").Call(jen.Qual("unsafe", "String").Call(jen.Qual("unsafe", "SliceData").Call(jen.Id("i").Dot("tail")), jen.Len(jen.Id("i").Dot("tail")))),
		jen.Id("i").Dot("release").Call(jen.Id("rightAddr")),
		jen.Id("i").Dot("release").Call(jen.Id("leftAddr")),
		jen.Id("i").Dot("sp").Op("--"),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("text"))),
		next(),
	)
}

// stringCompare lowers the string comparison op. The operands pop right to
// left, so the left operand is the one under the top.
func stringCompare(op string) jen.Code {
	return handler(underflow(2),
		jen.Id("v1").Op(":=").Id("unboxRef").Index(jen.Qual(typesPkg, "String")).Call(jen.Id("i"), top(1)),
		jen.Id("v2").Op(":=").Id("unboxRef").Index(jen.Qual(typesPkg, "String")).Call(jen.Id("i"), top(2)),
		jen.Id("i").Dot("sp").Op("--"),
		top(1).Op("=").Qual(typesPkg, "BoxI1").Call(jen.Id("v2").Op(op).Id("v1")),
		next())
}

func stringEncodeUtf32() jen.Code {
	return handler(underflow(1),
		jen.Id("val").Op(":=").Id("unboxRef").Index(jen.Qual(typesPkg, "String")).Call(jen.Id("i"), top(1)),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Qual(typesPkg, "TypedArray").Index(jen.Id("int32")).Call(jen.Id("val")))),
		next())
}

func stringIter() jen.Code {
	return handler(underflow(1),
		container(1),
		jen.List(jen.Id("val"), jen.Id("ok")).Op(":=").Id("i").Dot("heap").Index(jen.Id("addr")).Assert(jen.Qual(typesPkg, "String")),
		jen.If(jen.Op("!").Id("ok")).Block(jen.Panic(jen.Id("ErrTypeMismatch"))),
		jen.Id("iter").Op(":=").Qual(typesPkg, "NewStringIterator").Call(jen.Qual(typesPkg, "Ref").Call(jen.Id("addr")), jen.Id("val")),
		jen.Id("iter").Dot("Next").Call(),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("iter"))),
		next())
}

func stringLen() jen.Code {
	return handler(underflow(1),
		jen.Id("v").Op(":=").Id("unboxRef").Index(jen.Qual(typesPkg, "String")).Call(jen.Id("i"), top(1)),
		top(1).Op("=").Qual(typesPkg, "BoxI32").Call(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("v")))),
		next())
}

func stringNewUtf32() jen.Code {
	return handler(underflow(1),
		jen.Id("val").Op(":=").Id("unboxRef").Index(jen.Qual(typesPkg, "TypedArray").Index(jen.Id("int32"))).Call(jen.Id("i"), top(1)),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Qual(typesPkg, "String").Call(jen.String().Call(jen.Id("val"))))),
		next())
}
