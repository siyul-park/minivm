package codegen

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type loader struct {
	slot       int
	width      int
	raw        string
	boxed      string
	index      string
	addr       string
	pos        jen.Code
	label      string
	standalone bool
}

func newLoader(op instr.Opcode, slot, offset int, label string, standalone bool) loader {
	name := temp(slot)
	at := add(jen.Id("start"), offset)
	if standalone {
		at = jen.Id("c").Dot("ip")
	}
	return loader{
		slot:  slot,
		width: width(op),
		raw:   name,
		boxed: boxedTemp(name),
		index: fmt.Sprintf("i%d", slot),
		// addr names a runtime local-slot address temp declared inside the
		// closure body (LOCAL_GET only; see (loader).read). It uses a
		// distinct prefix from index so it can never shadow another fused
		// producer's compile-time index variable "iN" regardless of how
		// many producers a pattern fuses; index and addr previously shared
		// the "i" prefix at a fixed +2 offset, which collided once
		// array.set's three-producer container fusion put a LOCAL_GET
		// container at slot 0 (addr "i2") alongside a GLOBAL_GET/UPVAL_GET
		// producer at slot 2 (index "i2").
		addr:       fmt.Sprintf("a%d", slot),
		pos:        at,
		label:      label,
		standalone: standalone,
	}
}

func (l loader) decode(result *value, op instr.Opcode) {
	if !l.standalone {
		return
	}
	switch op {
	case instr.LOCAL_GET, instr.UPVAL_GET:
		result.compile = append(result.compile,
			jen.Id(l.index).Op(":=").Int().Call(jen.Id("c").Dot("code").Index(jen.Add(l.pos).Op("+").Lit(1))),
		)
	case instr.GLOBAL_GET, instr.CONST_GET:
		result.compile = append(result.compile,
			jen.Id(l.index).Op(":=").Int().Call(
				jen.Op("*").Parens(jen.Op("*").Uint16()).Call(
					jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Add(l.pos).Op("+").Lit(1)))),
				),
			),
		)
	}
}

func (l loader) read(result *value, current step) error {
	field, _, ok := slotInfo(current.op)
	if !ok {
		return fmt.Errorf("unsupported slot opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	if current.op == instr.LOCAL_GET || !l.standalone {
		guard, err := l.bounds(current, field)
		if err != nil {
			return err
		}
		result.compile = append(result.compile, guard...)
	}
	// l.read is called only for LOCAL_GET, GLOBAL_GET, and UPVAL_GET (see
	// load's switch), so current.typ != nil alone identifies a container
	// guard; typedContainer/structContainer are the only pattern builders
	// that set it for these three opcodes.
	if current.typ != nil {
		if current.typ == reflect.TypeFor[types.Struct]() {
			if err := l.structGuard(result, current); err != nil {
				return err
			}
		} else if err := l.arrayGuard(result, current); err != nil {
			return err
		}
	}
	switch current.op {
	case instr.LOCAL_GET:
		if l.standalone {
			result.check = append(result.check,
				jen.Id(l.addr).Op(":=").Id("i").Dot("fr").Dot("bp").Op("+").Id(l.index),
				jen.If(jen.Id(l.addr).Op(">=").Id("i").Dot("sp")).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
			)
		} else {
			result.check = append(result.check,
				jen.If(jen.Id("i").Dot("fr").Dot("bp").Op("+").Id(l.index).Op(">=").Id("i").Dot("sp")).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
			)
			result.body = append(result.body, jen.Id(l.addr).Op(":=").Id("i").Dot("fr").Dot("bp").Op("+").Id(l.index))
		}
		result.body = append(result.body, jen.Id(l.boxed).Op(":=").Id("i").Dot("stack").Index(jen.Id(l.addr)))
	case instr.GLOBAL_GET:
		result.check = append(result.check,
			jen.If(jen.Id(l.index).Op(">=").Len(jen.Id("i").Dot("globals"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
		)
		result.body = append(result.body, jen.Id(l.boxed).Op(":=").Id("i").Dot("globals").Index(jen.Id(l.index)))
	case instr.UPVAL_GET:
		result.check = append(result.check,
			jen.If(jen.Id(l.index).Op(">=").Len(jen.Id("i").Dot("fr").Dot("upvals"))).Block(jen.Panic(jen.Id("ErrSegmentationFault"))),
		)
		result.body = append(result.body, jen.Id(l.boxed).Op(":=").Id("i").Dot("fr").Dot("upvals").Index(jen.Id(l.index)))
	}
	return nil
}

func (l loader) bounds(current step, field string) ([]jen.Code, error) {
	if !l.standalone {
		return l.slotGuard(current)
	}
	condition := jen.Id(l.index).Op(">=").Len(jen.Id("c").Dot(field))
	body := []jen.Code{
		jen.Return(jen.Func().Params(jen.Op("*").Id("Interpreter")).Block(jen.Panic(jen.Id("ErrSegmentationFault")))),
	}
	return []jen.Code{jen.If(condition).Block(body...)}, nil
}

func (l loader) slotGuard(current step) ([]jen.Code, error) {
	name, ok := kindName(current.kind)
	if !ok {
		return nil, fmt.Errorf("unsupported source kind %s", current.kind)
	}
	_, method, ok := slotInfo(current.op)
	if !ok {
		return nil, fmt.Errorf("unsupported slot opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	expected := jen.Qual("github.com/siyul-park/minivm/types", "Kind"+name)
	okName := fmt.Sprintf("ok%d", l.slot)
	return []jen.Code{
		jen.List(jen.Id(l.index), jen.Id(okName)).Op(":=").Id("c").Dot(method).Call(jen.Add(l.pos).Op("+").Lit(1), expected),
		jen.If(jen.Op("!").Id(okName)).Block(reject(l.label)),
	}, nil
}

func (l loader) constant(result *value, current step) error {
	if !l.standalone {
		return l.constantGuard(result, current)
	}
	condition := jen.Id(l.index).Op(">=").Len(jen.Id("c").Dot("constants"))
	result.compile = append(result.compile,
		jen.If(condition).Block(
			jen.Return(jen.Func().Params(jen.Op("*").Id("Interpreter")).Block(jen.Panic(jen.Id("ErrSegmentationFault")))),
		),
		jen.Id(l.boxed).Op(":=").Id("c").Dot("constants").Index(jen.Id(l.index)),
	)
	return nil
}

func (l loader) constantGuard(result *value, current step) error {
	name, ok := kindName(current.kind)
	if !ok {
		return fmt.Errorf("unsupported source kind %s", current.kind)
	}
	expected := jen.Qual("github.com/siyul-park/minivm/types", "Kind"+name)
	okName := fmt.Sprintf("ok%d", l.slot)
	result.compile = append(result.compile,
		jen.List(jen.Id(l.boxed), jen.Id(okName)).Op(":=").Id("c").Dot("constant").Call(jen.Add(l.pos).Op("+").Lit(1), expected),
		jen.If(jen.Op("!").Id(okName)).Block(reject(l.label)),
	)
	if name != "Ref" || current.typ == nil {
		return nil
	}
	guardName := current.typ.Name()
	if guardName == "" {
		return fmt.Errorf("unsupported constant guard %s", current.typ)
	}
	ref := fmt.Sprintf("c%d", l.slot)
	result.compile = append(result.compile,
		jen.Id(ref).Op(":=").Id(l.boxed).Dot("Ref").Call(),
		jen.If(jen.Id(ref).Op("<").Lit(0).Op("||").Id(ref).Op(">=").Len(jen.Id("c").Dot("heap"))).Block(reject(l.label)),
	)
	guard := jen.List(jen.Id("_"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id(ref)).Assert(jen.Qual("github.com/siyul-park/minivm/types", guardName))
	if current.exclude {
		result.compile = append(result.compile, jen.If(guard, jen.Id("ok")).Block(reject(l.label)))
	} else {
		result.compile = append(result.compile, guard, jen.If(jen.Op("!").Id("ok")).Block(reject(l.label)))
		result.object = jen.Id(ref)
		result.typ = current.typ
	}
	return nil
}

// arrayGuard proves, once at threading time, that the local, global, or
// upvalue slot l.index addresses is declared as the concrete typed-array
// element kind current.typ names. A miss rejects the fusion attempt so the
// container's runtime value keeps its own type check in the standalone
// ARRAY_GET handler; it never assumes the declared type from a narrower
// Kind, because ArrayType.Kind is KindRef for every element kind alike.
func (l loader) arrayGuard(result *value, current step) error {
	kind, ok := arrayKind(current.typ)
	if !ok {
		return fmt.Errorf("unsupported array element type %s", current.typ)
	}
	name, ok := fieldKindName(kind)
	if !ok {
		return fmt.Errorf("unsupported array element kind %s", kind)
	}
	field, ok := declaredTypesField(current.op)
	if !ok {
		return fmt.Errorf("unsupported array container opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	expected := jen.Qual("github.com/siyul-park/minivm/types", "Kind"+name)
	declared := fmt.Sprintf("d%d", l.slot)
	okName := fmt.Sprintf("dok%d", l.slot)
	result.compile = append(result.compile,
		jen.List(jen.Id(declared), jen.Id(okName)).Op(":=").Id("c").Dot(field).Index(jen.Id(l.index)).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "ArrayType")),
		jen.If(jen.Op("!").Id(okName).Op("||").Id(declared).Dot("ElemKind").Op("!=").Add(expected)).Block(reject(l.label)),
	)
	result.typ = current.typ
	return nil
}

// structGuard proves, once at threading time, that the local, global, or
// upvalue slot l.index addresses is declared as a concrete *types.StructType,
// and records that declared type so a fused STRUCT_GET consumer can resolve
// each accessed field's static Kind without re-deriving it from the runtime
// heap value. A miss rejects the fusion attempt so the container's runtime
// value keeps its own type check in the standalone STRUCT_GET handler; it
// never assumes the runtime struct shares the declared type's field shape.
func (l loader) structGuard(result *value, current step) error {
	field, ok := declaredTypesField(current.op)
	if !ok {
		return fmt.Errorf("unsupported struct container opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	declared := fmt.Sprintf("d%d", l.slot)
	okName := fmt.Sprintf("dok%d", l.slot)
	result.compile = append(result.compile,
		jen.List(jen.Id(declared), jen.Id(okName)).Op(":=").Id("c").Dot(field).Index(jen.Id(l.index)).Assert(jen.Op("*").Qual("github.com/siyul-park/minivm/types", "StructType")),
		jen.If(jen.Op("!").Id(okName)).Block(reject(l.label)),
	)
	result.declared = jen.Id(declared)
	return nil
}

func (l loader) literal(result *value, current step) error {
	if _, ok := kindName(current.kind); !ok {
		return fmt.Errorf("unsupported source kind %s", current.kind)
	}
	if current.boxed {
		result.boxed = jen.Id(l.boxed)
		result.compile = append(result.compile,
			jen.Id(l.boxed).Op(":=").Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(immediate(current.kind, l.pos)),
		)
		return nil
	}
	result.raw = jen.Id(l.raw)
	result.compile = append(result.compile, jen.Id(l.raw).Op(":=").Add(immediate(current.kind, l.pos)))
	switch current.op {
	case instr.I32_CONST:
		result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxI32").Call(jen.Id(l.raw))
	case instr.I64_CONST:
		result.boxed = jen.Id("i").Dot("boxI64").Call(jen.Id(l.raw))
	case instr.F32_CONST:
		result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxF32").Call(jen.Id(l.raw))
	case instr.F64_CONST:
		result.boxed = jen.Qual("github.com/siyul-park/minivm/types", "BoxF64").Call(jen.Id(l.raw))
	}
	return nil
}

func (l loader) finish(result value, current step, indexed bool) (value, error) {
	if current.kind != instr.KindAny && !current.boxed && !current.commit && result.raw == nil {
		result.raw = jen.Id(l.raw)
		result.body = append(result.body, jen.Id(l.raw).Op(":=").Add(borrow(current.kind, result.boxed)))
	}

	if indexed {
		retain := current.kind == instr.KindAny || current.kind.Repr() == instr.KindI64 || current.kind.Repr() == instr.KindRef
		result.push = materialize(result, retain, l.width)
		return result, nil
	}
	if result.room {
		result.push = append(result.push, overflow())
	}
	result.push = append(result.push, result.check...)
	result.push = append(result.push, result.body...)
	if current.op == instr.CONST_GET && current.kind == instr.KindAny {
		result.push = append(result.push,
			jen.If(jen.Id(l.boxed).Dot("Kind").Call().Op("==").Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(
				jen.Id("addr").Op(":=").Id(l.boxed).Dot("Ref").Call(),
				jen.If(jen.List(jen.Id("str"), jen.Id("ok")).Op(":=").Id("c").Dot("heap").Index(jen.Id("addr")).Assert(jen.Qual("github.com/siyul-park/minivm/types", "String")), jen.Id("ok")).Block(
					jen.Id(l.boxed).Op("=").Qual("github.com/siyul-park/minivm/types", "BoxRef").Call(jen.Int().Call(jen.Id("i").Dot("intern").Call(jen.String().Call(jen.Id("str"))))),
				).Else().Block(jen.Id("i").Dot("retain").Call(jen.Id("addr"))),
			),
		)
	} else if current.op == instr.CONST_GET {
		result.push = append(result.push, jen.Id("i").Dot("retainBox").Call(result.boxed))
	}
	result.push = append(result.push,
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(result.boxed),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(l.width),
	)
	return result, nil
}

func load(current step, slot, offset int, label string, standalone bool) (value, error) {
	loader := newLoader(current.op, slot, offset, label, standalone)
	result := value{op: current.op, head: current.op, boxed: jen.Id(loader.boxed)}
	// A source only needs stack room when it pushes on its own. Fused into a
	// consumer it stays in a temporary, and the consumer checks the room its
	// own net push needs.
	result.room = true

	_, _, indexed := slotInfo(current.op)
	loader.decode(&result, current.op)
	if standalone && (indexed || current.op == instr.CONST_GET) {
		result.compile = append(result.compile, jen.Id("c").Dot("ip").Op("+=").Lit(loader.width))
	}

	var err error
	switch current.op {
	case instr.LOCAL_GET, instr.GLOBAL_GET, instr.UPVAL_GET:
		err = loader.read(&result, current)
	case instr.CONST_GET:
		err = loader.constant(&result, current)
	case instr.I32_CONST, instr.I64_CONST, instr.F32_CONST, instr.F64_CONST:
		err = loader.literal(&result, current)
	default:
		err = fmt.Errorf("unsupported source opcode %s", instr.TypeOf(current.op).Mnemonic)
	}
	if err != nil {
		return value{}, err
	}
	return loader.finish(result, current, indexed)
}

func slotInfo(op instr.Opcode) (field, method string, ok bool) {
	switch op {
	case instr.LOCAL_GET:
		return "locals", "local", true
	case instr.GLOBAL_GET:
		return "globals", "global", true
	case instr.UPVAL_GET:
		return "captures", "upval", true
	default:
		return "", "", false
	}
}

// isContainerSource reports whether op is a slot-read opcode (LOCAL_GET,
// GLOBAL_GET, UPVAL_GET) that array.get/struct.get container fusion can
// prove a declared element or field type from.
func isContainerSource(op instr.Opcode) bool {
	_, _, ok := slotInfo(op)
	return ok
}

// declaredTypesField names the threader field holding op's declared
// types.Type per slot, indexed the same way slotInfo's Kind-only field is:
// LOCAL_GET by localTypes, GLOBAL_GET by globalTypes, UPVAL_GET by
// captureTypes.
func declaredTypesField(op instr.Opcode) (string, bool) {
	switch op {
	case instr.LOCAL_GET:
		return "localTypes", true
	case instr.GLOBAL_GET:
		return "globalTypes", true
	case instr.UPVAL_GET:
		return "captureTypes", true
	default:
		return "", false
	}
}

func slotRead(state *state, current step) (value, error) {
	if state.standalone {
		switch current.op {
		case instr.I32_CONST:
			current.kind = instr.KindI32
		case instr.I64_CONST:
			current.kind = instr.KindI64
		case instr.F32_CONST:
			current.kind = instr.KindF32
		case instr.F64_CONST:
			current.kind = instr.KindF64
		}
	}
	result, err := load(current, len(state.stack), state.offset, state.label, state.standalone)
	if err != nil {
		return value{}, err
	}
	if state.standalone {
		if current.op == instr.CONST_GET {
			result.handler = constStandalone(current, result)
		} else if field, _, ok := slotInfo(current.op); ok {
			result.handler = slotStandalone(current, result, field)
		} else {
			result.handler = standalone(current.op, result.compile, result.push)
		}
		return result, nil
	}
	state.stack = append(state.stack, result)
	return result, nil
}

func slotStandalone(current step, input value, field string) jen.Code {
	compile := append([]jen.Code(nil), input.compile...)
	scalar := materialize(input, false, width(current.op))
	owned := materialize(input, true, width(current.op))
	choose := jen.Switch(jen.Id("c").Dot(field).Index(jen.Id("i0")).Dot("Repr").Call()).Block(
		jen.Case(
			jen.Qual("github.com/siyul-park/minivm/types", "KindI32"),
			jen.Qual("github.com/siyul-park/minivm/types", "KindF32"),
			jen.Qual("github.com/siyul-park/minivm/types", "KindF64"),
		).Block(jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(scalar...))),
	)
	if current.op == instr.LOCAL_GET {
		compile = append(compile, choose)
	} else {
		compile = append(compile,
			jen.If(jen.Id("i0").Op("<").Len(jen.Id("c").Dot(field))).Block(choose),
		)
	}
	compile = append(compile,
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(owned...)),
	)
	return threaderFunc(compile...)
}

func constStandalone(current step, input value) jen.Code {
	compile := append([]jen.Code(nil), input.compile...)
	boxed := input.boxed
	scalar := materialize(input, false, width(current.op))
	var owned []jen.Code
	if input.room {
		owned = append(owned, overflow())
	}
	owned = append(owned, input.check...)
	owned = append(owned, input.body...)
	owned = append(owned,
		jen.Id("i").Dot("retain").Call(jen.Id("addr")),
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(input.boxed),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(width(current.op)),
	)
	compile = append(compile,
		jen.Switch(jen.Add(boxed).Dot("Kind").Call()).Block(
			jen.Case(jen.Qual("github.com/siyul-park/minivm/types", "KindRef")).Block(
				jen.Id("addr").Op(":=").Add(boxed).Dot("Ref").Call(),
				jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(owned...)),
			),
		),
		jen.Return(jen.Func().Params(jen.Id("i").Op("*").Id("Interpreter")).Block(scalar...)),
	)
	return threaderFunc(compile...)
}

func materialize(input value, retain bool, advance int) []jen.Code {
	var body []jen.Code
	if input.room {
		body = append(body, overflow())
	}
	body = append(body, input.check...)
	body = append(body, input.body...)
	if retain {
		body = append(body, jen.Id("i").Dot("retainBox").Call(input.boxed))
	}
	return append(body,
		jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp")).Op("=").Add(input.boxed),
		jen.Id("i").Dot("sp").Op("++"),
		jen.Id("i").Dot("fr").Dot("ip").Op("+=").Lit(advance),
	)
}

func immediate(kind instr.Kind, at jen.Code) jen.Code {
	operand := jen.Qual("github.com/siyul-park/minivm/instr", "Instruction").Call(jen.Id("c").Dot("code").Index(jen.Add(at).Op(":"))).Dot("Operand").Call(jen.Lit(0))
	switch kind.Repr() {
	case instr.KindI32:
		return jen.Int32().Call(operand)
	case instr.KindI64:
		return jen.Int64().Call(operand)
	case instr.KindF32:
		return jen.Qual("github.com/siyul-park/minivm/types", "Box").Call(jen.Uint64().Call(jen.Uint32().Call(operand)), jen.Qual("github.com/siyul-park/minivm/types", "KindF32")).Dot("F32").Call()
	case instr.KindF64:
		return jen.Qual("github.com/siyul-park/minivm/types", "Boxed").Call(operand).Dot("F64").Call()
	default:
		panic(fmt.Sprintf("unsupported immediate kind %s", kind))
	}
}

func boxedTemp(raw string) string {
	return "r" + strings.TrimPrefix(raw, "v")
}

func localSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(2)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("locals")))).Block(jen.Switch(jen.Id("c").Dot("locals").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("fr").Dot("bp").Op("+").Add(jen.Id("idx"))),
			jen.If(jen.Id("addr").Op(">=").Add(jen.Id("i").Dot("sp"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("fr").Dot("bp").Op("+").Add(jen.Id("idx"))),
			jen.If(jen.Id("addr").Op(">=").Add(jen.Id("i").Dot("sp"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2)))))
}

func localTee() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(2)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("locals")))).Block(jen.Switch(jen.Id("c").Dot("locals").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("fr").Dot("bp").Op("+").Add(jen.Id("idx"))),
			jen.If(jen.Id("addr").Op(">=").Add(jen.Id("i").Dot("sp"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.List(jen.Id("addr")).Op(":=").List(jen.Id("i").Dot("fr").Dot("bp").Op("+").Add(jen.Id("idx"))),
			jen.If(jen.Id("addr").Op(">=").Add(jen.Id("i").Dot("sp"))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("val")),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("stack").Index(jen.Id("addr"))).Op("=").List(jen.Id("val")),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2)))))
}

func globalSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("globals")))).Block(jen.Switch(jen.Id("c").Dot("globals").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("globals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("globals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func globalTee() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Op("*").Add(jen.Parens(jen.Op("*").Add(jen.Id("uint16"))).Call(jen.Qual("unsafe", "Pointer").Call(jen.Op("&").Add(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(3)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("globals")))).Block(jen.Switch(jen.Id("c").Dot("globals").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("globals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("globals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("retainBox").Call(jen.Id("val")),
				jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("globals").Index(jen.Id("idx"))).Op("=").List(jen.Id("val")),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(3)))))
}

func upvalSet() jen.Code {
	return jen.Func().Params(jen.Id("c").Add(jen.Op("*").Add(jen.Id("threader")))).Params(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter"))))).Block(jen.List(jen.Id("idx")).Op(":=").List(jen.Id("int").Call(jen.Id("c").Dot("code").Index(jen.Id("c").Dot("ip").Op("+").Add(jen.Lit(1))))),
		jen.List(jen.Id("c").Dot("ip")).Op("+=").List(jen.Lit(2)),
		jen.If(jen.Id("idx").Op("<").Add(jen.Id("len").Call(jen.Id("c").Dot("captures")))).Block(jen.Switch(jen.Id("c").Dot("captures").Index(jen.Id("idx")).Dot("Repr").Call()).Block(jen.Case(jen.Id("types").Dot("KindI32"), jen.Id("types").Dot("KindF32"), jen.Id("types").Dot("KindF64")).Block(jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("fr").Dot("upvals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("i").Dot("fr").Dot("upvals").Index(jen.Id("idx"))).Op("=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2))))))),
		jen.Return(jen.Func().Params(jen.Id("i").Add(jen.Op("*").Add(jen.Id("Interpreter")))).Block(jen.If(jen.Id("i").Dot("sp").Op("==").Add(jen.Lit(0))).Block(jen.Id("panic").Call(jen.Id("ErrStackUnderflow"))),
			jen.If(jen.Id("idx").Op(">=").Add(jen.Id("len").Call(jen.Id("i").Dot("fr").Dot("upvals")))).Block(jen.Id("panic").Call(jen.Id("ErrSegmentationFault"))),
			jen.List(jen.Id("val")).Op(":=").List(jen.Id("i").Dot("stack").Index(jen.Id("i").Dot("sp").Op("-").Add(jen.Lit(1)))),
			jen.List(jen.Id("old")).Op(":=").List(jen.Id("i").Dot("fr").Dot("upvals").Index(jen.Id("idx"))),
			jen.If(jen.Id("old").Op("!=").Add(jen.Id("val"))).Block(jen.Id("i").Dot("releaseBox").Call(jen.Id("old"))),
			jen.List(jen.Id("i").Dot("fr").Dot("upvals").Index(jen.Id("idx"))).Op("=").List(jen.Id("val")),
			jen.Id("i").Dot("sp").Op("--"),
			jen.List(jen.Id("i").Dot("fr").Dot("ip")).Op("+=").List(jen.Lit(2)))))
}
