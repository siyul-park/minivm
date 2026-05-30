package arm64

// init intentionally does not register the Lowerer yet. The current
// implementation rejects every opcode, so callers gating on
// jit.Active() == nil correctly fall back to the threaded interpreter.
// Registration switches on once Phase A lowering lands.
func init() {}
