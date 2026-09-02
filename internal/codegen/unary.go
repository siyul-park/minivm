package codegen

import (
	"github.com/dave/jennifer/jen"
)

func f32Abs() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Abs").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Ceil() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Ceil").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Floor() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Floor").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Nearest() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "RoundToEven").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Neg() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Op("-").Add(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ReinterpretI32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Qual("math", "Float32frombits").Call(jen.Id("uint32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Sqrt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Sqrt").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToF64() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToI32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("int32")),
				jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("float64").Call(jen.Id("v")))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
					jen.Case(jen.Id("float64").Call(jen.Id("v")).Op(">=").Add(jen.Lit(2147483648.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxInt32"))),
					jen.Case(jen.Id("float64").Call(jen.Id("v")).Op("<").Add(jen.Op("-").Add(jen.Lit(2147483648.0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MinInt32"))),
					jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("int32").Call(jen.Id("float64").Call(jen.Id("v")))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("result")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToI32U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("uint32")),
				jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("float64").Call(jen.Id("v"))).Op("||").Add(jen.Id("float64").Call(jen.Id("v")).Op("<").Add(jen.Lit(0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
					jen.Case(jen.Id("float64").Call(jen.Id("v")).Op(">=").Add(jen.Lit(4294967296.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxUint32"))),
					jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("uint32").Call(jen.Id("float64").Call(jen.Id("v")))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("result"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToI64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("int64")),
			jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("float64").Call(jen.Id("v")))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
				jen.Case(jen.Id("float64").Call(jen.Id("v")).Op(">=").Add(jen.Lit(9223372036854775808.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxInt64"))),
				jen.Case(jen.Id("float64").Call(jen.Id("v")).Op("<").Add(jen.Op("-").Add(jen.Lit(9223372036854775808.0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MinInt64"))),
				jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("int64").Call(jen.Id("v"))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("result"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32ToI64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("uint64")),
			jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("float64").Call(jen.Id("v"))).Op("||").Add(jen.Id("float64").Call(jen.Id("v")).Op("<").Add(jen.Lit(0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
				jen.Case(jen.Id("float64").Call(jen.Id("v")).Op(">=").Add(jen.Lit(18446744073709551616.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxUint64"))),
				jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("uint64").Call(jen.Id("v"))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("result")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f32Trunc() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Qual("math", "Trunc").Call(jen.Id("float64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Abs() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Abs").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Ceil() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Ceil").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Floor() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Floor").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Nearest() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "RoundToEven").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Neg() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Op("-").Add(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ReinterpretI64() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Float64frombits").Call(jen.Id("uint64").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Sqrt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Sqrt").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToF32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToI32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("int32")),
				jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("v"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
					jen.Case(jen.Id("v").Op(">=").Add(jen.Lit(2147483648.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxInt32"))),
					jen.Case(jen.Id("v").Op("<").Add(jen.Op("-").Add(jen.Lit(2147483648.0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MinInt32"))),
					jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("int32").Call(jen.Id("v"))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("result")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToI32U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("uint32")),
				jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("v")).Op("||").Add(jen.Id("v").Op("<").Add(jen.Lit(0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
					jen.Case(jen.Id("v").Op(">=").Add(jen.Lit(4294967296.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxUint32"))),
					jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("uint32").Call(jen.Id("v"))))),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("result"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToI64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("int64")),
			jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("v"))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
				jen.Case(jen.Id("v").Op(">=").Add(jen.Lit(9223372036854775808.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxInt64"))),
				jen.Case(jen.Id("v").Op("<").Add(jen.Op("-").Add(jen.Lit(9223372036854775808.0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MinInt64"))),
				jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("int64").Call(jen.Id("v"))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("result"))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64ToI64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Var().Add(jen.List(jen.Id("result"))).Add(jen.Id("uint64")),
			jen.Switch().Block(jen.Case(jen.Qual("math", "IsNaN").Call(jen.Id("v")).Op("||").Add(jen.Id("v").Op("<").Add(jen.Lit(0)))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Lit(0))),
				jen.Case(jen.Id("v").Op(">=").Add(jen.Lit(18446744073709551616.0))).Block(jen.List(jen.Id("result")).Op("=").List(jen.Qual("math", "MaxUint64"))),
				jen.Default().Block(jen.List(jen.Id("result")).Op("=").List(jen.Id("uint64").Call(jen.Id("v"))))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("result")))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func f64Trunc() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Qual("math", "Trunc").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Clz() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Qual("math/bits", "LeadingZeros32").Call(jen.Id("uint32").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Ctz() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Qual("math/bits", "TrailingZeros32").Call(jen.Id("uint32").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Extend16S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("int16").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Extend8S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("int8").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32Popcnt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Qual("math/bits", "OnesCount32").Call(jen.Id("uint32").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ReinterpretF32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Qual("math", "Float32bits").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToF32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToF32U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("uint32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToF64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToF64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("uint32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToI64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i32ToI64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("I32").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("uint32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Clz() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Qual("math/bits", "LeadingZeros64").Call(jen.Id("uint64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Ctz() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Qual("math/bits", "TrailingZeros64").Call(jen.Id("uint64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Extend16S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("int16").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Extend32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("int32").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Extend8S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Id("int8").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64Popcnt() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Qual("math/bits", "OnesCount64").Call(jen.Id("uint64").Call(jen.Id("v"))))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ReinterpretF64() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))).Dot("F64").Call()),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("i").Dot("boxI64").Call(jen.Id("int64").Call(jen.Qual("math", "Float64bits").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToF32S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToF32U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF32").Call(jen.Id("float32").Call(jen.Id("uint64").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToF64S() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToF64U() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxF64").Call(jen.Id("float64").Call(jen.Id("uint64").Call(jen.Id("v")))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}

func i64ToI32() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.Id("c").Dot("ip").Op("++"),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("v")).Op(":=").List(jen.Id("i").Dot("unboxI64").Call(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1))))),
			jen.Block(jen.List(jen.Id("v")).Op(":=").List(jen.Id("v")),
				jen.List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))).Op("=").List(jen.Id("types").Dot("BoxI32").Call(jen.Id("int32").Call(jen.Id("v"))))),
			jen.Id("i").Dot("fr").Dot("ip").Op("++"))))
}
