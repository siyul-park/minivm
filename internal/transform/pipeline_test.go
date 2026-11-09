package transform_test

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/stretchr/testify/require"
)

func names(passes []pass.Pass[*ssa.Function]) []string {
	out := make([]string, len(passes))
	for i, p := range passes {
		out[i] = fmt.Sprintf("%T", p)
	}
	return out
}

func TestO1(t *testing.T) {
	require.Equal(t, names([]pass.Pass[*ssa.Function]{transform.NewFoldPass(), transform.NewDCEPass()}), names(transform.O1()))
}

func TestO2(t *testing.T) {
	require.Equal(t, names([]pass.Pass[*ssa.Function]{
		transform.NewFoldPass(), transform.NewCSEPass(), transform.NewGuardPass(), transform.NewDCEPass(),
	}), names(transform.O2()))
}

func TestO3(t *testing.T) {
	base := []pass.Pass[*ssa.Function]{
		transform.NewFoldPass(), transform.NewForwardPass(), transform.NewCSEPass(), transform.NewGuardPass(),
		transform.NewDCEPass(), transform.NewPromotePass(), transform.NewLICMPass(),
	}

	t.Run("ends with dead-code removal", func(t *testing.T) {
		require.Equal(t, names(append(base[:len(base):len(base)], transform.NewDCEPass())), names(transform.O3()))
	})

	t.Run("runs late passes before the last dead-code removal", func(t *testing.T) {
		late := transform.NewBCEPass(transform.Profiles{})
		want := append(base[:len(base):len(base)], late, transform.NewDCEPass())
		require.Equal(t, names(want), names(transform.O3(late)))
	})
}
