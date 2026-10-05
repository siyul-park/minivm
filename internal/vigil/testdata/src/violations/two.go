package violations

func (Thing) Third() {} // want "CP001"

func (Thing) String() string { return "" } // want "CP001"

func (Thing) Later() {} // want "CP001" "receiver Thing has methods in multiple files" "CP002"

func helper() { helperImpl() } // want "CP007"

func helperImpl() {}

func constantWrapper() { helperImplValue(1) }

func helperImplValue(value int) {}

func caller() { helper() } // want "CP006.*dependent caller follows dependency helper"

func recursiveB() { recursiveA() }

func recursiveA() { recursiveB() }

// documentedHelper is a single-use policy mechanic and may remain named.
func documentedHelper() {}

func documentedCaller() { documentedHelper() } // want "CP006.*dependent documentedCaller follows dependency documentedHelper"
