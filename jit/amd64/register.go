package amd64

import "github.com/siyul-park/minivm/jit"

func init() {
	jit.Register("amd64", Lowerer{})
}
