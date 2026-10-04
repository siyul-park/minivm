package jit

// Class is what an exit costs the code that takes it.
type Class uint8

// Ledger weighs the native work of one code account against the round trips
// its exits cost the interpreter, both in work units: Context.Budget's back
// edges, calls, and returns. The zero Ledger is empty.
type Ledger struct {
	// debt is unpaid exit cost minus work, in 1/scale work units, floored
	// at -slack.
	debt int64
}

const (
	// ClassBridge is a served ExitBridge or ExitBox.
	ClassBridge Class = iota
	// ClassRelease is an ExitRelease.
	ClassRelease
	// ClassCall is an ExitCall the interpreter serves.
	ClassCall
	// ClassCallout is an ExitBridge of an allocating operation the
	// interpreter serves from its operand words, with no scratch frame.
	ClassCallout
	// ClassGuard is a deopt that refutes a speculation: the interpreter
	// judges it by feedback, not by cost.
	ClassGuard
	// ClassTrap is a deopt for the program's own fault: it costs nothing.
	ClassTrap
)

const (
	// scale is the ledger's fixed-point resolution: a work unit is scale debt.
	scale = 8
	// slack is how far the ledger's debt or credit may run, 64 work units:
	// warm-up and bursts of exits pass, a sustained loss does not.
	slack = 64 * scale
)

// prices is each class's round trip in work units times scale: the time one
// served exit adds over threaded code, divided by the time native code saves
// per work unit.
var prices = [...]int64{
	ClassBridge:  19, // 53 ns / 22 ns ≈ 2.4 units
	ClassRelease: 6,  // 17 ns / 22 ns ≈ 0.75 units
	ClassCall:    28, // 76 ns / 22 ns ≈ 3.5 units
	ClassCallout: 14, // 0.75 of a bridge measured side by side (33 vs 44 ns over threaded) ≈ 1.75 units
}

// Spend credits work units of native work.
func (l *Ledger) Spend(work int64) {
	l.debt = max(l.debt-work*scale, -slack)
}

// Charge debits one exit of class c and reports whether native code still
// pays for its exits; once it does not, the code should retire.
func (l *Ledger) Charge(c Class) bool {
	if int(c) < len(prices) {
		l.debt += prices[c]
	}
	return l.debt <= slack
}
