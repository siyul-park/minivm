package arm64

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

	"github.com/siyul-park/minivm/asm"
)

func TestLDI(t *testing.T) {
	tests := []struct {
		val  uint64
		want []asm.Instruction
	}{
		{
			val:  0,
			want: []asm.Instruction{MOVZ(X0, 0, 0)},
		},
		{
			val:  0x1234,
			want: []asm.Instruction{MOVZ(X0, 0x1234, 0)},
		},
		{
			val:  0x7FF6000000000000,
			want: []asm.Instruction{MOVZ(X0, 0x7FF6, 48)},
		},
		{
			val: 0x1234000056780000,
			want: []asm.Instruction{
				MOVZ(X0, 0x5678, 16),
				MOVK(X0, 0x1234, 48),
			},
		},
		{
			val: 0x12345678,
			want: []asm.Instruction{
				MOVZ(X0, 0x5678, 0),
				MOVK(X0, 0x1234, 16),
			},
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("%#x", tt.val), func(t *testing.T) {
			require.Equal(t, tt.want, LDI(X0, tt.val))
		})
	}
}

func TestInstructionFactories(t *testing.T) {
	tests := []struct {
		name string
		op   Op
		inst asm.Instruction
	}{
		{name: "NEG", op: OpNEG, inst: NEG(X0, X1)},
		{name: "NEGS", op: OpNEGS, inst: NEGS(X0, X1)},
		{name: "MVN", op: OpMVN, inst: MVN(X0, X1)},
		{name: "TST", op: OpTST, inst: TST(X0, X1)},
		{name: "TSTI", op: OpTSTI, inst: TSTI(X0, 1)},
		{name: "LSLI", op: OpLSLI, inst: LSLI(X0, X1, 1)},
		{name: "LSRI", op: OpLSRI, inst: LSRI(X0, X1, 1)},
		{name: "ASRI", op: OpASRI, inst: ASRI(X0, X1, 1)},
		{name: "RORI", op: OpRORI, inst: RORI(X0, X1, 1)},
		{name: "REV", op: OpREV, inst: REV(X0, X1)},
		{name: "SXTB", op: OpSXTB, inst: SXTB(X0, X1)},
		{name: "SXTH", op: OpSXTH, inst: SXTH(X0, X1)},
		{name: "SXTW", op: OpSXTW, inst: SXTW(X0, X1)},
		{name: "UXTB", op: OpUXTB, inst: UXTB(X0, X1)},
		{name: "UXTH", op: OpUXTH, inst: UXTH(X0, X1)},
		{name: "UXTW", op: OpUXTW, inst: UXTW(X0, X1)},
		{name: "MOV", op: OpMOV, inst: MOV(X0, X1)},
		{name: "MOVI", op: OpMOVI, inst: MOVI(X0, 1)},
		{name: "CMP", op: OpCMP, inst: CMP(X0, X1)},
		{name: "CMPI", op: OpCMPI, inst: CMPI(X0, 1)},
		{name: "CMN", op: OpCMN, inst: CMN(X0, X1)},
		{name: "CMNI", op: OpCMNI, inst: CMNI(X0, 1)},
		{name: "CCMP", op: OpCCMP, inst: CCMP(X0, X1, 1, 1)},
		{name: "CCMPI", op: OpCCMPI, inst: CCMPI(X0, 1, 1, 1)},
		{name: "LDP", op: OpLDP, inst: LDP(X0, X1, X2, 1)},
		{name: "STP", op: OpSTP, inst: STP(X0, X1, X2, 1)},
		{name: "FCVT", op: OpFCVT, inst: FCVT(X0, X1)},
		{name: "FABS", op: OpFABS, inst: FABS(X0, X1)},
		{name: "FNEG", op: OpFNEG, inst: FNEG(X0, X1)},
		{name: "FSQRT", op: OpFSQRT, inst: FSQRT(X0, X1)},
		{name: "FRINTN", op: OpFRINTN, inst: FRINTN(X0, X1)},
		{name: "FRINTM", op: OpFRINTM, inst: FRINTM(X0, X1)},
		{name: "FRINTP", op: OpFRINTP, inst: FRINTP(X0, X1)},
		{name: "FRINTZ", op: OpFRINTZ, inst: FRINTZ(X0, X1)},
		{name: "FMOV", op: OpFMOV, inst: FMOV(X0, X1)},
		{name: "FCMP", op: OpFCMP, inst: FCMP(X0, X1)},
		{name: "FCMPE", op: OpFCMPE, inst: FCMPE(X0, X1)},
		{name: "BL", op: OpBL, inst: BL(1)},
		{name: "BR", op: OpBR, inst: BR(X0)},
		{name: "BLR", op: OpBLR, inst: BLR(X0)},
		{name: "BLabel", op: OpB, inst: BLabel(asm.Label(1))},
		{name: "BLLabel", op: OpBL, inst: BLLabel(asm.Label(1))},
		{name: "CBNZ", op: OpCBNZ, inst: CBNZ(X0, 1)},
		{name: "BNE", op: OpBNE, inst: BNE(1)},
		{name: "BLT", op: OpBLT, inst: BLT(1)},
		{name: "BGT", op: OpBGT, inst: BGT(1)},
		{name: "BLE", op: OpBLE, inst: BLE(1)},
		{name: "BGE", op: OpBGE, inst: BGE(1)},
		{name: "BMI", op: OpBMI, inst: BMI(1)},
		{name: "BPL", op: OpBPL, inst: BPL(1)},
		{name: "BVS", op: OpBVS, inst: BVS(1)},
		{name: "BVC", op: OpBVC, inst: BVC(1)},
		{name: "BHI", op: OpBHI, inst: BHI(1)},
		{name: "BLS", op: OpBLS, inst: BLS(1)},
		{name: "BCS", op: OpBCS, inst: BCS(1)},
		{name: "BCC", op: OpBCC, inst: BCC(1)},
		{name: "NOP", op: OpNOP, inst: NOP()},
		{name: "HLT", op: OpHLT, inst: HLT()},
		{name: "BRK", op: OpBRK, inst: BRK(1)},
		{name: "SVC", op: OpSVC, inst: SVC(1)},
		{name: "ERET", op: OpERET, inst: ERET()},
		{name: "MRS", op: OpMRS, inst: MRS(X0, 1)},
		{name: "MSR", op: OpMSR, inst: MSR(1, X0)},
		{name: "ISB", op: OpISB, inst: ISB()},
		{name: "DSB", op: OpDSB, inst: DSB()},
		{name: "DMB", op: OpDMB, inst: DMB()},
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
				name, ok := call.Fun.(*ast.Ident)
				if !ok {
					return true
				}
				if _, ok := factories[name.Name]; ok {
					covered[name.Name] = struct{}{}
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
