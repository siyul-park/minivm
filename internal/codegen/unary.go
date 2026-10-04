package codegen

import (
	"math"
	"strconv"

	"github.com/dave/jennifer/jen"
)

// pipe is one stage of a unary opcode: the top operand flows through its
// stages and is boxed back over the same slot. The saturating float to integer
// conversions are not a pipeline and build their body in saturate.
type pipe func(jen.Code) jen.Code

// float32Math lowers fn of package math over an f32.
func float32Math(fn string) jen.Code {
	return convert(narrow("F32"), "F32", as("float64"), via("math", fn), as("float32"))
}

// float64Math lowers fn of package math over an f64.
func float64Math(fn string) jen.Code {
	return convert(narrow("F64"), "F64", via("math", fn))
}

// convert lowers a unary opcode that runs operand through stages in order and
// boxes the result as dst.
func convert(operand jen.Code, dst string, stages ...pipe) jen.Code {
	var out jen.Code = jen.Id("v")
	for _, stage := range stages {
		out = stage(out)
	}
	return unary(operand, boxed(dst, out))
}

// saturate lowers the conversion of src, F32 or F64, to the Go integer type
// dst: NaN converts to zero and an out-of-range value clamps.
func saturate(src, dst string) jen.Code {
	signed := dst[0] == 'i'
	bits := 32
	if dst[len(dst)-2:] == "64" {
		bits = 64
	}
	width := strconv.Itoa(bits)
	exponent := bits
	if signed {
		exponent--
	}
	limit := func() jen.Code { return jen.Lit(math.Ldexp(1, exponent)) }
	operand := func() *jen.Statement {
		if src == "F32" {
			return jen.Id("float64").Call(jen.Id("v"))
		}
		return jen.Id("v")
	}

	var cases []jen.Code
	if signed {
		cases = []jen.Code{
			jen.Case(jen.Qual("math", "IsNaN").Call(operand())).Block(jen.Id("result").Op("=").Lit(0)),
			jen.Case(operand().Op(">=").Add(limit())).Block(jen.Id("result").Op("=").Qual("math", "MaxInt"+width)),
			jen.Case(operand().Op("<").Add(jen.Op("-").Add(limit()))).Block(jen.Id("result").Op("=").Qual("math", "MinInt"+width)),
		}
	} else {
		cases = []jen.Code{
			jen.Case(jen.Qual("math", "IsNaN").Call(operand()).Op("||").Add(operand().Op("<").Lit(0))).Block(jen.Id("result").Op("=").Lit(0)),
			jen.Case(operand().Op(">=").Add(limit())).Block(jen.Id("result").Op("=").Qual("math", "MaxUint"+width)),
		}
	}
	cases = append(cases, jen.Default().Block(jen.Id("result").Op("=").Id(dst).Call(operand())))

	var result jen.Code = jen.Id("result")
	if !signed {
		result = jen.Id("int" + width).Call(result)
	}
	kind := "I32"
	if bits == 64 {
		kind = "I64"
	}
	return handler(underflow(1),
		jen.Id("v").Op(":=").Add(narrow(src)),
		jen.Var().Id("result").Id(dst),
		jen.Switch().Block(cases...),
		top(1).Op("=").Add(boxed(kind, result)),
		next(),
	)
}

func unary(operand, store jen.Code) jen.Code {
	return handler(underflow(1),
		jen.Id("v").Op(":=").Add(operand),
		top(1).Op("=").Add(store),
		next(),
	)
}

// narrow reads the top operand through the types.Boxed accessor read.
func narrow(read string) jen.Code {
	return top(1).Dot(read).Call()
}

// wide reads the top operand as an i64, consuming a heap-boxed one.
func wide() jen.Code {
	return jen.Id("i").Dot("unboxI64").Call(top(1))
}

// boxed is x as the stack word of kind: an i64 may need the heap.
func boxed(kind string, x jen.Code) jen.Code {
	if kind == "I64" {
		return jen.Id("i").Dot("boxI64").Call(x)
	}
	return jen.Qual(typesPkg, "Box"+kind).Call(x)
}

// as converts to the Go type typ.
func as(typ string) pipe {
	return func(x jen.Code) jen.Code { return jen.Id(typ).Call(x) }
}

// via calls fn of package pkg.
func via(pkg, fn string) pipe {
	return func(x jen.Code) jen.Code { return jen.Qual(pkg, fn).Call(x) }
}

// neg negates.
func neg(x jen.Code) jen.Code {
	return jen.Op("-").Add(x)
}
