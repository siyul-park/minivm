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
    local result, big = 0, ""
    for i = 0, 511 do
        local tok = digits((i * 2654435761) % 99999)
        for j = 1, #tok do result = (result + string.byte(tok, j) * j) % 1000000007 end
        big = big .. tok .. " "
    end
    return result + #big
end
