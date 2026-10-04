function run()
    local total = 0
    for index = 0, 96 - 1 do
        local threshold = (index * 17 + 11) % 97
        if 37 < threshold then
            total = total + index % 7 + 1
        else
            total = total + index % 5 + 2
        end
    end
    return total
end
