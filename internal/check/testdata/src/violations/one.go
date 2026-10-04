package violations

import "context"

type Thing struct{} // want "CP001"

type privateThing struct{}

func Use(value int, ctx context.Context) {} // want "CP001" "CP003"

func NewThing() interface{} { return Thing{} } // want "CP001" "CP004"

type laterThing struct{} // want "CP002"

// UseHelperA exercises a cross-group dependency that file ordering does not constrain.
func UseHelperA() { helperValue() }

// UseHelperB exercises the same shared lower-level helper.
func UseHelperB() { helperValue() }

func (Thing) First() {} // want "CP001"

func (Thing) Second() {} // want "CP001" "receiver Thing has methods in multiple files"

// helperValue is shared by two higher-level functions.
func helperValue() {}

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

func metricFanout() { // want "CP009" "CP010"
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

func orderLow() {}

func orderHigh() { // want "CP012"
	orderHelp1()
	orderHelp2()
	orderHelp3()
	orderHelp4()
}

func orderOther() {
	orderHelp1()
	orderHelp2()
	orderHelp3()
	orderHelp4()
}

func orderHelp1() { orderLeaf() }
func orderHelp2() { orderLeaf() }
func orderHelp3() { orderLeaf() }
func orderHelp4() { orderLeaf() }
func orderLeaf()  {}
