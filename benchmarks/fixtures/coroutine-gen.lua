function run()
    local generator = coroutine.create(function()
        for index = 0, 512 - 1 do
            coroutine.yield((index * index + 7 * index) % 1009)
        end
    end)
    local sum = 0
    while true do
        local _, value = coroutine.resume(generator)
        if coroutine.status(generator) == "dead" then break end
        sum = (sum + value) % 65521
    end
    return sum
end
