package codegen

import (
	"github.com/dave/jennifer/jen"
)

func stringNewUTF32() jen.Code {
	return convertCall("text")
}

func stringLen() jen.Code {
	return handler(underflow(1),
		jen.Id("v").Op(":=").Id("unboxRef").Index(jen.Qual(typesPkg, "String")).Call(jen.Id("i"), top(1)),
		top(1).Op("=").Qual(typesPkg, "BoxI32").Call(jen.Id("int32").Call(jen.Id("len").Call(jen.Id("v")))),
		next())
}

func stringConcat() jen.Code {
	return handler(
		underflow(2),
		jen.List(jen.Id("right"), jen.Id("left")).Op(":=").List(top(1), top(2)),
		jen.Id("text").Op(":=").Id("i").Dot("concat").Call(jen.Id("left"), jen.Id("right")),
		jen.Id("i").Dot("release").Call(jen.Id("right").Dot("Ref").Call()),
		jen.Id("i").Dot("release").Call(jen.Id("left").Dot("Ref").Call()),
		jen.Id("i").Dot("sp").Op("--"),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("text"))),
		next(),
	)
}

// stringCompare lowers the string comparison rel. The operands pop right to
// left, so the left operand is the one under the top.
func stringCompare(rel string) jen.Code {
	return handler(underflow(2),
		jen.Id("v1").Op(":=").Id("unboxRef").Index(jen.Qual(typesPkg, "String")).Call(jen.Id("i"), top(1)),
		jen.Id("v2").Op(":=").Id("unboxRef").Index(jen.Qual(typesPkg, "String")).Call(jen.Id("i"), top(2)),
		jen.Id("i").Dot("sp").Op("--"),
		top(1).Op("=").Qual(typesPkg, "BoxI1").Call(jen.Id("v2").Op(rel).Id("v1")),
		next())
}

func stringEncodeUTF32() jen.Code {
	return convertCall("runes")
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

// convertCall pushes the Interpreter method method of its operand, releasing
// the operand first.
func convertCall(method string) jen.Code {
	return handler(underflow(1),
		jen.Id("val").Op(":=").Id("i").Dot(method).Call(top(1)),
		jen.Id("i").Dot("release").Call(top(1).Dot("Ref").Call()),
		top(1).Op("=").Qual(typesPkg, "BoxRef").Call(jen.Id("i").Dot("alloc").Call(jen.Id("val"))),
		next())
}
