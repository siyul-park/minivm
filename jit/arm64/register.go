package arm64

import "github.com/siyul-park/minivm/jit"

func init() {
	jit.Register("arm64", Lowerer{})
}
