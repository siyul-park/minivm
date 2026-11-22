local function token(index)
    return tostring(index * 7919 % 65521) .. "-" .. tostring(index)
end
function run()
    local words, probes, absent = {}, {}, {}
    for index = 0, 255 do
        words[index] = token(index)
        probes[index] = token(index)
        absent[index] = token(index + 256)
    end
    local counts = {}
    local size = 0
    for index = 0, 255 do
        counts[words[index]] = index
        size = size + 1
    end
    counts["total"] = 0
    size = size + 1
    local sum = 0
    for round = 0, 16384 - 1 do
        local probe = probes[round * 7919 % 256]
        counts[probe] = counts[probe] + 1
        counts["total"] = counts["total"] + 1
        if counts[absent[round % 256]] ~= nil then sum = sum + 1000 end
    end
    for index = 0, 255 do sum = sum + counts[words[index]] end
    return sum + counts["total"] * 3 + size
end
