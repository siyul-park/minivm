package interp

import (
	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

type jitCompiler struct {
	assembler *asm.Assembler
	types     []types.Type
	constants []types.Boxed
	heap      []types.Value
	code      []byte
	ip        int
}

var (
	_PROLOGUE = len(jit) - 2
	_EPILOGUE = len(jit) - 1
)

var arch *asm.Arch
var jit = [256]func(c *jitCompiler) bool{}

func init() {
	for i, fn := range jit {
		if fn == nil {
			jit[i] = func(c *jitCompiler) bool {
				inst := instr.Instruction(c.code[c.ip:])
				c.ip += inst.Width()
				return false
			}
		}
	}
}

func (c *jitCompiler) Compile(code []byte) []func(*Interpreter) {
	c.assembler.Reset()
	c.code, c.ip = code, 0

	m := pass.NewManager()
	if err := m.Register(analysis.NewBasicBlocksPass()); err != nil {
		return nil
	}
	if err := m.Run(&types.Function{
		Typ:  &types.FunctionType{},
		Code: code,
	}); err != nil {
		return nil
	}

	var blocks []*analysis.BasicBlock
	if err := m.Load(&blocks); err != nil {
		return nil
	}

	compiled := make([]func(*Interpreter), len(code))

	for _, b := range blocks {
		c.assembler.Reset()
		jit[_PROLOGUE](c)

		c.ip = b.Start

		entryIP := -1
		lastIP := b.Start
		count := 0
		for c.ip < b.End {
			ok := jit[c.code[c.ip]](c)
			if ok {
				if entryIP == -1 {
					entryIP = c.ip
				}
				lastIP = c.ip
				count++
			}

			if c.ip == b.End-1 || !ok {
				if count > 8 && entryIP != -1 {
					nParams := len(c.assembler.Params())
					nRets := len(c.assembler.Returns())

					kinds := make([]types.Kind, nRets)
					for i, r := range c.assembler.Returns() {
						switch r.Type() {
						case asm.RegTypeInt:
							switch r.Width() {
							case asm.Width32:
								kinds[i] = types.KindI32
							case asm.Width64:
								kinds[i] = types.KindI64
							default:
								return nil
							}
						case asm.RegTypeFloat:
							switch r.Width() {
							case asm.Width32:
								kinds[i] = types.KindF32
							case asm.Width64:
								kinds[i] = types.KindF64
							default:
								return nil
							}
						default:
							return nil
						}
					}

					jit[_EPILOGUE](c)

					fn, err := c.assembler.Build()
					if err != nil {
						return nil
					}

					exit := lastIP
					pc := entryIP

					params := make([]uint64, nParams)
					compiled[pc] = func(i *Interpreter) {
						base := i.sp - nParams
						for j := range params {
							params[j] = i.unbox64(i.stack[base+j])
						}
						rets, err := fn.Call(params)
						if err != nil {
							panic(err)
						}
						for j := 0; j < nRets; j++ {
							i.stack[base+j] = i.box64(rets[j], kinds[j])
						}
						i.sp = base + nRets
						i.frames[i.fp-1].ip = exit
					}
				}

				c.assembler.Reset()
				jit[_PROLOGUE](c)

				count = 0
				entryIP = -1
			}
		}
	}

	return compiled
}
