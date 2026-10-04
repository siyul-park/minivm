package violations

import "testing"

func TestMixed(t *testing.T) { // want "TP006"
	t.Run("case", func(t *testing.T) {
		t.Helper()
	})
	t.Fatal("direct assertion outside case")
}
