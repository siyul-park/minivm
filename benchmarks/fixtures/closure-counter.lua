function run()
    local value = 0
    local function next()
        value = value + 1
        return value
    end
    for _ = 1, 128 do
        value = next()
    end
    return value
end
