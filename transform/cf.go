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

func (p *ConstantFoldingPass) Run(m *pass.Manager, prog *program.Program) (pass.Preserved, error) {
	for _, fn := range functions(prog) {
		blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, fn)
		if err != nil {
			return pass.PreserveNone(), err
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
						w := i0.Width() + i1.Width() + i2.Width()
						switch i2.Opcode() {
						case instr.I32_ADD:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0+v1)))
							continue
						case instr.I32_SUB:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0-v1)))
							continue
						case instr.I32_MUL:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0*v1)))
							continue
						case instr.I32_DIV_S:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0/v1)))
							continue
						case instr.I32_DIV_U:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(uint32(v0)/uint32(v1))))
							continue
						case instr.I32_REM_S:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0%v1)))
							continue
						case instr.I32_REM_U:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(uint32(v0)%uint32(v1))))
							continue
						case instr.I32_SHL:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0<<(v1&0x1F))))
							continue
						case instr.I32_SHR_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0>>(v1&0x1F))))
							continue
						case instr.I32_SHR_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(uint32(v0)>>(uint32(v1)&0x1F))))
							continue
						case instr.I32_XOR:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0^v1)))
							continue
						case instr.I32_AND:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0&v1)))
							continue
						case instr.I32_OR:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(v0|v1)))
							continue
						case instr.I32_EQ:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1))))
							continue
						case instr.I32_NE:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1))))
							continue
						case instr.I32_LT_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1))))
							continue
						case instr.I32_LT_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(uint32(v0) < uint32(v1)))))
							continue
						case instr.I32_GT_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1))))
							continue
						case instr.I32_GT_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(uint32(v0) > uint32(v1)))))
							continue
						case instr.I32_LE_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1))))
							continue
						case instr.I32_LE_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(uint32(v0) <= uint32(v1)))))
							continue
						case instr.I32_GE_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1))))
							continue
						case instr.I32_GE_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(uint32(v0) >= uint32(v1)))))
							continue
						default:
						}
					case instr.I32_EQZ:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I32_CONST, uint64(types.Bool(v0 == 0))))
						continue
					case instr.I32_TO_F32_S:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(v0)))))
						continue
					case instr.I32_TO_F32_U:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(uint32(v0))))))
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
						w := i0.Width() + i1.Width() + i2.Width()
						switch i2.Opcode() {
						case instr.I64_ADD:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0+v1)))
							continue
						case instr.I64_SUB:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0-v1)))
							continue
						case instr.I64_MUL:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0*v1)))
							continue
						case instr.I64_DIV_S:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0/v1)))
							continue
						case instr.I64_DIV_U:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0)/uint64(v1)))
							continue
						case instr.I64_REM_S:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0%v1)))
							continue
						case instr.I64_REM_U:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0)%uint64(v1)))
							continue
						case instr.I64_SHL:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0<<(v1&0x3F))))
							continue
						case instr.I64_SHR_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0>>(v1&0x3F))))
							continue
						case instr.I64_SHR_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I64_CONST, uint64(v0)>>(uint64(v1)&0x3F)))
							continue
						case instr.I64_EQ:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1))))
							continue
						case instr.I64_NE:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1))))
							continue
						case instr.I64_LT_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1))))
							continue
						case instr.I64_LT_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(uint64(v0) < uint64(v1)))))
							continue
						case instr.I64_GT_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1))))
							continue
						case instr.I64_GT_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(uint64(v0) > uint64(v1)))))
							continue
						case instr.I64_LE_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1))))
							continue
						case instr.I64_LE_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(uint64(v0) <= uint64(v1)))))
							continue
						case instr.I64_GE_S:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1))))
							continue
						case instr.I64_GE_U:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(uint64(v0) >= uint64(v1)))))
							continue
						default:
						}
					case instr.I64_EQZ:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I32_CONST, uint64(types.Bool(v0 == 0))))
						continue
					case instr.I64_TO_I32:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I32_CONST, uint64(int32(v0))))
						continue
					case instr.I64_TO_F32_S:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(v0)))))
						continue
					case instr.I64_TO_F32_U:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(uint64(v0))))))
						continue
					case instr.I64_TO_F64_S:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.F64_CONST, math.Float64bits(float64(v0))))
						continue
					case instr.I64_TO_F64_U:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.F64_CONST, math.Float64bits(float64(uint64(v0)))))
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
						w := i0.Width() + i1.Width() + i2.Width()
						switch i2.Opcode() {
						case instr.F32_ADD:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.F32_CONST, uint64(math.Float32bits(v0+v1))))
							continue
						case instr.F32_SUB:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.F32_CONST, uint64(math.Float32bits(v0-v1))))
							continue
						case instr.F32_MUL:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.F32_CONST, uint64(math.Float32bits(v0*v1))))
							continue
						case instr.F32_DIV:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.F32_CONST, uint64(math.Float32bits(v0/v1))))
							continue
						case instr.F32_EQ:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1))))
							continue
						case instr.F32_NE:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1))))
							continue
						case instr.F32_LT:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1))))
							continue
						case instr.F32_GT:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1))))
							continue
						case instr.F32_LE:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1))))
							continue
						case instr.F32_GE:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1))))
							continue
						default:
						}
					case instr.F32_TO_I32_U:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I32_CONST, uint64(uint32(v0))))
						continue
					case instr.F32_TO_I32_S:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I32_CONST, uint64(int32(v0))))
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
						w := i0.Width() + i1.Width() + i2.Width()
						switch i2.Opcode() {
						case instr.F64_ADD:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.F64_CONST, math.Float64bits(v0+v1)))
							continue
						case instr.F64_SUB:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.F64_CONST, math.Float64bits(v0-v1)))
							continue
						case instr.F64_MUL:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.F64_CONST, math.Float64bits(v0*v1)))
							continue
						case instr.F64_DIV:
							if v1 == 0 {
								ip += i0.Width()
								continue
							}
							ip = p.replace(fn.Code, ip, w, instr.New(instr.F64_CONST, math.Float64bits(v0/v1)))
							continue
						case instr.F64_EQ:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1))))
							continue
						case instr.F64_NE:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1))))
							continue
						case instr.F64_LT:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1))))
							continue
						case instr.F64_GT:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1))))
							continue
						case instr.F64_LE:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1))))
							continue
						case instr.F64_GE:
							ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1))))
							continue
						default:
						}
					case instr.F64_TO_I32_S:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I32_CONST, uint64(uint32(v0))))
						continue
					case instr.F64_TO_I32_U:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I32_CONST, uint64(int32(v0))))
						continue
					case instr.F64_TO_I64_S:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I64_CONST, uint64(int64(v0))))
						continue
					case instr.F64_TO_I64_U:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.I64_CONST, uint64(v0)))
						continue
					case instr.F64_TO_F32:
						ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.F32_CONST, uint64(math.Float32bits(float32(v0)))))
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
							w := i0.Width() + i1.Width() + i2.Width()
							switch v1 := prog.Constants[addr1].(type) {
							case types.String:
								switch i2.Opcode() {
								case instr.STRING_CONCAT:
									prog.Constants = append(prog.Constants, v0+v1)
									ip = p.replace(fn.Code, ip, w, instr.New(instr.CONST_GET, uint64(len(prog.Constants)-1)))
									continue
								case instr.STRING_EQ:
									ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 == v1))))
									continue
								case instr.STRING_NE:
									ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 != v1))))
									continue
								case instr.STRING_LT:
									ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 < v1))))
									continue
								case instr.STRING_GT:
									ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 > v1))))
									continue
								case instr.STRING_LE:
									ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 <= v1))))
									continue
								case instr.STRING_GE:
									ip = p.replace(fn.Code, ip, w, instr.New(instr.I32_CONST, uint64(types.Bool(v0 >= v1))))
									continue
								default:
								}
							default:
							}
						case instr.STRING_ENCODE_UTF32:
							prog.Constants = append(prog.Constants, types.TypedArray[int32](v0))
							ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.CONST_GET, uint64(len(prog.Constants)-1)))
							continue
						default:
						}
					case types.TypedArray[int32]:
						switch i1.Opcode() {
						case instr.STRING_NEW_UTF32:
							prog.Constants = append(prog.Constants, types.String(v0))
							ip = p.replace(fn.Code, ip, i0.Width()+i1.Width(), instr.New(instr.CONST_GET, uint64(len(prog.Constants)-1)))
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
	return pass.PreserveNone(), nil
}

// replace folds the [ip, ip+width) range to inst and returns the next ip.
func (p *ConstantFoldingPass) replace(code []byte, ip, width int, inst instr.Instruction) int {
	p.fold(code[ip:ip+width], inst)
	return ip + width - inst.Width()
}

func (p *ConstantFoldingPass) fold(code []byte, inst instr.Instruction) {
	for i := range code {
		code[i] = byte(instr.NOP)
	}
	copy(code[len(code)-inst.Width():], inst)
}
