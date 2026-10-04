local function make_list(n, seed)
    local xs, s = {}, seed
    for i = 1, n do
        local hi = math.floor(s / 65536)
        local lo = s - hi * 65536
        s = ((1103515245 * lo + 12345) + ((1103515245 * hi) % 32768) * 65536) % 2147483648
        xs[i] = s % 1000000
    end
    return xs
end

local function sort(xs, n)
    for i = 2, n do
        local key, j = xs[i], i - 1
        while j >= 1 and xs[j] > key do xs[j+1] = xs[j]; j = j - 1 end
        xs[j+1] = key
    end
end

function run()
    local n, result = 128, 0
    for round = 0, 1 do
        local xs = make_list(n, 1 + round)
        sort(xs, n)
        for i = 1, n do result = result + xs[i] * ((i - 1) % 7) end
        result = result % 1000000007
    end
    return result
end
