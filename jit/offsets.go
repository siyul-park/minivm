package jit

import "reflect"

type Offsets struct {
	Stack  uintptr
	SP     uintptr
	FP     uintptr
	Frames uintptr
}

func GetOffsets(t reflect.Type) Offsets {
	var offsets Offsets
	stackField, _ := t.Elem().FieldByName("stack")
	spField, _ := t.Elem().FieldByName("sp")
	fpField, _ := t.Elem().FieldByName("fp")
	framesField, _ := t.Elem().FieldByName("frames")

	offsets.Stack = stackField.Offset
	offsets.SP = spField.Offset
	offsets.FP = fpField.Offset
	offsets.Frames = framesField.Offset

	return offsets
}
