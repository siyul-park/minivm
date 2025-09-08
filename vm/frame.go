package vm

import "github.com/siyul-park/minivm/types"

type Frame struct {
	cl *types.Closure
	ip int
	bp int
}
