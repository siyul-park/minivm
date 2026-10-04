local function advance(x, y, z, vx, vy, vz, mass, dt)
    for i = 1, 5 do
        for j = i + 1, 5 do
            local dx = x[i] - x[j]
            local dy = y[i] - y[j]
            local dz = z[i] - z[j]
            local d2 = dx * dx + dy * dy + dz * dz
            local mag = dt / (d2 * math.sqrt(d2))
            vx[i] = vx[i] - dx * mass[j] * mag
            vy[i] = vy[i] - dy * mass[j] * mag
            vz[i] = vz[i] - dz * mass[j] * mag
            vx[j] = vx[j] + dx * mass[i] * mag
            vy[j] = vy[j] + dy * mass[i] * mag
            vz[j] = vz[j] + dz * mass[i] * mag
        end
    end
    for i = 1, 5 do
        x[i] = x[i] + dt * vx[i]
        y[i] = y[i] + dt * vy[i]
        z[i] = z[i] + dt * vz[i]
    end
end

local function energy(x, y, z, vx, vy, vz, mass)
    local value = 0
    for i = 1, 5 do
        value = value + 0.5 * mass[i] * (vx[i] * vx[i] + vy[i] * vy[i] + vz[i] * vz[i])
        for j = i + 1, 5 do
            local dx = x[i] - x[j]
            local dy = y[i] - y[j]
            local dz = z[i] - z[j]
            value = value - mass[i] * mass[j] / math.sqrt(dx * dx + dy * dy + dz * dz)
        end
    end
    return value
end

function run()
    local pi = math.pi
    local solarMass = 4 * pi * pi
    local daysPerYear = 365.24
    local x = {
        0,
        4.841431442464721,
        8.34336671824458,
        12.943505513317835,
        15.379697114850945,
    }
    local y = {
        0,
        -1.1603200440274284,
        4.124798564124305,
        -15.111514016986319,
        -25.919314609987964,
    }
    local z = {
        0,
        -0.10362204447112311,
        -0.4035234171143214,
        -0.2237057963357768,
        0.17925877295037118,
    }
    local vx = {
        0,
        0.00166007664274403 * daysPerYear,
        0.00283009096225471 * daysPerYear,
        0.00296460137564761 * daysPerYear,
        0.00268067772490389 * daysPerYear,
    }
    local vy = {
        0,
        0.0076990111841974 * daysPerYear,
        0.00453000209594919 * daysPerYear,
        0.0023784717395948 * daysPerYear,
        0.00162824170038242 * daysPerYear,
    }
    local vz = {
        0,
        -0.00006902509938426 * daysPerYear,
        -0.00019131288713706 * daysPerYear,
        -0.0002958928886558 * daysPerYear,
        -0.00095159225451337 * daysPerYear,
    }
    local mass = {
        solarMass,
        0.0009547919384243266 * solarMass,
        0.0002858859806661308 * solarMass,
        0.00004366244043351563 * solarMass,
        0.00005151389020466115 * solarMass,
    }

    local px = 0
    local py = 0
    local pz = 0
    for i = 1, 5 do
        px = px + vx[i] * mass[i]
        py = py + vy[i] * mass[i]
        pz = pz + vz[i] * mass[i]
    end
    vx[1] = -px / solarMass
    vy[1] = -py / solarMass
    vz[1] = -pz / solarMass

    for _ = 1, 100 do
        advance(x, y, z, vx, vy, vz, mass, 0.01)
    end
    return math.ceil(energy(x, y, z, vx, vy, vz, mass) * 1e9)
end
