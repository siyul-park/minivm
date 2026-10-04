local function mul(n, a, b, out)
    for i = 0, n - 1 do
        for j = 0, n - 1 do
            local sum = 0.0
            for k = 0, n - 1 do sum = sum + a[i*n+k] * b[k*n+j] end
            out[i*n+j] = sum
        end
    end
end

function run()
    local n = 16
    local a, b, out = {}, {}, {}
    for i = 0, n*n-1 do a[i], b[i], out[i] = 0.0, 0.0, 0.0 end
    for i = 0, n-1 do
        for j = 0, n-1 do
            a[i*n+j] = (i*7+j*3) % 13 - 6.0
            b[i*n+j] = (i*5+j*11) % 17 - 8.0
        end
    end
    mul(n, a, b, out)
    local sum = 0.0
    for i = 0, n*n-1 do sum = sum + out[i] end
    return math.floor(sum * 1e6)
end
