package violations

func (Thing) Third() {} // want "CP001"

func (Thing) String() string { return "" } // want "CP001"

func (Thing) Later() {} // want "CP001" "receiver Thing has methods in multiple files" "CP002"

func helper() {} // want "CP007"

func caller() { helper() } // want "CP006.*dependent caller follows dependency helper"

func recursiveB() { recursiveA() } // want "CP007"

func recursiveA() { recursiveB() } // want "CP007"

// documentedHelper is a single-use policy mechanic and may remain named.
func documentedHelper() {}

func documentedCaller() { documentedHelper() } // want "CP006.*dependent documentedCaller follows dependency documentedHelper"
