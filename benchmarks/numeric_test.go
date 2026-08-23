package benchmarks_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

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

// branchTree builds nodes independent threshold branches over locals 0=input,
// 1=total, each node's "left"/"join" labels distinguished by its index since
// every node contributes its own label pair to one shared .code listing.
func branchTree(input int32, nodes int) (*program.Program, int32) {
	var sb strings.Builder
	sb.WriteString(".locals\ni32\ni32\n.code\n")
	fmt.Fprintf(&sb, "\ti32.const %d\n\tlocal.set 0\n", input)
	sb.WriteString("\ti32.const 0\n\tlocal.set 1\n")

	var want int32
	for index := range nodes {
		threshold := int32((index*17 + 11) % 97)
		leftValue := int32(index%7 + 1)
		rightValue := int32(index%5 + 2)
		fmt.Fprintf(&sb, "\tlocal.get 0\n\ti32.const %d\n\ti32.lt_s\n\tbr_if left%d\n", threshold, index)
		fmt.Fprintf(&sb, "\tlocal.get 1\n\ti32.const %d\n\ti32.add\n\tlocal.set 1\n\tbr join%d\n", rightValue, index)
		fmt.Fprintf(&sb, "left%d:\n\tlocal.get 1\n\ti32.const %d\n\ti32.add\n\tlocal.set 1\n", index, leftValue)
		fmt.Fprintf(&sb, "join%d:\n", index)
		if input < threshold {
			want += leftValue
		} else {
			want += rightValue
		}
	}
	sb.WriteString("\tlocal.get 1\n")

	return mustParseProgram(sb.String()), want
}

// nbody builds the benchmarks-game N-body kernel: offset_momentum runs once,
// advance runs steps times as a called function (it iterates in the source's
// own loop), and energy's checksum is inlined since it has one call site.
// nbodyListing builds the benchmarks-game N-body kernel: offset_momentum runs
// once, advance (constant 0) runs steps times as a called function (it
// iterates in the source's own loop), and energy's checksum is inlined since
// it has one call site.
//
// advance params: 0=nb,1=x,2=y,3=z,4=vx,5=vy,6=vz,7=mass,8=dt; locals:
// 9=i,10=j,11=dx,12=dy,13=dz,14=dist2,15=mag.
//
// Main locals: 0=x,1=y,2=z,3=vx,4=vy,5=vz,6=mass,7=pi,8=solarMass,
// 9=daysPerYear,10=px,11=py,12=pz,13=i,14=steps,15=j,16=e,17=dx,18=dy,19=dz,
// 20=dist. %[1]d substitutes steps.
const nbodyListing = `
.locals
[]f64
[]f64
[]f64
[]f64
[]f64
[]f64
[]f64
f64
f64
f64
f64
f64
f64
i32
i32
i32
f64
f64
f64
f64
f64
.types
[]f64
.constants
func(i32, []f64, []f64, []f64, []f64, []f64, []f64, []f64, f64)
	i32
	i32
	f64
	f64
	f64
	f64
	f64
	i32.const 0
	local.set 9
	pairLoop:
	local.get 9
	local.get 0
	i32.ge_s
	br_if pairDone
	local.get 9
	i32.const 1
	i32.add
	local.set 10
	bodyLoop:
	local.get 10
	local.get 0
	i32.ge_s
	br_if bodyDone
	local.get 1
	local.get 9
	array.get
	local.get 1
	local.get 10
	array.get
	f64.sub
	local.set 11
	local.get 2
	local.get 9
	array.get
	local.get 2
	local.get 10
	array.get
	f64.sub
	local.set 12
	local.get 3
	local.get 9
	array.get
	local.get 3
	local.get 10
	array.get
	f64.sub
	local.set 13
	local.get 11
	local.get 11
	f64.mul
	local.get 12
	local.get 12
	f64.mul
	f64.add
	local.get 13
	local.get 13
	f64.mul
	f64.add
	local.set 14
	local.get 8
	local.get 14
	local.get 14
	f64.sqrt
	f64.mul
	f64.div
	local.set 15
	local.get 4
	local.get 9
	local.get 4
	local.get 9
	array.get
	local.get 11
	local.get 7
	local.get 10
	array.get
	f64.mul
	local.get 15
	f64.mul
	f64.sub
	array.set
	local.get 5
	local.get 9
	local.get 5
	local.get 9
	array.get
	local.get 12
	local.get 7
	local.get 10
	array.get
	f64.mul
	local.get 15
	f64.mul
	f64.sub
	array.set
	local.get 6
	local.get 9
	local.get 6
	local.get 9
	array.get
	local.get 13
	local.get 7
	local.get 10
	array.get
	f64.mul
	local.get 15
	f64.mul
	f64.sub
	array.set
	local.get 4
	local.get 10
	local.get 4
	local.get 10
	array.get
	local.get 11
	local.get 7
	local.get 9
	array.get
	f64.mul
	local.get 15
	f64.mul
	f64.add
	array.set
	local.get 5
	local.get 10
	local.get 5
	local.get 10
	array.get
	local.get 12
	local.get 7
	local.get 9
	array.get
	f64.mul
	local.get 15
	f64.mul
	f64.add
	array.set
	local.get 6
	local.get 10
	local.get 6
	local.get 10
	array.get
	local.get 13
	local.get 7
	local.get 9
	array.get
	f64.mul
	local.get 15
	f64.mul
	f64.add
	array.set
	local.get 10
	i32.const 1
	i32.add
	local.set 10
	br bodyLoop
	bodyDone:
	local.get 9
	i32.const 1
	i32.add
	local.set 9
	br pairLoop
	pairDone:
	i32.const 0
	local.set 9
	settleLoop:
	local.get 9
	local.get 0
	i32.ge_s
	br_if settleDone
	local.get 1
	local.get 9
	local.get 1
	local.get 9
	array.get
	local.get 8
	local.get 4
	local.get 9
	array.get
	f64.mul
	f64.add
	array.set
	local.get 2
	local.get 9
	local.get 2
	local.get 9
	array.get
	local.get 8
	local.get 5
	local.get 9
	array.get
	f64.mul
	f64.add
	array.set
	local.get 3
	local.get 9
	local.get 3
	local.get 9
	array.get
	local.get 8
	local.get 6
	local.get 9
	array.get
	f64.mul
	f64.add
	array.set
	local.get 9
	i32.const 1
	i32.add
	local.set 9
	br settleLoop
	settleDone:
	return
.code
	f64.const 3.141592653589793
	local.set 7
	f64.const 4.0
	local.get 7
	f64.mul
	local.get 7
	f64.mul
	local.set 8
	f64.const 365.24
	local.set 9
	i32.const 5
	array.new_default 0
	local.set 0
	local.get 0
	i32.const 0
	f64.const 0.0
	array.set
	local.get 0
	i32.const 1
	f64.const 4.84143144246472090
	array.set
	local.get 0
	i32.const 2
	f64.const 8.34336671824457987
	array.set
	local.get 0
	i32.const 3
	f64.const 12.94350551331783510
	array.set
	local.get 0
	i32.const 4
	f64.const 15.37969711485094510
	array.set
	i32.const 5
	array.new_default 0
	local.set 1
	local.get 1
	i32.const 0
	f64.const 0.0
	array.set
	local.get 1
	i32.const 1
	f64.const -1.16032004402742839
	array.set
	local.get 1
	i32.const 2
	f64.const 4.12479856412430479
	array.set
	local.get 1
	i32.const 3
	f64.const -15.11151401698631891
	array.set
	local.get 1
	i32.const 4
	f64.const -25.91931460998796403
	array.set
	i32.const 5
	array.new_default 0
	local.set 2
	local.get 2
	i32.const 0
	f64.const 0.0
	array.set
	local.get 2
	i32.const 1
	f64.const -0.10362204447112311
	array.set
	local.get 2
	i32.const 2
	f64.const -0.40352341711432138
	array.set
	local.get 2
	i32.const 3
	f64.const -0.22370579633577680
	array.set
	local.get 2
	i32.const 4
	f64.const 0.17925877295037118
	array.set
	i32.const 5
	array.new_default 0
	local.set 3
	local.get 3
	i32.const 0
	f64.const 0.0
	array.set
	local.get 3
	i32.const 1
	f64.const 0.00166007664274403
	local.get 9
	f64.mul
	array.set
	local.get 3
	i32.const 2
	f64.const 0.00283009096225471
	local.get 9
	f64.mul
	array.set
	local.get 3
	i32.const 3
	f64.const 0.00296460137564761
	local.get 9
	f64.mul
	array.set
	local.get 3
	i32.const 4
	f64.const 0.00268067772490389
	local.get 9
	f64.mul
	array.set
	i32.const 5
	array.new_default 0
	local.set 4
	local.get 4
	i32.const 0
	f64.const 0.0
	array.set
	local.get 4
	i32.const 1
	f64.const 0.00769901118419740
	local.get 9
	f64.mul
	array.set
	local.get 4
	i32.const 2
	f64.const 0.00453000209594919
	local.get 9
	f64.mul
	array.set
	local.get 4
	i32.const 3
	f64.const 0.00237847173959480
	local.get 9
	f64.mul
	array.set
	local.get 4
	i32.const 4
	f64.const 0.00162824170038242
	local.get 9
	f64.mul
	array.set
	i32.const 5
	array.new_default 0
	local.set 5
	local.get 5
	i32.const 0
	f64.const 0.0
	array.set
	local.get 5
	i32.const 1
	f64.const -0.00006902509938426
	local.get 9
	f64.mul
	array.set
	local.get 5
	i32.const 2
	f64.const -0.00019131288713706
	local.get 9
	f64.mul
	array.set
	local.get 5
	i32.const 3
	f64.const -0.00029589288865580
	local.get 9
	f64.mul
	array.set
	local.get 5
	i32.const 4
	f64.const -0.00095159225451337
	local.get 9
	f64.mul
	array.set
	i32.const 5
	array.new_default 0
	local.set 6
	local.get 6
	i32.const 0
	local.get 8
	array.set
	local.get 6
	i32.const 1
	f64.const 9.54791938424326609e-04
	local.get 8
	f64.mul
	array.set
	local.get 6
	i32.const 2
	f64.const 2.85885980666130812e-04
	local.get 8
	f64.mul
	array.set
	local.get 6
	i32.const 3
	f64.const 4.36624404335156298e-05
	local.get 8
	f64.mul
	array.set
	local.get 6
	i32.const 4
	f64.const 5.15138902046611451e-05
	local.get 8
	f64.mul
	array.set
	f64.const 0.0
	local.set 10
	f64.const 0.0
	local.set 11
	f64.const 0.0
	local.set 12
	i32.const 0
	local.set 13
momentumLoop:
	local.get 13
	i32.const 5
	i32.ge_s
	br_if momentumDone
	local.get 10
	local.get 3
	local.get 13
	array.get
	local.get 6
	local.get 13
	array.get
	f64.mul
	f64.add
	local.set 10
	local.get 11
	local.get 4
	local.get 13
	array.get
	local.get 6
	local.get 13
	array.get
	f64.mul
	f64.add
	local.set 11
	local.get 12
	local.get 5
	local.get 13
	array.get
	local.get 6
	local.get 13
	array.get
	f64.mul
	f64.add
	local.set 12
	local.get 13
	i32.const 1
	i32.add
	local.set 13
	br momentumLoop
momentumDone:
	local.get 3
	i32.const 0
	f64.const 0.0
	local.get 10
	local.get 8
	f64.div
	f64.sub
	array.set
	local.get 4
	i32.const 0
	f64.const 0.0
	local.get 11
	local.get 8
	f64.div
	f64.sub
	array.set
	local.get 5
	i32.const 0
	f64.const 0.0
	local.get 12
	local.get 8
	f64.div
	f64.sub
	array.set
	i32.const 0
	local.set 14
stepsLoop:
	local.get 14
	i32.const %[1]d
	i32.ge_s
	br_if stepsDone
	i32.const 5
	local.get 0
	local.get 1
	local.get 2
	local.get 3
	local.get 4
	local.get 5
	local.get 6
	f64.const 0.01
	const.get 0
	call
	local.get 14
	i32.const 1
	i32.add
	local.set 14
	br stepsLoop
stepsDone:
	f64.const 0.0
	local.set 16
	i32.const 0
	local.set 13
energyOuter:
	local.get 13
	i32.const 5
	i32.ge_s
	br_if energyOuterDone
	local.get 16
	f64.const 0.5
	local.get 6
	local.get 13
	array.get
	f64.mul
	local.get 3
	local.get 13
	array.get
	local.get 3
	local.get 13
	array.get
	f64.mul
	local.get 4
	local.get 13
	array.get
	local.get 4
	local.get 13
	array.get
	f64.mul
	f64.add
	local.get 5
	local.get 13
	array.get
	local.get 5
	local.get 13
	array.get
	f64.mul
	f64.add
	f64.mul
	f64.add
	local.set 16
	local.get 13
	i32.const 1
	i32.add
	local.set 15
energyInner:
	local.get 15
	i32.const 5
	i32.ge_s
	br_if energyInnerDone
	local.get 0
	local.get 13
	array.get
	local.get 0
	local.get 15
	array.get
	f64.sub
	local.set 17
	local.get 1
	local.get 13
	array.get
	local.get 1
	local.get 15
	array.get
	f64.sub
	local.set 18
	local.get 2
	local.get 13
	array.get
	local.get 2
	local.get 15
	array.get
	f64.sub
	local.set 19
	local.get 17
	local.get 17
	f64.mul
	local.get 18
	local.get 18
	f64.mul
	f64.add
	local.get 19
	local.get 19
	f64.mul
	f64.add
	f64.sqrt
	local.set 20
	local.get 16
	local.get 6
	local.get 13
	array.get
	local.get 6
	local.get 15
	array.get
	f64.mul
	local.get 20
	f64.div
	f64.sub
	local.set 16
	local.get 15
	i32.const 1
	i32.add
	local.set 15
	br energyInner
energyInnerDone:
	local.get 13
	i32.const 1
	i32.add
	local.set 13
	br energyOuter
energyOuterDone:
	local.get 16
	f64.const 1e9
	f64.mul
	f64.to_i32_s
`

func nbody(steps int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(nbodyListing, steps))
}

// spectralnormListing builds the benchmarks-game spectral-norm kernel. eval_a
// (constant 0), eval_a_times_u (constant 1), and eval_at_times_u (constant 2)
// each have several call sites, so they stay real bytecode functions;
// eval_ata_times_u has one call site (the round loop body) and is inlined
// into it. eval_a_times_u and eval_at_times_u share every instruction except
// the order of eval_a's two arguments (local.get 3/4 vs 4/3). Main locals:
// 0=u,1=v,2=tmp,3=it,4=vbv,5=vv,6=i. %[1]d substitutes n, %[2]d rounds.
const spectralnormListing = `
.locals
[]f64
[]f64
[]f64
i32
f64
f64
i32
.types
[]f64
.constants
func(i32, i32) f64
	f64.const 1.0
	local.get 0
	local.get 1
	i32.add
	local.get 0
	local.get 1
	i32.add
	i32.const 1
	i32.add
	i32.mul
	i32.const 2
	i32.div_s
	local.get 0
	i32.const 1
	i32.add
	i32.add
	i32.to_f64_s
	f64.div
	return
func(i32, []f64, []f64)
	i32
	i32
	f64
	i32.const 0
	local.set 3
	outer:
	local.get 3
	local.get 0
	i32.ge_s
	br_if outerDone
	f64.const 0.0
	local.set 5
	i32.const 0
	local.set 4
	inner:
	local.get 4
	local.get 0
	i32.ge_s
	br_if innerDone
	local.get 5
	local.get 3
	local.get 4
	const.get 0
	call
	local.get 1
	local.get 4
	array.get
	f64.mul
	f64.add
	local.set 5
	local.get 4
	i32.const 1
	i32.add
	local.set 4
	br inner
	innerDone:
	local.get 2
	local.get 3
	local.get 5
	array.set
	local.get 3
	i32.const 1
	i32.add
	local.set 3
	br outer
	outerDone:
	return
func(i32, []f64, []f64)
	i32
	i32
	f64
	i32.const 0
	local.set 3
	outer:
	local.get 3
	local.get 0
	i32.ge_s
	br_if outerDone
	f64.const 0.0
	local.set 5
	i32.const 0
	local.set 4
	inner:
	local.get 4
	local.get 0
	i32.ge_s
	br_if innerDone
	local.get 5
	local.get 4
	local.get 3
	const.get 0
	call
	local.get 1
	local.get 4
	array.get
	f64.mul
	f64.add
	local.set 5
	local.get 4
	i32.const 1
	i32.add
	local.set 4
	br inner
	innerDone:
	local.get 2
	local.get 3
	local.get 5
	array.set
	local.get 3
	i32.const 1
	i32.add
	local.set 3
	br outer
	outerDone:
	return
.code
	i32.const %[1]d
	array.new_default 0
	local.set 0
	local.get 0
	i32.const 0
	f64.const 1.0
	i32.const %[1]d
	array.fill
	i32.const %[1]d
	array.new_default 0
	local.set 1
	i32.const %[1]d
	array.new_default 0
	local.set 2
	i32.const 0
	local.set 3
itLoop:
	local.get 3
	i32.const %[2]d
	i32.ge_s
	br_if itDone
	i32.const %[1]d
	local.get 0
	local.get 2
	const.get 1
	call
	i32.const %[1]d
	local.get 2
	local.get 1
	const.get 2
	call
	i32.const %[1]d
	local.get 1
	local.get 2
	const.get 1
	call
	i32.const %[1]d
	local.get 2
	local.get 0
	const.get 2
	call
	local.get 3
	i32.const 1
	i32.add
	local.set 3
	br itLoop
itDone:
	f64.const 0.0
	local.set 4
	f64.const 0.0
	local.set 5
	i32.const 0
	local.set 6
sumLoop:
	local.get 6
	i32.const %[1]d
	i32.ge_s
	br_if sumDone
	local.get 4
	local.get 0
	local.get 6
	array.get
	local.get 1
	local.get 6
	array.get
	f64.mul
	f64.add
	local.set 4
	local.get 5
	local.get 1
	local.get 6
	array.get
	local.get 1
	local.get 6
	array.get
	f64.mul
	f64.add
	local.set 5
	local.get 6
	i32.const 1
	i32.add
	local.set 6
	br sumLoop
sumDone:
	local.get 4
	local.get 5
	f64.div
	f64.sqrt
	f64.const 1e9
	f64.mul
	f64.to_i32_s
`

func spectralnorm(n, rounds int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(spectralnormListing, n, rounds))
}

// mandelbrot builds the benchmarks-game Mandelbrot kernel. escape_count has
// an early return inside its loop, so it stays a real bytecode function with
// a genuine early RETURN rather than a flag variable.
// mandelbrotListing's escape_count constant (params: 0=cr,1=ci,2=maxIter;
// locals: 3=zr,4=zi,5=i,6=zr2,7=zi2,8=newZr,9=newZi) has an early return
// inside its loop, so it stays a real bytecode function with a genuine early
// RETURN rather than a flag variable. Main locals: 0=total,1=py,2=cy,3=px,
// 4=cx. %[2]d and %[4]d substitute height-1/width-1 as f64.const literals
// (the trailing ".0" keeps them float tokens, not integer ones).
const mandelbrotListing = `
.locals
i32
i32
f64
i32
f64
.constants
func(f64, f64, i32) i32
	f64
	f64
	i32
	f64
	f64
	f64
	f64
	f64.const 0.0
	local.set 3
	f64.const 0.0
	local.set 4
	i32.const 0
	local.set 5
	loop:
	local.get 5
	local.get 2
	i32.ge_s
	br_if loopDone
	local.get 3
	local.get 3
	f64.mul
	local.set 6
	local.get 4
	local.get 4
	f64.mul
	local.set 7
	local.get 6
	local.get 7
	f64.add
	f64.const 4.0
	f64.gt
	br_if escape
	local.get 6
	local.get 7
	f64.sub
	local.get 0
	f64.add
	local.set 8
	f64.const 2.0
	local.get 3
	f64.mul
	local.get 4
	f64.mul
	local.get 1
	f64.add
	local.set 9
	local.get 8
	local.set 3
	local.get 9
	local.set 4
	local.get 5
	i32.const 1
	i32.add
	local.set 5
	br loop
	escape:
	local.get 5
	return
	loopDone:
	local.get 2
	return
.code
	i32.const 0
	local.set 0
	i32.const 0
	local.set 1
pyLoop:
	local.get 1
	i32.const %[1]d
	i32.ge_s
	br_if pyDone
	f64.const -1.5
	f64.const 1.5
	f64.const -1.5
	f64.sub
	local.get 1
	i32.to_f64_s
	f64.mul
	f64.const %[2]d.0
	f64.div
	f64.add
	local.set 2
	i32.const 0
	local.set 3
pxLoop:
	local.get 3
	i32.const %[3]d
	i32.ge_s
	br_if pxDone
	f64.const -2.0
	f64.const 1.0
	f64.const -2.0
	f64.sub
	local.get 3
	i32.to_f64_s
	f64.mul
	f64.const %[4]d.0
	f64.div
	f64.add
	local.set 4
	local.get 0
	local.get 4
	local.get 2
	i32.const %[5]d
	const.get 0
	call
	i32.add
	local.set 0
	local.get 3
	i32.const 1
	i32.add
	local.set 3
	br pxLoop
pxDone:
	local.get 1
	i32.const 1
	i32.add
	local.set 1
	br pyLoop
pyDone:
	local.get 0
`

func mandelbrot(width, height, maxIter int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(mandelbrotListing, height, height-1, width, width-1, maxIter))
}

// matmulListing builds the benchmarks-game dense-matmul kernel. matmul has
// one call site in the source (main calls it once), so it is inlined into
// the top-level code rather than built as a separate bytecode function.
// Locals: 0=a,1=b,2=out,3=i,4=j,5=k,6=s,7=idx,8=checksum. %[1]d substitutes
// n*n (array sizes and the checksum loop bound); %[2]d substitutes n (every
// other loop bound and index stride).
const matmulListing = `
.locals
[]f64
[]f64
[]f64
i32
i32
i32
f64
i32
f64
.types
[]f64
.code
	i32.const %[1]d
	array.new_default 0
	local.set 0
	i32.const %[1]d
	array.new_default 0
	local.set 1
	i32.const %[1]d
	array.new_default 0
	local.set 2
	i32.const 0
	local.set 3
fillOuter:
	local.get 3
	i32.const %[2]d
	i32.ge_s
	br_if fillOuterDone
	i32.const 0
	local.set 4
fillInner:
	local.get 4
	i32.const %[2]d
	i32.ge_s
	br_if fillInnerDone
	local.get 0
	local.get 3
	i32.const %[2]d
	i32.mul
	local.get 4
	i32.add
	local.get 3
	i32.const 7
	i32.mul
	local.get 4
	i32.const 3
	i32.mul
	i32.add
	i32.const 13
	i32.rem_s
	i32.to_f64_s
	f64.const 6.0
	f64.sub
	array.set
	local.get 1
	local.get 3
	i32.const %[2]d
	i32.mul
	local.get 4
	i32.add
	local.get 3
	i32.const 5
	i32.mul
	local.get 4
	i32.const 11
	i32.mul
	i32.add
	i32.const 17
	i32.rem_s
	i32.to_f64_s
	f64.const 8.0
	f64.sub
	array.set
	local.get 4
	i32.const 1
	i32.add
	local.set 4
	br fillInner
fillInnerDone:
	local.get 3
	i32.const 1
	i32.add
	local.set 3
	br fillOuter
fillOuterDone:
	i32.const 0
	local.set 3
mmI:
	local.get 3
	i32.const %[2]d
	i32.ge_s
	br_if mmIDone
	i32.const 0
	local.set 4
mmJ:
	local.get 4
	i32.const %[2]d
	i32.ge_s
	br_if mmJDone
	f64.const 0.0
	local.set 6
	i32.const 0
	local.set 5
mmK:
	local.get 5
	i32.const %[2]d
	i32.ge_s
	br_if mmKDone
	local.get 6
	local.get 0
	local.get 3
	i32.const %[2]d
	i32.mul
	local.get 5
	i32.add
	array.get
	local.get 1
	local.get 5
	i32.const %[2]d
	i32.mul
	local.get 4
	i32.add
	array.get
	f64.mul
	f64.add
	local.set 6
	local.get 5
	i32.const 1
	i32.add
	local.set 5
	br mmK
mmKDone:
	local.get 2
	local.get 3
	i32.const %[2]d
	i32.mul
	local.get 4
	i32.add
	local.get 6
	array.set
	local.get 4
	i32.const 1
	i32.add
	local.set 4
	br mmJ
mmJDone:
	local.get 3
	i32.const 1
	i32.add
	local.set 3
	br mmI
mmIDone:
	f64.const 0.0
	local.set 8
	i32.const 0
	local.set 7
sumLoop:
	local.get 7
	i32.const %[1]d
	i32.ge_s
	br_if sumDone
	local.get 8
	local.get 2
	local.get 7
	array.get
	f64.add
	local.set 8
	local.get 7
	i32.const 1
	i32.add
	local.set 7
	br sumLoop
sumDone:
	local.get 8
	f64.const 1e6
	f64.mul
	f64.to_i32_s
`

func matmul(n int32) *program.Program {
	nn := uint64(uint32(n * n))
	return mustParseProgram(fmt.Sprintf(matmulListing, nn, n))
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
