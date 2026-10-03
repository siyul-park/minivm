// Package transform holds bytecode↔SSA translation (Translate, emit) and the
// SSA rewrite passes (FoldPass, PromotePass, ForwardPass, CSEPass, GuardPass,
// HoistPass, BoundPass, CompactPass, DCEPass) that compose under SSAPass.
//
// Rewrite passes run after translation because only SSA is target-independent;
// CSE and DCE sit after the simplification passes — PromotePass and
// ForwardPass before CSEPass, GuardPass and HoistPass after it — with DCEPass
// last to drop what earlier passes left unused. Each pass composes independently;
// optimize assembles the leveled compositions.
package transform
