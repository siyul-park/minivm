function run()
    local acc = 0
    for index = 0, 1024 - 1 do
        acc = mix(acc, index)
    end
    return acc
end
