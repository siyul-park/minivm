package types

type RTT struct {
	Elem Type
}

type rttType struct{}

var TypeRTT = rttType{}

var _ Value = (*RTT)(nil)
var _ Type = (*RTT)(nil)
var _ Type = rttType{}

func NewRTT(elem Type) *RTT {
	return &RTT{Elem: elem}
}

func (r *RTT) Kind() Kind {
	return KindRef
}

func (r *RTT) Type() Type {
	return TypeRTT
}

func (r *RTT) Interface() any {
	return r.Elem
}

func (r *RTT) String() string {
	return r.Elem.String()
}

func (r *RTT) Cast(other Type) bool {
	return r.Elem.Cast(other)
}

func (r *RTT) Equals(other Type) bool {
	return r.Elem.Equals(other)
}

func (rttType) Kind() Kind {
	return KindRef
}

func (rttType) String() string {
	return "rtt"
}

func (t rttType) Cast(other Type) bool {
	return other == TypeRTT
}

func (rttType) Equals(other Type) bool {
	return other == TypeRTT
}
