package violations

func (Thing) Third() {} // want "CP001" "receiver Thing has methods in multiple files"

func helper() {} // want "CP007"

func caller() { helper() } // want "CP006.*dependent caller follows dependency helper"
