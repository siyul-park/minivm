function run()
    local current = 0
    local next = 1
    for _ = 1, 30 do
        local sum = current + next
        current = next
        next = sum
    end
    return current
end
