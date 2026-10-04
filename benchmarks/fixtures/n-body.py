import math


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
    energy = 0.0
    i = 0
    while i < nb:
        energy = energy + 0.5 * mass[i] * (
            vx[i] * vx[i] + vy[i] * vy[i] + vz[i] * vz[i]
        )
        j = i + 1
        while j < nb:
            dx = x[i] - x[j]
            dy = y[i] - y[j]
            dz = z[i] - z[j]
            distance = math.sqrt(dx * dx + dy * dy + dz * dz)
            energy = energy - (mass[i] * mass[j]) / distance
            j = j + 1
        i = i + 1
    return energy


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

    x = [
        0.0,
        4.84143144246472090,
        8.34336671824457987,
        12.94350551331783510,
        15.37969711485094510,
    ]
    y = [
        0.0,
        -1.16032004402742839,
        4.12479856412430479,
        -15.11151401698631891,
        -25.91931460998796403,
    ]
    z = [
        0.0,
        -0.10362204447112311,
        -0.40352341711432138,
        -0.22370579633577680,
        0.17925877295037118,
    ]

    vx = [
        0.0,
        0.00166007664274403 * days_per_year,
        0.00283009096225471 * days_per_year,
        0.00296460137564761 * days_per_year,
        0.00268067772490389 * days_per_year,
    ]
    vy = [
        0.0,
        0.00769901118419740 * days_per_year,
        0.00453000209594919 * days_per_year,
        0.00237847173959480 * days_per_year,
        0.00162824170038242 * days_per_year,
    ]
    vz = [
        0.0,
        -0.00006902509938426 * days_per_year,
        -0.00019131288713706 * days_per_year,
        -0.00029589288865580 * days_per_year,
        -0.00095159225451337 * days_per_year,
    ]
    mass = [
        solar_mass,
        9.54791938424326609e-04 * solar_mass,
        2.85885980666130812e-04 * solar_mass,
        4.36624404335156298e-05 * solar_mass,
        5.15138902046611451e-05 * solar_mass,
    ]

    nb = 5
    offset_momentum(nb, vx, vy, vz, mass, solar_mass)

    steps = 0
    while steps < 100:
        advance(nb, x, y, z, vx, vy, vz, mass, 0.01)
        steps = steps + 1

    return int(energy(nb, x, y, z, vx, vy, vz, mass) * 1e9)
