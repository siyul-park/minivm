package asm

// Enter runs the native code at code on ctx's native stack, starting at
// ctx.NSP, until that activation returns or exits. It returns how native code
// left; anything but TrapReturn leaves the activation suspended for Resume.
func Enter(code uintptr, ctx *Context) Trap {
	enter(code, ctx)
	return ctx.Trap
}

// Resume continues the activation ctx last suspended, with the registers
// Regs and Fregs hold, at PC, on the native stack at NSP.
func Resume(ctx *Context) Trap {
	resume(ctx)
	return ctx.Trap
}

func enter(code uintptr, ctx *Context)
func resume(ctx *Context)

// exit is the stub native code branches to when it leaves other than by
// returning. It is native code's to call, never Go's.
func exit()

func exitPC() uintptr
