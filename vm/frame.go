package vm

import "github.com/siyul-park/minivm/types"

type Frame struct {
	fn   *types.Function
	addr int
	ip   int
	bp   int
}
