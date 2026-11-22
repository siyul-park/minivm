function run()
    local entries = {}
    local size = 0
    for index = 0, 1024 - 1 do
        entries[index * 7919 % 65521] = index
        size = size + 1
    end
    for index = 0, 1024 - 1, 4 do
        entries[index * 7919 % 65521] = nil
        size = size - 1
    end
    local sum = 0
    for pass = 1, 32 do
        for key in pairs(entries) do sum = sum + key + entries[key] end
    end
    return sum + size
end
