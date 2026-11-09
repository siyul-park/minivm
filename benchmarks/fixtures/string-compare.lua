local function digits(n)
    if n == 0 then return "0" end
    local s = ""
    while n > 0 do
        local d = n % 10
        s = string.char(48 + d) .. s
        n = math.floor(n / 10)
    end
    return s
end
function run()
    local words = {}
    for i = 0, 63 do words[i] = digits((i * 2654435761) % 997) end
    local count = 0
    for r = 0, 255 do
        local key = digits((r * 31) % 997)
        for i = 0, 63 do
            if words[i] == key then count = count + i + 1 end
        end
    end
    return count
end
