//go:build arm64

package interp

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/siyul-park/minivm/types"
)

func TestMergeCFGSlot(t *testing.T) {
	dst := cfgSlot{kind: types.KindRef, ref: 7, refKnown: true}
	changed, ok := mergeCFGSlot(&dst, cfgSlot{kind: types.KindRef, ref: 8, refKnown: true})
	if !ok || !changed || dst.refKnown {
		t.Fatalf("merge = (%v, %v), slot = %+v", changed, ok, dst)
	}
	if _, ok := mergeCFGSlot(&dst, cfgSlot{kind: types.KindI32}); ok {
		t.Fatal("kind mismatch accepted")
	}
}

func TestJITOpcodeDispatchIsShared(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	files, err := filepath.Glob(filepath.Join(filepath.Dir(file), "jit_arm64*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var source strings.Builder
	for _, name := range files {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source.Write(data)
	}
	text := source.String()
	if got := strings.Count(text, "func (l arm64Lowerer) emitStep("); got != 1 {
		t.Fatalf("emitStep definitions = %d, want 1", got)
	}
	for _, obsolete := range []string{"cfgOp(", "cfgConstGet(", "cfgArrayGet("} {
		if strings.Contains(text, obsolete) {
			t.Fatalf("obsolete dispatch helper %q remains", obsolete)
		}
	}
}
