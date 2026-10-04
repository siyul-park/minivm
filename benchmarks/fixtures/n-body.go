package fixtures

import (
	"math"

	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
)

func init() {
	run := func() int32 { return nBody(100) }
	registry.Register(registry.Spec{
		Name:   "n-body",
		Result: func() types.Value { return types.I32(run()) },
		Native: registry.Native{I32: run},
	})
}

func nBodyEnergy(nb int, x, y, z, vx, vy, vz, mass []float64) float64 {
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
func nBody(steps int32) int32 {
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
		nBodyAdvance(nb, x, y, z, vx, vy, vz, mass, dt)
	}

	e := nBodyEnergy(nb, x, y, z, vx, vy, vz, mass)
	return int32(e * 1e9)
}
func nBodyAdvance(nb int, x, y, z, vx, vy, vz, mass []float64, dt float64) {
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
