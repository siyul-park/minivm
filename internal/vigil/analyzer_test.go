package vigil_test

import (
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/siyul-park/minivm/internal/vigil"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), vigil.Analyzer, "violations")
}

func TestRules(t *testing.T) {
	rules := vigil.Rules()
	seen := make(map[string]bool)
	for _, text := range rules {
		parts := strings.Fields(text)
		if len(parts) < 2 {
			t.Fatalf("incomplete rule metadata: %q", text)
		}
		if seen[parts[0]] {
			t.Fatalf("duplicate rule ID: %s", parts[0])
		}
		seen[parts[0]] = true
	}
	if !seen["CP009"] {
		t.Fatal("CP009 must be registered")
	}
}
