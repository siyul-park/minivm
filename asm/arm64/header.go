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

func (h Header) Params() []asm.RegType {
	n := int(h & 0xFF)
	types := uint8((h >> 16) & 0xFF)

	res := make([]asm.RegType, n)
	for i := 0; i < n; i++ {
		if (types>>uint(i))&1 == 1 {
			res[i] = asm.TypeFloat
		} else {
			res[i] = asm.TypeInt
		}
	}
	return res
}

func (h Header) Returns() []asm.RegType {
	n := int((h >> 8) & 0xFF)
	types := uint8((h >> 24) & 0xFF)

	res := make([]asm.RegType, n)
	for i := 0; i < n; i++ {
		if (types>>uint(i))&1 == 1 {
			res[i] = asm.TypeFloat
		} else {
			res[i] = asm.TypeInt
		}
	}
	return res
}
