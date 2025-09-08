package vm

import "github.com/siyul-park/minivm/types"

type Frame struct {
	cl   *types.Closure
	addr int
	ip   int
	bp   int
}
