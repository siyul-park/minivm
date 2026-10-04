package violations

func crossCloneLeft(x int) int {
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
