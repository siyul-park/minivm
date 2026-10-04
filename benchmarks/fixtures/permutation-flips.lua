function walk(depth)
    if depth == 0 then
        return 0
    end

    local array = {}
    for index = 0, 24 - 1 do
        array[index] = 24 - 1 - index
    end

    local low = 0
    local high = 24 - 1
    while low < high do
        local value = array[low]
        array[low] = array[high]
        array[high] = value
        low = low + 1
        high = high - 1
    end

    return array[24 - 1] + walk(depth - 1)
end

function run()
    return walk(64)
end
