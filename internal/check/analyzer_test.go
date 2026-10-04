package check_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/siyul-park/minivm/internal/check"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), check.Analyzer, "violations")
}
