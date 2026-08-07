package benchmarks_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkNumeric_BranchTree(b *testing.B) {
	const input int32 = 37
	const nodes = 96
	prog, want := branchTree(input, nodes)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			var total int32
			for index := range nodes {
				threshold := int32((index*17 + 11) % 97)
				if input < threshold {
					total += int32(index%7 + 1)
				} else {
					total += int32(index%5 + 2)
				}
			}
			return total
		},
		wazero: "branch_tree",
		args:   []uint64{uint64(uint32(input)), uint64(uint32(nodes))},
		scripts: benchmarkScripts{
			tengo:     fmt.Sprintf(`result := func() { total := 0; for index := 0; index < %d; index++ { threshold := (index*17+11) %% 97; if %d < threshold { total += index %% 7 + 1 } else { total += index %% 5 + 2 } }; return total }()`, nodes, input),
			gopherLua: fmt.Sprintf(`function run() local total = 0; for index = 0, %d - 1 do local threshold = (index*17+11) %% 97; if %d < threshold then total = total + index %% 7 + 1 else total = total + index %% 5 + 2 end end; return total end`, nodes, input),
			goja:      fmt.Sprintf(`function run() { let total = 0; for (let index = 0; index < %d; index++) { const threshold = (index*17+11) %% 97; total += %d < threshold ? index %% 7 + 1 : index %% 5 + 2; } return total; }`, nodes, input),
			gpython: fmt.Sprintf(`def run():
    total = 0
    for index in range(%d):
        threshold = (index * 17 + 11) %% 97
        if %d < threshold: total += index %% 7 + 1
        else: total += index %% 5 + 2
    return total`, nodes, input),
			yaegi: fmt.Sprintf(`package bench
func Run() int32 { var total int32; for index := 0; index < %d; index++ { threshold := int32((index*17+11) %% 97); if %d < threshold { total += int32(index%%7+1) } else { total += int32(index%%5+2) } }; return total }`, nodes, input),
		},
	}, want)
}

func BenchmarkNumeric_NBody(b *testing.B) {
	const steps int32 = 100
	want := nbodyReference(steps)
	prog := nbody(steps)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	script := fmt.Sprintf(`import math


def advance(nb, x, y, z, vx, vy, vz, mass, dt):
    i = 0
    while i < nb:
        j = i + 1
        while j < nb:
            dx = x[i] - x[j]
            dy = y[i] - y[j]
            dz = z[i] - z[j]
            dist2 = dx * dx + dy * dy + dz * dz
            mag = dt / (dist2 * math.sqrt(dist2))
            vx[i] = vx[i] - dx * mass[j] * mag
            vy[i] = vy[i] - dy * mass[j] * mag
            vz[i] = vz[i] - dz * mass[j] * mag
            vx[j] = vx[j] + dx * mass[i] * mag
            vy[j] = vy[j] + dy * mass[i] * mag
            vz[j] = vz[j] + dz * mass[i] * mag
            j = j + 1
        i = i + 1
    i = 0
    while i < nb:
        x[i] = x[i] + dt * vx[i]
        y[i] = y[i] + dt * vy[i]
        z[i] = z[i] + dt * vz[i]
        i = i + 1


def energy(nb, x, y, z, vx, vy, vz, mass):
    e = 0.0
    i = 0
    while i < nb:
        e = e + 0.5 * mass[i] * (vx[i] * vx[i] + vy[i] * vy[i] + vz[i] * vz[i])
        j = i + 1
        while j < nb:
            dx = x[i] - x[j]
            dy = y[i] - y[j]
            dz = z[i] - z[j]
            dist = math.sqrt(dx * dx + dy * dy + dz * dz)
            e = e - (mass[i] * mass[j]) / dist
            j = j + 1
        i = i + 1
    return e


def offset_momentum(nb, vx, vy, vz, mass, solar_mass):
    px = 0.0
    py = 0.0
    pz = 0.0
    i = 0
    while i < nb:
        px = px + vx[i] * mass[i]
        py = py + vy[i] * mass[i]
        pz = pz + vz[i] * mass[i]
        i = i + 1
    vx[0] = 0.0 - px / solar_mass
    vy[0] = 0.0 - py / solar_mass
    vz[0] = 0.0 - pz / solar_mass


def run():
    pi = 3.141592653589793
    solar_mass = 4.0 * pi * pi
    days_per_year = 365.24

    x = [0.0, 4.84143144246472090, 8.34336671824457987, 12.94350551331783510, 15.37969711485094510]
    y = [0.0, -1.16032004402742839, 4.12479856412430479, -15.11151401698631891, -25.91931460998796403]
    z = [0.0, -0.10362204447112311, -0.40352341711432138, -0.22370579633577680, 0.17925877295037118]

    vx = [0.0, 0.00166007664274403 * days_per_year, 0.00283009096225471 * days_per_year, 0.00296460137564761 * days_per_year, 0.00268067772490389 * days_per_year]
    vy = [0.0, 0.00769901118419740 * days_per_year, 0.00453000209594919 * days_per_year, 0.00237847173959480 * days_per_year, 0.00162824170038242 * days_per_year]
    vz = [0.0, -0.00006902509938426 * days_per_year, -0.00019131288713706 * days_per_year, -0.00029589288865580 * days_per_year, -0.00095159225451337 * days_per_year]

    mass = [solar_mass, 9.54791938424326609e-04 * solar_mass, 2.85885980666130812e-04 * solar_mass, 4.36624404335156298e-05 * solar_mass, 5.15138902046611451e-05 * solar_mass]

    nb = 5
    offset_momentum(nb, vx, vy, vz, mass, solar_mass)

    steps = 0
    n = %d
    dt = 0.01
    while steps < n:
        advance(nb, x, y, z, vx, vy, vz, mass, dt)
        steps = steps + 1

    e = energy(nb, x, y, z, vx, vy, vz, mass)
    return int(e * 1e9)`, steps)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return nbodyReference(steps) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

func BenchmarkNumeric_SpectralNorm(b *testing.B) {
	const n int32 = 24
	const rounds int32 = 2
	want := spectralnormReference(n, rounds)
	prog := spectralnorm(n, rounds)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	script := fmt.Sprintf(`import math


def eval_a(i, j):
    return 1.0 / float((i + j) * (i + j + 1) // 2 + i + 1)


def eval_a_times_u(n, u, out):
    i = 0
    while i < n:
        s = 0.0
        j = 0
        while j < n:
            s = s + eval_a(i, j) * u[j]
            j = j + 1
        out[i] = s
        i = i + 1


def eval_at_times_u(n, u, out):
    i = 0
    while i < n:
        s = 0.0
        j = 0
        while j < n:
            s = s + eval_a(j, i) * u[j]
            j = j + 1
        out[i] = s
        i = i + 1


def eval_ata_times_u(n, u, out, tmp):
    eval_a_times_u(n, u, tmp)
    eval_at_times_u(n, tmp, out)


def run():
    n = %d
    u = [1.0] * n
    v = [0.0] * n
    tmp = [0.0] * n

    it = 0
    while it < %d:
        eval_ata_times_u(n, u, v, tmp)
        eval_ata_times_u(n, v, u, tmp)
        it = it + 1

    vbv = 0.0
    vv = 0.0
    i = 0
    while i < n:
        vbv = vbv + u[i] * v[i]
        vv = vv + v[i] * v[i]
        i = i + 1

    result = math.sqrt(vbv / vv)
    return int(result * 1e9)`, n, rounds)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return spectralnormReference(n, rounds) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

func BenchmarkNumeric_Mandelbrot(b *testing.B) {
	const width, height int32 = 16, 16
	const maxIter int32 = 50
	want := mandelbrotReference(width, height, maxIter)
	prog := mandelbrot(width, height, maxIter)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	script := fmt.Sprintf(`def escape_count(cr, ci, max_iter):
    zr = 0.0
    zi = 0.0
    i = 0
    while i < max_iter:
        zr2 = zr * zr
        zi2 = zi * zi
        if zr2 + zi2 > 4.0:
            return i
        new_zr = zr2 - zi2 + cr
        new_zi = 2.0 * zr * zi + ci
        zr = new_zr
        zi = new_zi
        i = i + 1
    return max_iter


def run():
    width = %d
    height = %d
    max_iter = %d
    x_min = -2.0
    x_max = 1.0
    y_min = -1.5
    y_max = 1.5

    total = 0
    py = 0
    while py < height:
        cy = y_min + (y_max - y_min) * float(py) / float(height - 1)
        px = 0
        while px < width:
            cx = x_min + (x_max - x_min) * float(px) / float(width - 1)
            total = total + escape_count(cx, cy, max_iter)
            px = px + 1
        py = py + 1

    return total`, width, height, maxIter)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return mandelbrotReference(width, height, maxIter) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

func BenchmarkNumeric_MatMul(b *testing.B) {
	const n int32 = 16
	want := matmulReference(n)
	prog := matmul(n)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	script := fmt.Sprintf(`def matmul(n, a, b, out):
    i = 0
    while i < n:
        j = 0
        while j < n:
            s = 0.0
            k = 0
            while k < n:
                s = s + a[i * n + k] * b[k * n + j]
                k = k + 1
            out[i * n + j] = s
            j = j + 1
        i = i + 1


def run():
    n = %d
    a = [0.0] * (n * n)
    b = [0.0] * (n * n)
    out = [0.0] * (n * n)

    i = 0
    while i < n:
        j = 0
        while j < n:
            a[i * n + j] = float((i * 7 + j * 3) %% 13) - 6.0
            b[i * n + j] = float((i * 5 + j * 11) %% 17) - 8.0
            j = j + 1
        i = i + 1

    matmul(n, a, b, out)

    checksum = 0.0
    idx = 0
    while idx < n * n:
        checksum = checksum + out[idx]
        idx = idx + 1

    return int(checksum * 1e6)`, n)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return matmulReference(n) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

func branchTree(input int32, nodes int) (*program.Program, int32) {
	b := program.NewBuilder()
	b.Locals(types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, uint64(uint32(input))).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	var want int32
	for index := range nodes {
		left := b.Label()
		join := b.Label()
		threshold := int32((index*17 + 11) % 97)
		leftValue := int32(index%7 + 1)
		rightValue := int32(index%5 + 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(threshold))).Emit(instr.I32_LT_S).BrIf(left)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(rightValue))).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(join)
		b.Bind(left)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(leftValue))).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(join)
		if input < threshold {
			want += leftValue
		} else {
			want += rightValue
		}
	}
	b.Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog, want
}

// nbody builds the benchmarks-game N-body kernel: offset_momentum runs once,
// advance runs steps times as a called function (it iterates in the source's
// own loop), and energy's checksum is inlined since it has one call site.
func nbody(steps int32) *program.Program {
	b := program.NewBuilder()
	arrayType := b.Type(types.TypeF64Array)

	// advance params: 0=nb,1=x,2=y,3=z,4=vx,5=vy,6=vz,7=mass,8=dt
	// advance locals: 9=i,10=j,11=dx,12=dy,13=dz,14=dist2,15=mag
	advanceBuilder := types.NewFunctionBuilder(&types.FunctionType{}).
		Params(types.TypeI32, types.TypeF64Array, types.TypeF64Array, types.TypeF64Array, types.TypeF64Array, types.TypeF64Array, types.TypeF64Array, types.TypeF64Array, types.TypeF64).
		Locals(types.TypeI32, types.TypeI32, types.TypeF64, types.TypeF64, types.TypeF64, types.TypeF64, types.TypeF64)
	pairLoop := advanceBuilder.Label()
	pairDone := advanceBuilder.Label()
	bodyLoop := advanceBuilder.Label()
	bodyDone := advanceBuilder.Label()
	settleLoop := advanceBuilder.Label()
	settleDone := advanceBuilder.Label()
	advanceFn := advanceBuilder.
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 9)).
		Bind(pairLoop).
		Emit(instr.New(instr.LOCAL_GET, 9), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).
		BrIf(pairDone).
		Emit(instr.New(instr.LOCAL_GET, 9), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 10)).
		Bind(bodyLoop).
		Emit(instr.New(instr.LOCAL_GET, 10), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).
		BrIf(bodyDone).
		Emit(
			// dx,dy,dz = x[i]-x[j], y[i]-y[j], z[i]-z[j]
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET),
			instr.New(instr.F64_SUB), instr.New(instr.LOCAL_SET, 11),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET),
			instr.New(instr.F64_SUB), instr.New(instr.LOCAL_SET, 12),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET),
			instr.New(instr.F64_SUB), instr.New(instr.LOCAL_SET, 13),
			// dist2 = dx*dx + dy*dy + dz*dz
			instr.New(instr.LOCAL_GET, 11), instr.New(instr.LOCAL_GET, 11), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 12), instr.New(instr.LOCAL_GET, 12), instr.New(instr.F64_MUL), instr.New(instr.F64_ADD),
			instr.New(instr.LOCAL_GET, 13), instr.New(instr.LOCAL_GET, 13), instr.New(instr.F64_MUL), instr.New(instr.F64_ADD),
			instr.New(instr.LOCAL_SET, 14),
			// mag = dt / (dist2 * sqrt(dist2))
			instr.New(instr.LOCAL_GET, 8),
			instr.New(instr.LOCAL_GET, 14), instr.New(instr.LOCAL_GET, 14), instr.New(instr.F64_SQRT), instr.New(instr.F64_MUL),
			instr.New(instr.F64_DIV), instr.New(instr.LOCAL_SET, 15),
			// vx[i],vy[i],vz[i] -= d*mass[j]*mag
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 9),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 11), instr.New(instr.LOCAL_GET, 7), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 15), instr.New(instr.F64_MUL),
			instr.New(instr.F64_SUB), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 9),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 12), instr.New(instr.LOCAL_GET, 7), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 15), instr.New(instr.F64_MUL),
			instr.New(instr.F64_SUB), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 9),
			instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 13), instr.New(instr.LOCAL_GET, 7), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 15), instr.New(instr.F64_MUL),
			instr.New(instr.F64_SUB), instr.New(instr.ARRAY_SET),
			// vx[j],vy[j],vz[j] += d*mass[i]*mag
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 10),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 11), instr.New(instr.LOCAL_GET, 7), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 15), instr.New(instr.F64_MUL),
			instr.New(instr.F64_ADD), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 10),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 12), instr.New(instr.LOCAL_GET, 7), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 15), instr.New(instr.F64_MUL),
			instr.New(instr.F64_ADD), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 10),
			instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 10), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 13), instr.New(instr.LOCAL_GET, 7), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 15), instr.New(instr.F64_MUL),
			instr.New(instr.F64_ADD), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 10), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 10),
		).
		Br(bodyLoop).
		Bind(bodyDone).
		Emit(instr.New(instr.LOCAL_GET, 9), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 9)).
		Br(pairLoop).
		Bind(pairDone).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 9)).
		Bind(settleLoop).
		Emit(instr.New(instr.LOCAL_GET, 9), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).
		BrIf(settleDone).
		Emit(
			// x[i],y[i],z[i] += dt*v[i]
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 9),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 8), instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.F64_ADD), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 9),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 8), instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.F64_ADD), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 9),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 8), instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 9), instr.New(instr.ARRAY_GET), instr.New(instr.F64_MUL),
			instr.New(instr.F64_ADD), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 9), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 9),
		).
		Br(settleLoop).
		Bind(settleDone).
		Emit(instr.New(instr.RETURN)).
		MustBuild()
	advanceIdx := b.Const(advanceFn)

	// main locals: 0=x,1=y,2=z,3=vx,4=vy,5=vz,6=mass,7=pi,8=solarMass,9=daysPerYear,
	// 10=px,11=py,12=pz,13=i,14=steps,15=j,16=e,17=dx,18=dy,19=dz,20=dist
	b.Locals(
		types.TypeF64Array, types.TypeF64Array, types.TypeF64Array,
		types.TypeF64Array, types.TypeF64Array, types.TypeF64Array, types.TypeF64Array,
		types.TypeF64, types.TypeF64, types.TypeF64,
		types.TypeF64, types.TypeF64, types.TypeF64,
		types.TypeI32, types.TypeI32, types.TypeI32,
		types.TypeF64, types.TypeF64, types.TypeF64, types.TypeF64, types.TypeF64,
	)

	// pi, solar_mass, days_per_year
	b.Emit(instr.F64_CONST, math.Float64bits(3.141592653589793)).Emit(instr.LOCAL_SET, 7)
	b.Emit(instr.F64_CONST, math.Float64bits(4.0)).Emit(instr.LOCAL_GET, 7).Emit(instr.F64_MUL).Emit(instr.LOCAL_GET, 7).Emit(instr.F64_MUL).Emit(instr.LOCAL_SET, 8)
	b.Emit(instr.F64_CONST, math.Float64bits(365.24)).Emit(instr.LOCAL_SET, 9)

	// x, y, z (plain literals)
	b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
	arraySet(b, 0, 0, 0.0)
	arraySet(b, 0, 1, 4.84143144246472090)
	arraySet(b, 0, 2, 8.34336671824457987)
	arraySet(b, 0, 3, 12.94350551331783510)
	arraySet(b, 0, 4, 15.37969711485094510)

	b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 1)
	arraySet(b, 1, 0, 0.0)
	arraySet(b, 1, 1, -1.16032004402742839)
	arraySet(b, 1, 2, 4.12479856412430479)
	arraySet(b, 1, 3, -15.11151401698631891)
	arraySet(b, 1, 4, -25.91931460998796403)

	b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 2)
	arraySet(b, 2, 0, 0.0)
	arraySet(b, 2, 1, -0.10362204447112311)
	arraySet(b, 2, 2, -0.40352341711432138)
	arraySet(b, 2, 3, -0.22370579633577680)
	arraySet(b, 2, 4, 0.17925877295037118)

	// vx, vy, vz (literal * days_per_year, index 0 stays 0.0)
	b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 3)
	arraySet(b, 3, 0, 0.0)
	arraySetScaled(b, 3, 1, 0.00166007664274403, 9)
	arraySetScaled(b, 3, 2, 0.00283009096225471, 9)
	arraySetScaled(b, 3, 3, 0.00296460137564761, 9)
	arraySetScaled(b, 3, 4, 0.00268067772490389, 9)

	b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 4)
	arraySet(b, 4, 0, 0.0)
	arraySetScaled(b, 4, 1, 0.00769901118419740, 9)
	arraySetScaled(b, 4, 2, 0.00453000209594919, 9)
	arraySetScaled(b, 4, 3, 0.00237847173959480, 9)
	arraySetScaled(b, 4, 4, 0.00162824170038242, 9)

	b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 5)
	arraySet(b, 5, 0, 0.0)
	arraySetScaled(b, 5, 1, -0.00006902509938426, 9)
	arraySetScaled(b, 5, 2, -0.00019131288713706, 9)
	arraySetScaled(b, 5, 3, -0.00029589288865580, 9)
	arraySetScaled(b, 5, 4, -0.00095159225451337, 9)

	// mass (index 0 is solar_mass itself, others scale it)
	b.Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 6)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 8).Emit(instr.ARRAY_SET)
	arraySetScaled(b, 6, 1, 9.54791938424326609e-04, 8)
	arraySetScaled(b, 6, 2, 2.85885980666130812e-04, 8)
	arraySetScaled(b, 6, 3, 4.36624404335156298e-05, 8)
	arraySetScaled(b, 6, 4, 5.15138902046611451e-05, 8)

	// offset_momentum(nb=5, vx, vy, vz, mass, solar_mass)
	momentumLoop := b.Label()
	momentumDone := b.Label()
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_SET, 10)
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_SET, 11)
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_SET, 12)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 13)
	b.Bind(momentumLoop)
	b.Emit(instr.LOCAL_GET, 13).Emit(instr.I32_CONST, 5).Emit(instr.I32_GE_S).BrIf(momentumDone)
	b.Emit(instr.LOCAL_GET, 10)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL).Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 10)
	b.Emit(instr.LOCAL_GET, 11)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL).Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 11)
	b.Emit(instr.LOCAL_GET, 12)
	b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL).Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 12)
	b.Emit(instr.LOCAL_GET, 13).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 13)
	b.Br(momentumLoop)
	b.Bind(momentumDone)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0)
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_GET, 10).Emit(instr.LOCAL_GET, 8).Emit(instr.F64_DIV).Emit(instr.F64_SUB)
	b.Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 0)
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_GET, 11).Emit(instr.LOCAL_GET, 8).Emit(instr.F64_DIV).Emit(instr.F64_SUB)
	b.Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 5).Emit(instr.I32_CONST, 0)
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_GET, 12).Emit(instr.LOCAL_GET, 8).Emit(instr.F64_DIV).Emit(instr.F64_SUB)
	b.Emit(instr.ARRAY_SET)

	// steps x advance(nb, x, y, z, vx, vy, vz, mass, dt)
	stepsLoop := b.Label()
	stepsDone := b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 14)
	b.Bind(stepsLoop)
	b.Emit(instr.LOCAL_GET, 14).Emit(instr.I32_CONST, uint64(uint32(steps))).Emit(instr.I32_GE_S).BrIf(stepsDone)
	b.Emit(instr.I32_CONST, 5)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 6)
	b.Emit(instr.F64_CONST, math.Float64bits(0.01))
	b.Emit(instr.CONST_GET, uint64(advanceIdx))
	b.Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 14).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 14)
	b.Br(stepsLoop)
	b.Bind(stepsDone)

	// e = energy(nb, x, y, z, vx, vy, vz, mass)
	energyOuter := b.Label()
	energyOuterDone := b.Label()
	energyInner := b.Label()
	energyInnerDone := b.Label()
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_SET, 16)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 13)
	b.Bind(energyOuter)
	b.Emit(instr.LOCAL_GET, 13).Emit(instr.I32_CONST, 5).Emit(instr.I32_GE_S).BrIf(energyOuterDone)
	b.Emit(instr.LOCAL_GET, 16)
	b.Emit(instr.F64_CONST, math.Float64bits(0.5))
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL).Emit(instr.F64_ADD)
	b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL).Emit(instr.F64_ADD)
	b.Emit(instr.F64_MUL)
	b.Emit(instr.F64_ADD)
	b.Emit(instr.LOCAL_SET, 16)
	b.Emit(instr.LOCAL_GET, 13).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 15)
	b.Bind(energyInner)
	b.Emit(instr.LOCAL_GET, 15).Emit(instr.I32_CONST, 5).Emit(instr.I32_GE_S).BrIf(energyInnerDone)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 15).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_SUB).Emit(instr.LOCAL_SET, 17)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 15).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_SUB).Emit(instr.LOCAL_SET, 18)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 15).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_SUB).Emit(instr.LOCAL_SET, 19)
	b.Emit(instr.LOCAL_GET, 17).Emit(instr.LOCAL_GET, 17).Emit(instr.F64_MUL)
	b.Emit(instr.LOCAL_GET, 18).Emit(instr.LOCAL_GET, 18).Emit(instr.F64_MUL).Emit(instr.F64_ADD)
	b.Emit(instr.LOCAL_GET, 19).Emit(instr.LOCAL_GET, 19).Emit(instr.F64_MUL).Emit(instr.F64_ADD)
	b.Emit(instr.F64_SQRT).Emit(instr.LOCAL_SET, 20)
	b.Emit(instr.LOCAL_GET, 16)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 13).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 15).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL)
	b.Emit(instr.LOCAL_GET, 20).Emit(instr.F64_DIV)
	b.Emit(instr.F64_SUB).Emit(instr.LOCAL_SET, 16)
	b.Emit(instr.LOCAL_GET, 15).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 15)
	b.Br(energyInner)
	b.Bind(energyInnerDone)
	b.Emit(instr.LOCAL_GET, 13).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 13)
	b.Br(energyOuter)
	b.Bind(energyOuterDone)

	b.Emit(instr.LOCAL_GET, 16)
	b.Emit(instr.F64_CONST, math.Float64bits(1e9))
	b.Emit(instr.F64_MUL)
	b.Emit(instr.F64_TO_I32_S)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// arraySet emits array[idx] = value for a local f64 array and a literal.
func arraySet(b *program.Builder, array, idx int, value float64) {
	b.Emit(instr.LOCAL_GET, uint64(array)).Emit(instr.I32_CONST, uint64(uint32(idx))).Emit(instr.F64_CONST, math.Float64bits(value)).Emit(instr.ARRAY_SET)
}

// arraySetScaled emits array[idx] = component * local, matching an
// initializer that scales a literal by a runtime variable.
func arraySetScaled(b *program.Builder, array, idx int, component float64, scale int) {
	b.Emit(instr.LOCAL_GET, uint64(array)).Emit(instr.I32_CONST, uint64(uint32(idx)))
	b.Emit(instr.F64_CONST, math.Float64bits(component)).Emit(instr.LOCAL_GET, uint64(scale)).Emit(instr.F64_MUL)
	b.Emit(instr.ARRAY_SET)
}

// spectralnorm builds the benchmarks-game spectral-norm kernel. eval_a,
// eval_a_times_u, and eval_at_times_u each have several call sites, so they
// stay real bytecode functions; eval_ata_times_u has one call site (the round
// loop body) and is inlined into it.
func spectralnorm(n, rounds int32) *program.Program {
	b := program.NewBuilder()
	arrayType := b.Type(types.TypeF64Array)

	// eval_a params: 0=i,1=j
	evalAFn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeF64}}).
		Params(types.TypeI32, types.TypeI32).
		Emit(
			instr.New(instr.F64_CONST, math.Float64bits(1.0)),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.I32_MUL),
			instr.New(instr.I32_CONST, 2), instr.New(instr.I32_DIV_S),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.I32_ADD),
			instr.New(instr.I32_TO_F64_S),
			instr.New(instr.F64_DIV),
			instr.New(instr.RETURN),
		).
		MustBuild()
	evalAIdx := b.Const(evalAFn)

	// eval_a_times_u / eval_at_times_u params: 0=n,1=u,2=out; locals: 3=i,4=j,5=s
	aTimesUFn := spectralnormTimesU(evalAIdx, false)
	aTimesUIdx := b.Const(aTimesUFn)
	atTimesUFn := spectralnormTimesU(evalAIdx, true)
	atTimesUIdx := b.Const(atTimesUFn)

	// main locals: 0=u,1=v,2=tmp,3=it,4=vbv,5=vv,6=i
	b.Locals(types.TypeF64Array, types.TypeF64Array, types.TypeF64Array, types.TypeI32, types.TypeF64, types.TypeF64, types.TypeI32)

	// u = [1.0] * n
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.F64_CONST, math.Float64bits(1.0)).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.ARRAY_FILL)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 2)

	itLoop := b.Label()
	itDone := b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Bind(itLoop)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(rounds))).Emit(instr.I32_GE_S).BrIf(itDone)
	// eval_ata_times_u(n, u, v, tmp): eval_a_times_u(n, u, tmp); eval_at_times_u(n, tmp, v)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.CONST_GET, uint64(aTimesUIdx)).Emit(instr.CALL)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, uint64(atTimesUIdx)).Emit(instr.CALL)
	// eval_ata_times_u(n, v, u, tmp): eval_a_times_u(n, v, tmp); eval_at_times_u(n, tmp, u)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.CONST_GET, uint64(aTimesUIdx)).Emit(instr.CALL)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, uint64(atTimesUIdx)).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(itLoop)
	b.Bind(itDone)

	sumLoop := b.Label()
	sumDone := b.Label()
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_SET, 5)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 6)
	b.Bind(sumLoop)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(sumDone)
	b.Emit(instr.LOCAL_GET, 4)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 6).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 6).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL).Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.LOCAL_GET, 5)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 6).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 6).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL).Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 5)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 6)
	b.Br(sumLoop)
	b.Bind(sumDone)

	b.Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_GET, 5).Emit(instr.F64_DIV).Emit(instr.F64_SQRT)
	b.Emit(instr.F64_CONST, math.Float64bits(1e9)).Emit(instr.F64_MUL)
	b.Emit(instr.F64_TO_I32_S)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// spectralnormTimesU builds eval_a_times_u (transposed=false) or
// eval_at_times_u (transposed=true): both share every instruction except the
// order of eval_a's two arguments.
func spectralnormTimesU(evalAIdx int, transposed bool) *types.Function {
	fb := types.NewFunctionBuilder(&types.FunctionType{}).
		Params(types.TypeI32, types.TypeF64Array, types.TypeF64Array).
		Locals(types.TypeI32, types.TypeI32, types.TypeF64)
	outer := fb.Label()
	outerDone := fb.Label()
	inner := fb.Label()
	innerDone := fb.Label()
	first, second := instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 4)
	if transposed {
		first, second = second, first
	}
	return fb.
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 3)).
		Bind(outer).
		Emit(instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).
		BrIf(outerDone).
		Emit(
			instr.New(instr.F64_CONST, math.Float64bits(0.0)), instr.New(instr.LOCAL_SET, 5),
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 4),
		).
		Bind(inner).
		Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).
		BrIf(innerDone).
		Emit(
			// s = s + eval_a(i, j) * u[j] (args swapped for eval_at_times_u)
			instr.New(instr.LOCAL_GET, 5),
			first, second,
			instr.New(instr.CONST_GET, uint64(evalAIdx)), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 4), instr.New(instr.ARRAY_GET),
			instr.New(instr.F64_MUL), instr.New(instr.F64_ADD), instr.New(instr.LOCAL_SET, 5),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4),
		).
		Br(inner).
		Bind(innerDone).
		Emit(
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 5), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 3),
		).
		Br(outer).
		Bind(outerDone).
		Emit(instr.New(instr.RETURN)).
		MustBuild()
}

// mandelbrot builds the benchmarks-game Mandelbrot kernel. escape_count has
// an early return inside its loop, so it stays a real bytecode function with
// a genuine early RETURN rather than a flag variable.
func mandelbrot(width, height, maxIter int32) *program.Program {
	b := program.NewBuilder()

	// escape_count params: 0=cr,1=ci,2=maxIter
	// locals: 3=zr,4=zi,5=i,6=zr2,7=zi2,8=newZr,9=newZi
	escapeBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeF64, types.TypeF64, types.TypeI32).
		Locals(types.TypeF64, types.TypeF64, types.TypeI32, types.TypeF64, types.TypeF64, types.TypeF64, types.TypeF64)
	loop := escapeBuilder.Label()
	loopDone := escapeBuilder.Label()
	escape := escapeBuilder.Label()
	escapeFn := escapeBuilder.
		Emit(
			instr.New(instr.F64_CONST, math.Float64bits(0.0)), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.F64_CONST, math.Float64bits(0.0)), instr.New(instr.LOCAL_SET, 4),
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 5),
		).
		Bind(loop).
		Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_GE_S)).
		BrIf(loopDone).
		Emit(
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 3), instr.New(instr.F64_MUL), instr.New(instr.LOCAL_SET, 6),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 4), instr.New(instr.F64_MUL), instr.New(instr.LOCAL_SET, 7),
			instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 7), instr.New(instr.F64_ADD),
			instr.New(instr.F64_CONST, math.Float64bits(4.0)), instr.New(instr.F64_GT),
		).
		BrIf(escape).
		Emit(
			instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 7), instr.New(instr.F64_SUB),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.F64_ADD), instr.New(instr.LOCAL_SET, 8),
			instr.New(instr.F64_CONST, math.Float64bits(2.0)), instr.New(instr.LOCAL_GET, 3), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.F64_MUL),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.F64_ADD), instr.New(instr.LOCAL_SET, 9),
			instr.New(instr.LOCAL_GET, 8), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.LOCAL_GET, 9), instr.New(instr.LOCAL_SET, 4),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 5),
		).
		Br(loop).
		Bind(escape).
		Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.RETURN)).
		Bind(loopDone).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN)).
		MustBuild()
	escapeIdx := b.Const(escapeFn)

	// main locals: 0=total,1=py,2=cy,3=px,4=cx
	b.Locals(types.TypeI32, types.TypeI32, types.TypeF64, types.TypeI32, types.TypeF64)

	pyLoop := b.Label()
	pyDone := b.Label()
	pxLoop := b.Label()
	pxDone := b.Label()

	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(pyLoop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(height))).Emit(instr.I32_GE_S).BrIf(pyDone)
	// cy = y_min + (y_max - y_min) * float(py) / float(height - 1)
	b.Emit(instr.F64_CONST, math.Float64bits(-1.5))
	b.Emit(instr.F64_CONST, math.Float64bits(1.5)).Emit(instr.F64_CONST, math.Float64bits(-1.5)).Emit(instr.F64_SUB)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_TO_F64_S).Emit(instr.F64_MUL)
	b.Emit(instr.F64_CONST, math.Float64bits(float64(height-1))).Emit(instr.F64_DIV)
	b.Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 2)

	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Bind(pxLoop)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(width))).Emit(instr.I32_GE_S).BrIf(pxDone)
	// cx = x_min + (x_max - x_min) * float(px) / float(width - 1)
	b.Emit(instr.F64_CONST, math.Float64bits(-2.0))
	b.Emit(instr.F64_CONST, math.Float64bits(1.0)).Emit(instr.F64_CONST, math.Float64bits(-2.0)).Emit(instr.F64_SUB)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_TO_F64_S).Emit(instr.F64_MUL)
	b.Emit(instr.F64_CONST, math.Float64bits(float64(width-1))).Emit(instr.F64_DIV)
	b.Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 4)

	// total += escape_count(cx, cy, max_iter)
	b.Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(maxIter)))
	b.Emit(instr.CONST_GET, uint64(escapeIdx)).Emit(instr.CALL)
	b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)

	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(pxLoop)
	b.Bind(pxDone)

	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(pyLoop)
	b.Bind(pyDone)

	b.Emit(instr.LOCAL_GET, 0)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// matmul builds the benchmarks-game dense-matmul kernel. matmul has one call
// site in the source (main calls it once), so it is inlined into the
// top-level code rather than built as a separate bytecode function.
func matmul(n int32) *program.Program {
	b := program.NewBuilder()
	arrayType := b.Type(types.TypeF64Array)

	// locals: 0=a,1=b,2=out,3=i,4=j,5=k,6=s,7=idx,8=checksum
	b.Locals(types.TypeF64Array, types.TypeF64Array, types.TypeF64Array, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeF64, types.TypeI32, types.TypeF64)

	nn := uint64(uint32(n * n))
	b.Emit(instr.I32_CONST, nn).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, nn).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, nn).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 2)

	fillOuter := b.Label()
	fillOuterDone := b.Label()
	fillInner := b.Label()
	fillInnerDone := b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Bind(fillOuter)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(fillOuterDone)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
	b.Bind(fillInner)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(fillInnerDone)
	// a[i*n+j] = float((i*7+j*3)%13) - 6.0
	b.Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_MUL).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 7).Emit(instr.I32_MUL)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 3).Emit(instr.I32_MUL)
	b.Emit(instr.I32_ADD).Emit(instr.I32_CONST, 13).Emit(instr.I32_REM_S).Emit(instr.I32_TO_F64_S)
	b.Emit(instr.F64_CONST, math.Float64bits(6.0)).Emit(instr.F64_SUB)
	b.Emit(instr.ARRAY_SET)
	// b[i*n+j] = float((i*5+j*11)%17) - 8.0
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_MUL).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 5).Emit(instr.I32_MUL)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 11).Emit(instr.I32_MUL)
	b.Emit(instr.I32_ADD).Emit(instr.I32_CONST, 17).Emit(instr.I32_REM_S).Emit(instr.I32_TO_F64_S)
	b.Emit(instr.F64_CONST, math.Float64bits(8.0)).Emit(instr.F64_SUB)
	b.Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
	b.Br(fillInner)
	b.Bind(fillInnerDone)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(fillOuter)
	b.Bind(fillOuterDone)

	mmI := b.Label()
	mmIDone := b.Label()
	mmJ := b.Label()
	mmJDone := b.Label()
	mmK := b.Label()
	mmKDone := b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Bind(mmI)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(mmIDone)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
	b.Bind(mmJ)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(mmJDone)
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_SET, 6)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 5)
	b.Bind(mmK)
	b.Emit(instr.LOCAL_GET, 5).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(mmKDone)
	// s = s + a[i*n+k]*b[k*n+j]
	b.Emit(instr.LOCAL_GET, 6)
	b.Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_MUL).Emit(instr.LOCAL_GET, 5).Emit(instr.I32_ADD)
	b.Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.LOCAL_GET, 5).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_MUL).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD)
	b.Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_MUL).Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 6)
	b.Emit(instr.LOCAL_GET, 5).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 5)
	b.Br(mmK)
	b.Bind(mmKDone)
	// out[i*n+j] = s
	b.Emit(instr.LOCAL_GET, 2)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_MUL).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD)
	b.Emit(instr.LOCAL_GET, 6)
	b.Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
	b.Br(mmJ)
	b.Bind(mmJDone)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(mmI)
	b.Bind(mmIDone)

	sumLoop := b.Label()
	sumDone := b.Label()
	b.Emit(instr.F64_CONST, math.Float64bits(0.0)).Emit(instr.LOCAL_SET, 8)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 7)
	b.Bind(sumLoop)
	b.Emit(instr.LOCAL_GET, 7).Emit(instr.I32_CONST, nn).Emit(instr.I32_GE_S).BrIf(sumDone)
	b.Emit(instr.LOCAL_GET, 8)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 7).Emit(instr.ARRAY_GET)
	b.Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 8)
	b.Emit(instr.LOCAL_GET, 7).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 7)
	b.Br(sumLoop)
	b.Bind(sumDone)

	b.Emit(instr.LOCAL_GET, 8)
	b.Emit(instr.F64_CONST, math.Float64bits(1e6)).Emit(instr.F64_MUL)
	b.Emit(instr.F64_TO_I32_S)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// nbodyReference transcribes nbody's advance/energy loops operation-for-
// operation in float64 so its result is bit-identical to the bytecode kernel.
func nbodyReference(steps int32) int32 {
	const pi = 3.141592653589793
	solarMass := 4.0 * pi * pi
	daysPerYear := 365.24

	x := []float64{0.0, 4.84143144246472090, 8.34336671824457987, 12.94350551331783510, 15.37969711485094510}
	y := []float64{0.0, -1.16032004402742839, 4.12479856412430479, -15.11151401698631891, -25.91931460998796403}
	z := []float64{0.0, -0.10362204447112311, -0.40352341711432138, -0.22370579633577680, 0.17925877295037118}

	vx := []float64{0.0, 0.00166007664274403 * daysPerYear, 0.00283009096225471 * daysPerYear, 0.00296460137564761 * daysPerYear, 0.00268067772490389 * daysPerYear}
	vy := []float64{0.0, 0.00769901118419740 * daysPerYear, 0.00453000209594919 * daysPerYear, 0.00237847173959480 * daysPerYear, 0.00162824170038242 * daysPerYear}
	vz := []float64{0.0, -0.00006902509938426 * daysPerYear, -0.00019131288713706 * daysPerYear, -0.00029589288865580 * daysPerYear, -0.00095159225451337 * daysPerYear}

	mass := []float64{solarMass, 9.54791938424326609e-04 * solarMass, 2.85885980666130812e-04 * solarMass, 4.36624404335156298e-05 * solarMass, 5.15138902046611451e-05 * solarMass}

	const nb = 5

	var px, py, pz float64
	for i := 0; i < nb; i++ {
		px += vx[i] * mass[i]
		py += vy[i] * mass[i]
		pz += vz[i] * mass[i]
	}
	vx[0] = 0.0 - px/solarMass
	vy[0] = 0.0 - py/solarMass
	vz[0] = 0.0 - pz/solarMass

	const dt = 0.01
	for s := int32(0); s < steps; s++ {
		nbodyAdvanceReference(nb, x, y, z, vx, vy, vz, mass, dt)
	}

	e := nbodyEnergyReference(nb, x, y, z, vx, vy, vz, mass)
	return int32(e * 1e9)
}

func nbodyAdvanceReference(nb int, x, y, z, vx, vy, vz, mass []float64, dt float64) {
	for i := 0; i < nb; i++ {
		for j := i + 1; j < nb; j++ {
			dx := x[i] - x[j]
			dy := y[i] - y[j]
			dz := z[i] - z[j]
			dist2 := dx*dx + dy*dy + dz*dz
			mag := dt / (dist2 * math.Sqrt(dist2))
			vx[i] -= dx * mass[j] * mag
			vy[i] -= dy * mass[j] * mag
			vz[i] -= dz * mass[j] * mag
			vx[j] += dx * mass[i] * mag
			vy[j] += dy * mass[i] * mag
			vz[j] += dz * mass[i] * mag
		}
	}
	for i := 0; i < nb; i++ {
		x[i] += dt * vx[i]
		y[i] += dt * vy[i]
		z[i] += dt * vz[i]
	}
}

func nbodyEnergyReference(nb int, x, y, z, vx, vy, vz, mass []float64) float64 {
	var e float64
	for i := 0; i < nb; i++ {
		e += 0.5 * mass[i] * (vx[i]*vx[i] + vy[i]*vy[i] + vz[i]*vz[i])
		for j := i + 1; j < nb; j++ {
			dx := x[i] - x[j]
			dy := y[i] - y[j]
			dz := z[i] - z[j]
			dist := math.Sqrt(dx*dx + dy*dy + dz*dz)
			e -= (mass[i] * mass[j]) / dist
		}
	}
	return e
}

// spectralnormReference transcribes spectralnorm's power iteration
// operation-for-operation in float64 so its result is bit-identical to the
// bytecode kernel.
func spectralnormReference(n, rounds int32) int32 {
	u := make([]float64, n)
	for i := range u {
		u[i] = 1.0
	}
	v := make([]float64, n)
	tmp := make([]float64, n)

	for it := int32(0); it < rounds; it++ {
		spectralnormAtaTimesU(n, u, v, tmp)
		spectralnormAtaTimesU(n, v, u, tmp)
	}

	var vbv, vv float64
	for i := int32(0); i < n; i++ {
		vbv += u[i] * v[i]
		vv += v[i] * v[i]
	}

	result := math.Sqrt(vbv / vv)
	return int32(result * 1e9)
}

func spectralnormAtaTimesU(n int32, u, out, tmp []float64) {
	spectralnormATimesU(n, u, tmp)
	spectralnormAtTimesU(n, tmp, out)
}

func spectralnormATimesU(n int32, u, out []float64) {
	for i := int32(0); i < n; i++ {
		var s float64
		for j := int32(0); j < n; j++ {
			s += spectralnormEvalA(i, j) * u[j]
		}
		out[i] = s
	}
}

func spectralnormAtTimesU(n int32, u, out []float64) {
	for i := int32(0); i < n; i++ {
		var s float64
		for j := int32(0); j < n; j++ {
			s += spectralnormEvalA(j, i) * u[j]
		}
		out[i] = s
	}
}

func spectralnormEvalA(i, j int32) float64 {
	return 1.0 / float64((i+j)*(i+j+1)/2+i+1)
}

// mandelbrotReference transcribes mandelbrot's escape-time loop operation-
// for-operation in float64 so its result is bit-identical to the bytecode
// kernel.
func mandelbrotReference(width, height, maxIter int32) int32 {
	const xMin, xMax = -2.0, 1.0
	const yMin, yMax = -1.5, 1.5

	var total int32
	for py := int32(0); py < height; py++ {
		cy := yMin + (yMax-yMin)*float64(py)/float64(height-1)
		for px := int32(0); px < width; px++ {
			cx := xMin + (xMax-xMin)*float64(px)/float64(width-1)
			total += mandelbrotEscapeCount(cx, cy, maxIter)
		}
	}
	return total
}

func mandelbrotEscapeCount(cr, ci float64, maxIter int32) int32 {
	var zr, zi float64
	for i := int32(0); i < maxIter; i++ {
		zr2 := zr * zr
		zi2 := zi * zi
		if zr2+zi2 > 4.0 {
			return i
		}
		newZr := zr2 - zi2 + cr
		newZi := 2.0*zr*zi + ci
		zr = newZr
		zi = newZi
	}
	return maxIter
}

// matmulReference transcribes matmul's fill and multiply loops operation-
// for-operation in float64 so its result is bit-identical to the bytecode
// kernel.
func matmulReference(n int32) int32 {
	a := make([]float64, n*n)
	b := make([]float64, n*n)
	out := make([]float64, n*n)

	for i := int32(0); i < n; i++ {
		for j := int32(0); j < n; j++ {
			a[i*n+j] = float64((i*7+j*3)%13) - 6.0
			b[i*n+j] = float64((i*5+j*11)%17) - 8.0
		}
	}

	for i := int32(0); i < n; i++ {
		for j := int32(0); j < n; j++ {
			var s float64
			for k := int32(0); k < n; k++ {
				s += a[i*n+k] * b[k*n+j]
			}
			out[i*n+j] = s
		}
	}

	var checksum float64
	for idx := int32(0); idx < n*n; idx++ {
		checksum += out[idx]
	}
	return int32(checksum * 1e6)
}
