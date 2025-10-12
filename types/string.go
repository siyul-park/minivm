package types

type String string

type stringType struct{}

var TypeString = stringType{}

var _ Value = String("")
var _ Type = stringType{}

func (s String) Kind() Kind {
	return KindRef
}

func (s String) Type() Type {
	return TypeString
}

func (s String) String() string {
	return string(s)
}

func (stringType) Kind() Kind {
	return KindRef
}

func (stringType) String() string {
	return "string"
}

func (stringType) Cast(other Type) bool {
	return other == TypeString
}

func (stringType) Equals(other Type) bool {
	return other == TypeString
}
