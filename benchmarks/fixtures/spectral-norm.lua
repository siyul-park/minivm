local function a(i, j)
    local k = i + j
    return 1.0 / ((k * (k + 1)) / 2 + i + 1)
end
local function atimes(n, u, out, transpose)
    for i = 0, n - 1 do
        local sum = 0.0
        for j = 0, n - 1 do
            if transpose then sum = sum + a(j, i) * u[j] else sum = sum + a(i, j) * u[j] end
        end
        out[i] = sum
    end
end
function run()
    local n, u, v, tmp = 24, {}, {}, {}
    for i = 0, n-1 do u[i], v[i], tmp[i] = 1.0, 0.0, 0.0 end
    for _ = 1, 2 do
        atimes(n, u, tmp, false); atimes(n, tmp, v, true)
        atimes(n, v, tmp, false); atimes(n, tmp, u, true)
    end
    local vbv, vv = 0.0, 0.0
    for i = 0, n-1 do vbv = vbv + u[i] * v[i]; vv = vv + v[i] * v[i] end
    return math.floor(math.sqrt(vbv / vv) * 1e9)
end
