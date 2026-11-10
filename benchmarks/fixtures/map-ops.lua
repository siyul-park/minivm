function run()
    local entries = {}
    local size = 0
    for index = 0, 256 - 1 do
        local key = 134217728 + index * 7919 % 65521
        if entries[key] == nil then size = size + 1 end
        entries[key] = index
    end
    local sum = 0
    for index = 0, 256 - 1 do
        local value = entries[134217728 + index * 7919 % 65521 + index % 2 * 65521]
        if value ~= nil then sum = sum + value + 1 end
    end
    for index = 0, 256 - 1, 2 do entries[134217728 + index * 7919 % 65521] = index * 3 + 1 end
    for index = 0, 256 - 1, 4 do
        entries[134217728 + index * 7919 % 65521] = nil
        size = size - 1
    end
    for index = 0, 256 - 1 do
        local value = entries[134217728 + index * 7919 % 65521]
        if value ~= nil then sum = sum + value end
    end
    return sum * 1024 + size
end
