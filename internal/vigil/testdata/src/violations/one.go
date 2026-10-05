package violations

import (
	"context"
	"fmt"
)

type Thing struct{} // want "CP001"

// PublicState owns its private state for boundary tests.
type PublicState struct {
	value int
}

// OtherState is a distinct owner for boundary tests.
type OtherState struct{}

// PublicFactory is the public contract for factory results.
type PublicFactory interface {
	Name() string
}

// PublicOption hides private option state behind a public functional option type.
type PublicOption func(*privateState)

// PublicContainer exposes a private field type through a public struct field.
type PublicContainer struct { // want "CP004"
	Value map[string]privateState
}

// PublicContract exposes a private method parameter through a public interface.
type PublicContract interface { // want "CP004"
	Use(privateState)
}

type privateThing struct{}
type privateFactory struct{}
type privateState struct{}
type orderThing struct{}

func Use(value int, ctx context.Context) {} // want "CP001"

// UseHelperA exercises a cross-group dependency that file ordering does not constrain.
func UseHelperA() { helperValue() }

// UseHelperB exercises the same shared lower-level helper.
func UseHelperB() { helperValue() }

// UsePrivate exercises a public parameter that directly exposes private state.
func UsePrivate(values []privateState) {} // want "CP004"

func NewThing() interface{} { return Thing{} } // want "CP001"

func NewFactory(useOther bool) interface{} { // want "CP001"
	if useOther {
		return privateThing{}
	}
	return Thing{}
}

func NewClosure() interface{} { // want "CP001"
	closure := func() interface{} { return privateThing{} }
	_ = closure
	return Thing{}
}

// NewFactory returns a private implementation through its public contract.
func NewFactoryContract() PublicFactory {
	return privateFactory{}
}

// NewState initializes PublicState through its constructor boundary.
func NewState(value int) PublicState {
	return PublicState{value: value}
}

type laterThing struct{} // want "CP002"

func (OtherState) BoundaryRead(state PublicState) int { // want "CP001"
	return state.value // want "CP012"
}

func (OtherState) BoundaryConstruct() PublicState { // want "CP001"
	return PublicState{value: 1} // want "CP012"
}

func (OtherState) BoundaryUnkeyed() PublicState { // want "CP001"
	return PublicState{1} // want "CP012"
}

func (Thing) First() {} // want "CP001"

func (Thing) Second() {} // want "CP001" "receiver Thing has methods in multiple files"

// Name returns the public factory name.
func (privateFactory) Name() string { return "factory" }

// LaterMethod provides the method dependency target.
func (orderThing) LaterMethod() {}

// CallsLaterMethod exercises method dependency ordering.
func (orderThing) CallsLaterMethod() { // want "CP006.*dependent CallsLaterMethod follows dependency LaterMethod"
	orderThing{}.LaterMethod()
}

func laterFunction() {}

func useLaterFunction() func() { // want "CP006.*dependent useLaterFunction follows dependency laterFunction"
	return laterFunction
}

// helperValue is shared by two higher-level functions.
func helperValue() {}

func (privateState) read(state PublicState) int {
	return state.value
}

func (state PublicState) read() int {
	return state.value
}

func wrapError(err error) error {
	return fmt.Errorf("wrap: %v", err) // want "CP013"
}

func wrapErrorPreserve(err error) error {
	return fmt.Errorf("wrap: %w", err)
}

func metricBooleanComplexity(a, b int) int { // want "CP008"
	if a > 0 && b > 0 {
		a++
		if a > 1 && b > 1 {
			a++
			if a > 2 && b > 2 {
				a++
				if a > 3 && b > 3 {
					a++
					if a > 4 && b > 4 {
						a++
						if a > 5 && b > 5 {
							a++
							if a > 6 && b > 6 {
								a++
								if a > 7 && b > 7 {
									a++
								}
							}
						}
					}
				}
			}
		}
	}
	a += 1
	a += 1
	a += 1
	a += 1
	a += 1
	a += 1
	a += 1
	a += 1
	a += 1
	return a
}

func metricComplexity(n int) int { // want "CP008"
	if n%2 == 0 {
		n++
	}
	if n%3 == 0 {
		n++
	}
	if n%5 == 0 {
		n++
	}
	if n%7 == 0 {
		n++
	}
	if n%11 == 0 {
		n++
	}
	if n%13 == 0 {
		n++
	}
	if n%17 == 0 {
		n++
	}
	if n%19 == 0 {
		n++
	}
	if n%23 == 0 {
		n++
	}
	if n%29 == 0 {
		n++
	}
	if n%31 == 0 {
		n++
	}
	if n%37 == 0 {
		n++
	}
	if n%41 == 0 {
		n++
	}
	if n%43 == 0 {
		n++
	}
	if n%47 == 0 {
		n++
	}
	return n
}

func metricFanout() { // want "CP009"
	metricFan1()
	metricFan2()
	metricFan3()
	metricFan4()
	metricFan5()
	metricFan6()
	metricFan7()
	metricFan8()
	metricFan9()
	metricFan10()
	metricFan11()
	metricFan12()
	metricFan13()
	metricFan14()
	metricFan15()
	metricFan16()
}

func metricFanoutOther() { // want "CP009"
	metricFan1()
	metricFan2()
	metricFan3()
	metricFan4()
	metricFan5()
	metricFan6()
	metricFan7()
	metricFan8()
	metricFan9()
	metricFan10()
	metricFan11()
	metricFan12()
	metricFan13()
	metricFan14()
	metricFan15()
	metricFan16()
}

// shared metrics helpers are intentionally two-use.
func metricFan1()  {}
func metricFan2()  {}
func metricFan3()  {}
func metricFan4()  {}
func metricFan5()  {}
func metricFan6()  {}
func metricFan7()  {}
func metricFan8()  {}
func metricFan9()  {}
func metricFan10() {}
func metricFan11() {}
func metricFan12() {}
func metricFan13() {}
func metricFan14() {}
func metricFan15() {}
func metricFan16() {}

func siblingAlpha(x int) int { // want "CP011"
	if x > 0 {
		x++
	}
	if x%2 == 0 {
		x++
	}
	if x%3 == 0 {
		x++
	}
	if x%5 == 0 {
		x++
	}
	if x%7 == 0 {
		x++
	}
	if x%11 == 0 {
		x++
	}
	return x
}

func siblingGap1() {}
func siblingGap2() {}
func siblingGap3() {}
func siblingGap4() {}

func siblingBeta(x int) int {
	if x > 0 {
		x++
	}
	if x%2 == 0 {
		x++
	}
	if x%3 == 0 {
		x++
	}
	if x%5 == 0 {
		x++
	}
	if x%7 == 0 {
		x++
	}
	if x%11 == 0 {
		x++
	}
	return x
}

func decodeFirst(x int) int {
	if x > 0 {
		x++
	}
	if x%2 == 0 {
		x++
	}
	if x%3 == 0 {
		x++
	}
	if x%5 == 0 {
		x++
	}
	if x%7 == 0 {
		x++
	}
	if x%11 == 0 {
		x++
	}
	if x%13 == 0 {
		x++
	}
	if x%17 == 0 {
		x++
	}
	if x%19 == 0 {
		x++
	}
	if x%23 == 0 {
		x++
	}
	return x
}

func gap() {}

func decodeSecond(x int) int {
	if x > 0 {
		x++
	}
	if x%2 == 0 {
		x++
	}
	if x%3 == 0 {
		x++
	}
	if x%5 == 0 {
		x++
	}
	if x%7 == 0 {
		x++
	}
	if x%11 == 0 {
		x++
	}
	if x%13 == 0 {
		x++
	}
	if x%17 == 0 {
		x++
	}
	if x%19 == 0 {
		x++
	}
	if x%23 == 0 {
		x++
	}
	return x
}
