package transform

import (
	"math"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

type ConstantFoldingPass struct{}

var _ pass.Pass[*program.Program] = (*ConstantFoldingPass)(nil)

func NewConstantFoldingPass() *ConstantFoldingPass {
	return &ConstantFoldingPass{}
}

func (p *ConstantFoldingPass) Run(m *pass.Manager) (*program.Program, error) {
	var prog *program.Program
	if err := m.Load(&prog); err != nil {
		return nil, err
	}

	var fns []*types.Function
	fns = append(fns, &types.Function{
		Signature: types.NewFunctionSignature(),
		Code:      prog.Code,
	})
	for _, v := range prog.Constants {
		if fn, ok := v.(*types.Function); ok {
			fns = append(fns, fn)
		}
	}

	for _, fn := range fns {
		var blocks []*analysis.BasicBlock
		if err := m.Convert(fn, &blocks); err != nil {
			return nil, err
		}

		for _, blk := range blocks {
			for ip := blk.Start; ip < blk.End; {
				i0 := instr.Instruction(fn.Code[ip:])
				switch i0.Opcode() {
				case instr.I32_CONST:
					v0 := int32(i0.Operand(0))
					if ip+i0.Width() >= blk.End {
						break
					}
					i1 := instr.Instruction(fn.Code[ip+i0.Width():])
					switch i1.Opcode() {
					case instr.I32_CONST:
						if ip+i0.Width()+i1.Width() >= blk.End {
							break
						}
						v1 := int32(i1.Operand(0))
						i2 := instr.Instruction(fn.Code[ip+i0.Width()+i1.Width():])
						switch i2.Opcode() {
						case instr.I32_ADD:
							inst := instr.New(instr.I32_CONST, uint64(v0+v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_SUB:
							inst := instr.New(instr.I32_CONST, uint64(v0-v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_MUL:
							inst := instr.New(instr.I32_CONST, uint64(v0*v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_DIV_S:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.I32_CONST, uint64(v0/v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_DIV_U:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.I32_CONST, uint64(uint32(v0)/uint32(v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_REM_S:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.I32_CONST, uint64(v0%v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_REM_U:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.I32_CONST, uint64(uint32(v0)%uint32(v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_SHL:
							inst := instr.New(instr.I32_CONST, uint64(v0<<(v1&0x1F)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_SHR_S:
							inst := instr.New(instr.I32_CONST, uint64(v0>>(v1&0x1F)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_SHR_U:
							inst := instr.New(instr.I32_CONST, uint64(uint32(v0)>>(uint32(v1)&0x1F)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_XOR:
							inst := instr.New(instr.I32_CONST, uint64(v0^v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_AND:
							inst := instr.New(instr.I32_CONST, uint64(v0&v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_OR:
							inst := instr.New(instr.I32_CONST, uint64(v0|v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_EQ:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_NE:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_LT_S:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_LT_U:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(uint32(v0) < uint32(v1))))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_GT_S:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_GT_U:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(uint32(v0) > uint32(v1))))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_LE_S:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_LE_U:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(uint32(v0) <= uint32(v1))))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_GE_S:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I32_GE_U:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(uint32(v0) >= uint32(v1))))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						default:
						}
					case instr.I32_EQZ:
						inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 == 0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.I32_TO_F32_S:
						inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(v0))))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.I32_TO_F32_U:
						inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(uint32(v0)))))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					default:
					}
				case instr.I64_CONST:
					v0 := int64(i0.Operand(0))
					if ip+i0.Width() >= blk.End {
						break
					}
					i1 := instr.Instruction(fn.Code[ip+i0.Width():])
					switch i1.Opcode() {
					case instr.I64_CONST:
						if ip+i0.Width()+i1.Width() >= blk.End {
							break
						}
						v1 := int64(i1.Operand(0))
						i2 := instr.Instruction(fn.Code[ip+i0.Width()+i1.Width():])
						switch i2.Opcode() {
						case instr.I64_ADD:
							inst := instr.New(instr.I64_CONST, uint64(v0+v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_SUB:
							inst := instr.New(instr.I64_CONST, uint64(v0-v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_MUL:
							inst := instr.New(instr.I64_CONST, uint64(v0*v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_DIV_S:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.I64_CONST, uint64(v0/v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_DIV_U:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.I64_CONST, uint64(v0)/uint64(v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_REM_S:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.I64_CONST, uint64(v0%v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_REM_U:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.I64_CONST, uint64(v0)%uint64(v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_SHL:
							inst := instr.New(instr.I64_CONST, uint64(v0<<(v1&0x3F)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_SHR_S:
							inst := instr.New(instr.I64_CONST, uint64(v0>>(v1&0x3F)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_SHR_U:
							inst := instr.New(instr.I64_CONST, uint64(v0)>>(uint64(v1)&0x3F))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_EQ:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_NE:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_LT_S:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_LT_U:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(uint64(v0) < uint64(v1))))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_GT_S:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_GT_U:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(uint64(v0) > uint64(v1))))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_LE_S:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_LE_U:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(uint64(v0) <= uint64(v1))))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_GE_S:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.I64_GE_U:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(uint64(v0) >= uint64(v1))))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						default:
						}
					case instr.I64_EQZ:
						inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 == 0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.I64_TO_I32:
						inst := instr.New(instr.I32_CONST, uint64(int32(v0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.I64_TO_F32_S:
						inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(v0))))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.I64_TO_F32_U:
						inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(uint64(v0)))))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.I64_TO_F64_S:
						inst := instr.New(instr.F64_CONST, math.Float64bits(float64(v0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.I64_TO_F64_U:
						inst := instr.New(instr.F64_CONST, math.Float64bits(float64(uint64(v0))))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					default:
					}
				case instr.F32_CONST:
					v0 := math.Float32frombits(uint32(i0.Operand(0)))
					if ip+i0.Width() >= blk.End {
						break
					}
					i1 := instr.Instruction(fn.Code[ip+i0.Width():])
					switch i1.Opcode() {
					case instr.F32_CONST:
						if ip+i0.Width()+i1.Width() >= blk.End {
							break
						}
						v1 := math.Float32frombits(uint32(i1.Operand(0)))
						i2 := instr.Instruction(fn.Code[ip+i0.Width()+i1.Width():])
						switch i2.Opcode() {
						case instr.F32_ADD:
							inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(v0+v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_SUB:
							inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(v0-v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_MUL:
							inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(v0*v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_DIV:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(v0/v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_EQ:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_NE:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_LT:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_GT:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_LE:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F32_GE:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						default:
						}
					case instr.F32_TO_I32_U:
						inst := instr.New(instr.I32_CONST, uint64(uint32(v0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.F32_TO_I32_S:
						inst := instr.New(instr.I32_CONST, uint64(int32(v0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					default:
					}
				case instr.F64_CONST:
					v0 := math.Float64frombits(i0.Operand(0))
					if ip+i0.Width() >= blk.End {
						break
					}
					i1 := instr.Instruction(fn.Code[ip+i0.Width():])
					switch i1.Opcode() {
					case instr.F64_CONST:
						if ip+i0.Width()+i1.Width() >= blk.End {
							break
						}
						v1 := math.Float64frombits(i1.Operand(0))
						i2 := instr.Instruction(fn.Code[ip+i0.Width()+i1.Width():])
						switch i2.Opcode() {
						case instr.F64_ADD:
							inst := instr.New(instr.F64_CONST, math.Float64bits(v0+v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_SUB:
							inst := instr.New(instr.F64_CONST, math.Float64bits(v0-v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_MUL:
							inst := instr.New(instr.F64_CONST, math.Float64bits(v0*v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_DIV:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							inst := instr.New(instr.F64_CONST, math.Float64bits(v0/v1))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_EQ:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_NE:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_LT:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_GT:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_LE:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						case instr.F64_GE:
							inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1)))
							width := i0.Width() + i1.Width() + i2.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						default:
						}
					case instr.F64_TO_I32_S:
						inst := instr.New(instr.I32_CONST, uint64(uint32(v0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.F64_TO_I32_U:
						inst := instr.New(instr.I32_CONST, uint64(int32(v0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.F64_TO_I64_S:
						inst := instr.New(instr.I64_CONST, uint64(int64(v0)))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.F64_TO_I64_U:
						inst := instr.New(instr.I64_CONST, uint64(v0))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					case instr.F64_TO_F32:
						inst := instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(v0))))
						width := i0.Width() + i1.Width()
						p.fold(fn.Code[ip:ip+width], inst)
						ip += width - inst.Width()
						continue
					default:
					}
				case instr.CONST_GET:
					addr0 := int(i0.Operand(0))
					if addr0 < 0 || addr0 >= len(prog.Constants) || ip+i0.Width() >= blk.End {
						break
					}
					i1 := instr.Instruction(fn.Code[ip+i0.Width():])
					switch v0 := prog.Constants[addr0].(type) {
					case types.String:
						switch i1.Opcode() {
						case instr.CONST_GET:
							addr1 := int(i1.Operand(0))
							if addr1 < 0 || addr1 >= len(prog.Constants) || ip+i0.Width()+i1.Width() >= blk.End {
								break
							}
							i2 := instr.Instruction(fn.Code[ip+i0.Width()+i1.Width():])
							switch v1 := prog.Constants[addr1].(type) {
							case types.String:
								switch i2.Opcode() {
								case instr.STRING_CONCAT:
									prog.Constants = append(prog.Constants, v0+v1)
									inst := instr.New(instr.CONST_GET, uint64(len(prog.Constants)-1))
									width := i0.Width() + i1.Width() + i2.Width()
									p.fold(fn.Code[ip:ip+width], inst)
									ip += width - inst.Width()
									continue
								case instr.STRING_EQ:
									inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1)))
									width := i0.Width() + i1.Width() + i2.Width()
									p.fold(fn.Code[ip:ip+width], inst)
									ip += width - inst.Width()
									continue
								case instr.STRING_NE:
									inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1)))
									width := i0.Width() + i1.Width() + i2.Width()
									p.fold(fn.Code[ip:ip+width], inst)
									ip += width - inst.Width()
									continue
								case instr.STRING_LT:
									inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1)))
									width := i0.Width() + i1.Width() + i2.Width()
									p.fold(fn.Code[ip:ip+width], inst)
									ip += width - inst.Width()
									continue
								case instr.STRING_GT:
									inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1)))
									width := i0.Width() + i1.Width() + i2.Width()
									p.fold(fn.Code[ip:ip+width], inst)
									ip += width - inst.Width()
									continue
								case instr.STRING_LE:
									inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1)))
									width := i0.Width() + i1.Width() + i2.Width()
									p.fold(fn.Code[ip:ip+width], inst)
									ip += width - inst.Width()
									continue
								case instr.STRING_GE:
									inst := instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1)))
									width := i0.Width() + i1.Width() + i2.Width()
									p.fold(fn.Code[ip:ip+width], inst)
									ip += width - inst.Width()
									continue
								default:
								}
							default:
							}
						case instr.STRING_ENCODE_UTF32:
							prog.Constants = append(prog.Constants, types.I32Array(v0))
							inst := instr.New(instr.CONST_GET, uint64(len(prog.Constants)-1))
							width := i0.Width() + i1.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						default:
						}
					case types.I32Array:
						switch i1.Opcode() {
						case instr.STRING_NEW_UTF32:
							prog.Constants = append(prog.Constants, types.String(v0))
							inst := instr.New(instr.CONST_GET, uint64(len(prog.Constants)-1))
							width := i0.Width() + i1.Width()
							p.fold(fn.Code[ip:ip+width], inst)
							ip += width - inst.Width()
							continue
						default:
						}
					default:
					}
				default:
				}
				ip += i0.Width()
			}
		}
	}
	return prog, nil
}

func (p *ConstantFoldingPass) fold(code []byte, inst instr.Instruction) {
	for i := range code {
		code[i] = byte(instr.NOP)
	}
	copy(code[len(code)-inst.Width():], inst)
}
