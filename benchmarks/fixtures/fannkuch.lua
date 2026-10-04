local function count_flips(perm)
    local a = {}
    for i = 1, #perm do a[i] = perm[i] end
    local flips = 0
    local k = a[1]
    while k ~= 0 do
        local i, j = 1, k + 1
        while i < j do
            a[i], a[j] = a[j], a[i]
            i, j = i + 1, j - 1
        end
        flips = flips + 1
        k = a[1]
    end
    return flips
end

local function permute(a, k, permcount, checksum, maxflips)
    if k == 1 then
        local flips = count_flips(a)
        if flips > maxflips then maxflips = flips end
        if permcount % 2 == 0 then checksum = checksum + flips else checksum = checksum - flips end
        return permcount + 1, checksum, maxflips
    end
    for i = 1, k do
        permcount, checksum, maxflips = permute(a, k - 1, permcount, checksum, maxflips)
        if k % 2 == 0 then a[i], a[k] = a[k], a[i] else a[1], a[k] = a[k], a[1] end
    end
    return permcount, checksum, maxflips
end

function run()
    local a = {}
    for i = 1, 6 do a[i] = i - 1 end
    local _, checksum, maxflips = permute(a, 6, 0, 0, 0)
    return checksum * 1000 + maxflips
end
