package vm

import "github.com/siyul-park/minivm/types"

type Frame struct {
	closure *types.Closure
	ip      int
	bp      int
}
