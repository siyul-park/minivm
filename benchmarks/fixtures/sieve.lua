function run()
    local composite = {}
    for index = 0, 256 - 1 do composite[index] = 0 end
    for value = 2, 256 - 1 do
        if value * value >= 256 then break end
        for multiple = value * value, 256 - 1, value do composite[multiple] = 1 end
    end
    local count = 0
    for value = 2, 256 - 1 do if composite[value] == 0 then count = count + 1 end end
    return count
end
