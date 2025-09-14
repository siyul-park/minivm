package types

type Type interface {
	Kind() Kind
	String() string
	Equals(other Type) bool
}
