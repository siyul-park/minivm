package codegen

import (
	"fmt"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
)

func threaderType(file *jen.File) {
	file.Type().Id("threader").Struct(
		jen.Id("types").Index().Qual(typesPkg, "Type"),
		jen.Id("constants").Index().Qual(typesPkg, "Boxed"),
		jen.Id("heap").Index().Qual(typesPkg, "Value"),
		jen.Id("coros").Index().Bool(),
		jen.Id("locals").Index().Qual(typesPkg, "Kind"),
		jen.Id("localTypes").Index().Qual(typesPkg, "Type"),
		jen.Id("globals").Index().Qual(typesPkg, "Kind"),
		jen.Id("globalTypes").Index().Qual(typesPkg, "Type"),
		jen.Id("captures").Index().Qual(typesPkg, "Kind"),
		jen.Id("captureTypes").Index().Qual(typesPkg, "Type"),
		jen.Id("code").Index().Byte(),
		jen.Id("ip").Int(),
		jen.Id("exact").Bool(),
	)
}

func fusionTable(patterns []pattern) (jen.Code, error) {
	var groups [256][]pattern
	for _, pattern := range patterns {
		groups[pattern[0].op] = append(groups[pattern[0].op], pattern)
	}

	var entries []jen.Code
	for opcode, patterns := range groups {
		if len(patterns) == 0 {
			continue
		}
		body := []jen.Code{jen.Id("start").Op(":=").Id("c").Dot("ip")}
		for index, pattern := range patterns {
			label := fmt.Sprintf("l%d", index)
			size := pattern.width()
			statements, err := compose(pattern, size, label)
			if err != nil {
				return nil, err
			}
			if pattern[len(pattern)-1].op == instr.BR_IF {
				offset := size - width(instr.BR_IF) + 1
				statements = append([]jen.Code{jen.Id("offset").Op(":=").Qual(instrPkg, "ParseI16").Call(jen.Id("c").Dot("code"), jen.Id("start").Op("+").Lit(offset))}, statements...)
			}
			body = append(body, jen.If(jen.Op("!").Parens(matchGuard(pattern))).Block(jen.Goto().Id(label)))
			body = append(body, jen.Block(statements...))
			body = append(body, jen.Id(label).Op(":"))
		}
		body = append(body, jen.Return(jen.Nil()))
		entries = append(entries, jen.Qual(instrPkg, symbol(instr.Opcode(opcode))).Op(":").Func().Params(jen.Id("c").Op("*").Id("threader")).Func().Params(jen.Op("*").Id("Interpreter")).Block(body...))
	}

	table := jen.Index(jen.Lit(256)).Func().Params(jen.Id("c").Op("*").Id("threader")).Func().Params(jen.Op("*").Id("Interpreter"))
	return table.ValuesFunc(func(group *jen.Group) {
		for _, entry := range entries {
			group.Line().Add(entry)
		}
	}), nil
}

func matchGuard(pattern pattern) jen.Code {
	size := pattern.width()
	condition := jen.Id("start").Op("+").Lit(size).Op("<=").Len(jen.Id("c").Dot("code"))
	offset := width(pattern[0].op)
	for _, current := range pattern[1:] {
		condition = condition.Op("&&").Qual(instrPkg, "Opcode").Call(jen.Id("c").Dot("code").Index(add(jen.Id("start"), offset))).Op("==").Qual(instrPkg, symbol(current.op))
		offset += width(current.op)
	}
	return condition
}

func handlerTable() jen.Code {
	table := jen.Index(jen.Lit(256)).Func().Params(jen.Id("c").Op("*").Id("threader")).Func().Params(jen.Id("i").Op("*").Id("Interpreter"))
	return table.ValuesFunc(func(group *jen.Group) {
		for opcode, emit := range lowerers {
			if emit == nil {
				continue
			}
			op := instr.Opcode(opcode)
			group.Line().Qual(instrPkg, symbol(op)).Op(":").Add(lower(op))
		}
	})
}

func fallbackInit(file *jen.File) {
	file.Func().Id("init").Params().Block(
		jen.For(jen.List(jen.Id("i"), jen.Id("fn")).Op(":=").Range().Id("threaded")).Block(
			jen.If(jen.Id("fn").Op("==").Nil()).Block(
				jen.Id("threaded").Index(jen.Id("i")).Op("=").Func().Params(jen.Id("c").Op("*").Id("threader")).Func().Params(jen.Op("*").Id("Interpreter")).Block(
					jen.Id("inst").Op(":=").Qual(instrPkg, "Instruction").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op(":"))),
					jen.Id("c").Dot("ip").Op("+=").Id("inst").Dot("Width").Call(),
					jen.Return(jen.Id("invalid")),
				),
			),
		),
	)
	file.Line()
}

func compileMethod(file *jen.File) {
	file.Func().Params(jen.Id("c").Op("*").Id("threader")).Id("Compile").Params(
		jen.Id("code").Index().Byte(),
		jen.Id("locals").Index().Qual(typesPkg, "Kind"),
		jen.Id("localTypes").Index().Qual(typesPkg, "Type"),
		jen.Id("captures").Index().Qual(typesPkg, "Kind"),
		jen.Id("captureTypes").Index().Qual(typesPkg, "Type"),
	).Index().Func().Params(jen.Op("*").Id("Interpreter")).Block(
		jen.Id("c").Dot("code").Op("=").Id("code"),
		jen.Id("c").Dot("locals").Op("=").Id("locals"),
		jen.Id("c").Dot("localTypes").Op("=").Id("localTypes"),
		jen.Id("c").Dot("captures").Op("=").Id("captures"),
		jen.Id("c").Dot("captureTypes").Op("=").Id("captureTypes"),
		jen.Id("c").Dot("ip").Op("=").Lit(0),
		jen.Id("compiled").Op(":=").Make(jen.Index().Func().Params(jen.Op("*").Id("Interpreter")), jen.Len(jen.Id("code"))),
		jen.For(jen.Id("c").Dot("ip").Op("<").Len(jen.Id("code"))).Block(
			jen.Id("ip").Op(":=").Id("c").Dot("ip"),
			jen.If(jen.Op("!").Id("c").Dot("exact")).Block(
				jen.If(jen.Id("compile").Op(":=").Id("fusions").Index(jen.Id("code").Index(jen.Id("ip"))), jen.Id("compile").Op("!=").Nil()).Block(
					jen.If(jen.Id("fn").Op(":=").Id("compile").Call(jen.Id("c")), jen.Id("fn").Op("!=").Nil()).Block(
						jen.Id("compiled").Index(jen.Id("ip")).Op("=").Id("fn"),
						jen.Continue(),
					),
				),
			),
			jen.Id("compiled").Index(jen.Id("ip")).Op("=").Id("threaded").Index(jen.Id("code").Index(jen.Id("ip"))).Call(jen.Id("c")),
		),
		jen.For(jen.Id("ip").Op(":=").Lit(0), jen.Id("ip").Op("<").Len(jen.Id("code")), jen.Id("ip").Op("++")).Block(
			jen.If(jen.Id("compiled").Index(jen.Id("ip")).Op("==").Nil()).Block(
				jen.Id("compiled").Index(jen.Id("ip")).Op("=").Id("invalid"),
			),
		),
		jen.Return(jen.Id("compiled")),
	)
}

func threaderMethods(file *jen.File) {
	for _, op := range []instr.Opcode{instr.LOCAL_GET, instr.GLOBAL_GET, instr.UPVAL_GET} {
		slotMethod(file, op)
		file.Line()
	}
	constantMethod(file)
	file.Line()
}

func slotMethod(file *jen.File, op instr.Opcode) {
	field, method, _ := slotInfo(op)
	at := jen.Int().Call(jen.Id("c").Dot("code").Index(jen.Id("at")))
	if op == instr.GLOBAL_GET {
		at = jen.Qual(instrPkg, "ParseU16").Call(jen.Id("c").Dot("code"), jen.Id("at"))
	}
	file.Func().Params(jen.Id("c").Op("*").Id("threader")).Id(method).Params(
		jen.Id("at").Int(),
		jen.Id("kind").Qual(typesPkg, "Kind"),
	).Params(jen.Int(), jen.Bool()).Block(
		jen.Id("idx").Op(":=").Add(at),
		jen.If(
			jen.Id("idx").Op(">=").Len(jen.Id("c").Dot(field)).Op("||").
				Id("c").Dot(field).Index(jen.Id("idx")).Dot("Repr").Call().Op("!=").Id("kind"),
		).Block(jen.Return(jen.Lit(0), jen.False())),
		jen.Return(jen.Id("idx"), jen.True()),
	)
}

func constantMethod(file *jen.File) {
	file.Func().Params(jen.Id("c").Op("*").Id("threader")).Id("constant").Params(
		jen.Id("at").Int(),
		jen.Id("kind").Qual(typesPkg, "Kind"),
	).Params(
		jen.Qual(typesPkg, "Boxed"),
		jen.Bool(),
	).Block(
		jen.Id("idx").Op(":=").Qual(instrPkg, "ParseU16").Call(jen.Id("c").Dot("code"), jen.Id("at")),
		jen.If(jen.Id("idx").Op(">=").Len(jen.Id("c").Dot("constants"))).Block(
			jen.Return(jen.Qual(typesPkg, "Boxed").Call(jen.Lit(0)), jen.False()),
		),
		jen.Id("boxed").Op(":=").Id("c").Dot("constants").Index(jen.Id("idx")),
		jen.If(jen.Id("kind").Op("==").Qual(typesPkg, "KindI64")).Block(
			jen.Switch(jen.Id("boxed").Dot("Kind").Call()).Block(
				jen.Case(jen.Qual(typesPkg, "KindI64")),
				jen.Case(jen.Qual(typesPkg, "KindRef")).Block(
					jen.Id("ref").Op(":=").Id("boxed").Dot("Ref").Call(),
					jen.If(jen.Id("ref").Op("<").Lit(0).Op("||").Id("ref").Op(">=").Len(jen.Id("c").Dot("heap"))).Block(
						jen.Return(jen.Qual(typesPkg, "Boxed").Call(jen.Lit(0)), jen.False()),
					),
					jen.If(jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id("ref")).Assert(jen.Qual(typesPkg, "I64")), jen.Op("!").Id("ok")).Block(
						jen.Return(jen.Qual(typesPkg, "Boxed").Call(jen.Lit(0)), jen.False()),
					),
				),
				jen.Default().Block(
					jen.Return(jen.Qual(typesPkg, "Boxed").Call(jen.Lit(0)), jen.False()),
				),
			),
		).Else().If(jen.Id("boxed").Dot("Kind").Call().Op("!=").Id("kind")).Block(
			jen.Return(jen.Qual(typesPkg, "Boxed").Call(jen.Lit(0)), jen.False()),
		),
		jen.Return(jen.Id("boxed"), jen.True()),
	)
}

func symbol(op instr.Opcode) string {
	return strings.ToUpper(strings.ReplaceAll(instr.TypeOf(op).Mnemonic, ".", "_"))
}
