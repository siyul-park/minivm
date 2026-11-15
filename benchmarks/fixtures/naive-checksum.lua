function run()
    local sum = 0
    for i = 0, 2047 do
        sum = (sum * 31 + 2 * (i % 1024) * (i % 97) + i + 2147483648) % 4294967296 - 2147483648
    end
    return sum
end
