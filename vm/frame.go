package vm

import "github.com/siyul-park/minivm/types"

type Frame struct {
	cl  *types.Closure
	ref types.Ref
	ip  int
	bp  int
}
