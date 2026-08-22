package arm64_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/asm"
	arm64 "github.com/siyul-park/minivm/internal/asm/arm64"
)

func TestLDI(t *testing.T) {
	tests := []struct {
		val  uint64
		want []asm.Instruction
	}{
		{
			val:  0,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0, 0)},
		},
		{
			val:  0x1234,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0x1234, 0)},
		},
		{
			val:  0x7FF6000000000000,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0x7FF6, 48)},
		},
		{
			val:  0x1234000056780000,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0x5678, 16), arm64.MOVK(arm64.X0, 0x1234, 48)},
		},
		{
			val:  0x12345678,
			want: []asm.Instruction{arm64.MOVZ(arm64.X0, 0x5678, 0), arm64.MOVK(arm64.X0, 0x1234, 16)},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%#x", tt.val), func(t *testing.T) {
			require.Equal(t, tt.want, arm64.LDI(arm64.X0, tt.val))
		})
	}
}

func TestInstructionFactories(t *testing.T) {
	tests := []struct {
		name string
		op   arm64.Op
		inst asm.Instruction
	}{
		{name: "NEG", op: arm64.OpNEG, inst: arm64.NEG(arm64.X0, arm64.X1)},
		{name: "NEGS", op: arm64.OpNEGS, inst: arm64.NEGS(arm64.X0, arm64.X1)},
		{name: "MVN", op: arm64.OpMVN, inst: arm64.MVN(arm64.X0, arm64.X1)},
		{name: "TST", op: arm64.OpTST, inst: arm64.TST(arm64.X0, arm64.X1)},
		{name: "TSTI", op: arm64.OpTSTI, inst: arm64.TSTI(arm64.X0, 1)},
		{name: "LSLI", op: arm64.OpLSLI, inst: arm64.LSLI(arm64.X0, arm64.X1, 1)},
		{name: "LSRI", op: arm64.OpLSRI, inst: arm64.LSRI(arm64.X0, arm64.X1, 1)},
		{name: "ASRI", op: arm64.OpASRI, inst: arm64.ASRI(arm64.X0, arm64.X1, 1)},
		{name: "RORI", op: arm64.OpRORI, inst: arm64.RORI(arm64.X0, arm64.X1, 1)},
		{name: "REV", op: arm64.OpREV, inst: arm64.REV(arm64.X0, arm64.X1)},
		{name: "SXTB", op: arm64.OpSXTB, inst: arm64.SXTB(arm64.X0, arm64.X1)},
		{name: "SXTH", op: arm64.OpSXTH, inst: arm64.SXTH(arm64.X0, arm64.X1)},
		{name: "SXTW", op: arm64.OpSXTW, inst: arm64.SXTW(arm64.X0, arm64.X1)},
		{name: "UXTB", op: arm64.OpUXTB, inst: arm64.UXTB(arm64.X0, arm64.X1)},
		{name: "UXTH", op: arm64.OpUXTH, inst: arm64.UXTH(arm64.X0, arm64.X1)},
		{name: "UXTW", op: arm64.OpUXTW, inst: arm64.UXTW(arm64.X0, arm64.X1)},
		{name: "MOV", op: arm64.OpMOV, inst: arm64.MOV(arm64.X0, arm64.X1)},
		{name: "MOVI", op: arm64.OpMOVI, inst: arm64.MOVI(arm64.X0, 1)},
		{name: "CMP", op: arm64.OpCMP, inst: arm64.CMP(arm64.X0, arm64.X1)},
		{name: "CMPI", op: arm64.OpCMPI, inst: arm64.CMPI(arm64.X0, 1)},
		{name: "CMN", op: arm64.OpCMN, inst: arm64.CMN(arm64.X0, arm64.X1)},
		{name: "CMNI", op: arm64.OpCMNI, inst: arm64.CMNI(arm64.X0, 1)},
		{name: "CCMP", op: arm64.OpCCMP, inst: arm64.CCMP(arm64.X0, arm64.X1, 1, 1)},
		{name: "CCMPI", op: arm64.OpCCMPI, inst: arm64.CCMPI(arm64.X0, 1, 1, 1)},
		{name: "LDP", op: arm64.OpLDP, inst: arm64.LDP(arm64.X0, arm64.X1, arm64.X2, 1)},
		{name: "STP", op: arm64.OpSTP, inst: arm64.STP(arm64.X0, arm64.X1, arm64.X2, 1)},
		{name: "FCVT", op: arm64.OpFCVT, inst: arm64.FCVT(arm64.X0, arm64.X1)},
		{name: "FABS", op: arm64.OpFABS, inst: arm64.FABS(arm64.X0, arm64.X1)},
		{name: "FNEG", op: arm64.OpFNEG, inst: arm64.FNEG(arm64.X0, arm64.X1)},
		{name: "FSQRT", op: arm64.OpFSQRT, inst: arm64.FSQRT(arm64.X0, arm64.X1)},
		{name: "FRINTN", op: arm64.OpFRINTN, inst: arm64.FRINTN(arm64.X0, arm64.X1)},
		{name: "FRINTM", op: arm64.OpFRINTM, inst: arm64.FRINTM(arm64.X0, arm64.X1)},
		{name: "FRINTP", op: arm64.OpFRINTP, inst: arm64.FRINTP(arm64.X0, arm64.X1)},
		{name: "FRINTZ", op: arm64.OpFRINTZ, inst: arm64.FRINTZ(arm64.X0, arm64.X1)},
		{name: "FMOV", op: arm64.OpFMOV, inst: arm64.FMOV(arm64.X0, arm64.X1)},
		{name: "FCMP", op: arm64.OpFCMP, inst: arm64.FCMP(arm64.X0, arm64.X1)},
		{name: "FCMPE", op: arm64.OpFCMPE, inst: arm64.FCMPE(arm64.X0, arm64.X1)},
		{name: "BL", op: arm64.OpBL, inst: arm64.BL(1)},
		{name: "BR", op: arm64.OpBR, inst: arm64.BR(arm64.X0)},
		{name: "BLR", op: arm64.OpBLR, inst: arm64.BLR(arm64.X0)},
		{name: "BLabel", op: arm64.OpB, inst: arm64.BLabel(asm.Label(1))},
		{name: "BLLabel", op: arm64.OpBL, inst: arm64.BLLabel(asm.Label(1))},
		{name: "CBNZ", op: arm64.OpCBNZ, inst: arm64.CBNZ(arm64.X0, 1)},
		{name: "BNE", op: arm64.OpBNE, inst: arm64.BNE(1)},
		{name: "BLT", op: arm64.OpBLT, inst: arm64.BLT(1)},
		{name: "BGT", op: arm64.OpBGT, inst: arm64.BGT(1)},
		{name: "BLE", op: arm64.OpBLE, inst: arm64.BLE(1)},
		{name: "BGE", op: arm64.OpBGE, inst: arm64.BGE(1)},
		{name: "BMI", op: arm64.OpBMI, inst: arm64.BMI(1)},
		{name: "BPL", op: arm64.OpBPL, inst: arm64.BPL(1)},
		{name: "BVS", op: arm64.OpBVS, inst: arm64.BVS(1)},
		{name: "BVC", op: arm64.OpBVC, inst: arm64.BVC(1)},
		{name: "BHI", op: arm64.OpBHI, inst: arm64.BHI(1)},
		{name: "BLS", op: arm64.OpBLS, inst: arm64.BLS(1)},
		{name: "BCS", op: arm64.OpBCS, inst: arm64.BCS(1)},
		{name: "BCC", op: arm64.OpBCC, inst: arm64.BCC(1)},
		{name: "NOP", op: arm64.OpNOP, inst: arm64.NOP()},
		{name: "HLT", op: arm64.OpHLT, inst: arm64.HLT()},
		{name: "BRK", op: arm64.OpBRK, inst: arm64.BRK(1)},
		{name: "SVC", op: arm64.OpSVC, inst: arm64.SVC(1)},
		{name: "ERET", op: arm64.OpERET, inst: arm64.ERET()},
		{name: "MRS", op: arm64.OpMRS, inst: arm64.MRS(arm64.X0, 1)},
		{name: "MSR", op: arm64.OpMSR, inst: arm64.MSR(1, arm64.X0)},
		{name: "ISB", op: arm64.OpISB, inst: arm64.ISB()},
		{name: "DSB", op: arm64.OpDSB, inst: arm64.DSB()},
		{name: "DMB", op: arm64.OpDMB, inst: arm64.DMB()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, uint16(tt.op), tt.inst.Op)
			require.NotEmpty(t, tt.inst.String())
		})
	}

	t.Run("covers every exported instruction factory", func(t *testing.T) {
		_, file, _, ok := runtime.Caller(0)
		require.True(t, ok)
		dir := filepath.Dir(file)
		set := token.NewFileSet()

		production, err := parser.ParseFile(set, filepath.Join(dir, "instr.go"), nil, 0)
		require.NoError(t, err)
		factories := make(map[string]struct{})
		for _, decl := range production.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !fn.Name.IsExported() {
				continue
			}
			factories[fn.Name.Name] = struct{}{}
		}

		covered := make(map[string]struct{})
		files, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
		require.NoError(t, err)
		for _, path := range files {
			file, err := parser.ParseFile(set, path, nil, 0)
			require.NoError(t, err)
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				// Tests live both in this package (Name) and beside it
				// (arm64.Name), so accept either call shape.
				var name string
				switch fn := call.Fun.(type) {
				case *ast.Ident:
					name = fn.Name
				case *ast.SelectorExpr:
					name = fn.Sel.Name
				default:
					return true
				}
				if _, ok := factories[name]; ok {
					covered[name] = struct{}{}
				}
				return true
			})
		}

		var missing []string
		for name := range factories {
			if _, ok := covered[name]; !ok {
				missing = append(missing, name)
			}
		}
		slices.Sort(missing)
		require.Empty(t, missing)
	})
}
