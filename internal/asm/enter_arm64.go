package asm

// Enter runs native code at code on the context's native stack until that
// activation returns or exits. Anything but TrapReturn leaves the activation
// suspended for Resume.
func Enter(code uintptr, ctx *Context) Trap {
	enter(code, ctx)
	return ctx.trap
}

// Resume continues the last suspended activation from its saved native PC
// and native stack pointer, after restoring its saved register file.
func Resume(ctx *Context) Trap {
	resume(ctx)
	return ctx.trap
}

func enter(code uintptr, ctx *Context)
func resume(ctx *Context)

// exit is the stub native code branches to when it leaves other than by
// returning. It is native code's to call, never Go's.
func exit()

func exitPC() uintptr
