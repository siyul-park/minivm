function run()
    local entries = {}
    local size = 0
    local sum = 0
    for index = 0, 16384 - 1 do
        local key = index * 7919 % 65521
        if entries[key] == nil then size = size + 1 end
        entries[key] = index
        if index >= 512 then
            entries[(index - 512) * 7919 % 65521] = nil
            size = size - 1
        end
        if index >= 256 then
            local value = entries[(index - 256) * 7919 % 65521]
            if value ~= nil then sum = sum + value end
        end
    end
    return sum + size
end
