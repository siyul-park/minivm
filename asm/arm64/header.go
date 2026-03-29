package arm64

import "github.com/siyul-park/minivm/asm"

type Header uint64

func NewHeader(params, rets []asm.RegType) Header {
	var paramTypes, returnTypes uint64
	for i, t := range params {
		if t == asm.TypeFloat {
			paramTypes |= 1 << uint(i)
		}
	}
	for i, t := range rets {
		if t == asm.TypeFloat {
			returnTypes |= 1 << uint(i)
		}
	}
	return Header(
		uint64(len(params))&0xFF |
			uint64(len(rets))&0xFF<<8 |
			paramTypes<<16 |
			returnTypes<<24,
	)
}

func (h Header) Params() int {
	return int(h & 0xFF)
}

func (h Header) Returns() int {
	return int((h >> 8) & 0xFF)
}

func (h Header) ParamTypes() uint8 {
	return uint8((h >> 16) & 0xFF)
}

func (h Header) ReturnTypes() uint8 {
	return uint8((h >> 24) & 0xFF)
}
