package violations

var initOrderValue int

func init() {}

func beforeInit() {}

func init() {} // want "CP002.*init must appear immediately after package-level declarations"
