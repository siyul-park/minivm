local function solve(row, n, cols, diag1, diag2)
    if row == n then return 1 end
    local count = 0
    for col = 0, n - 1 do
        local d1, d2 = row - col + n - 1, row + col
        if not cols[col] and not diag1[d1] and not diag2[d2] then
            cols[col], diag1[d1], diag2[d2] = true, true, true
            count = count + solve(row + 1, n, cols, diag1, diag2)
            cols[col], diag1[d1], diag2[d2] = false, false, false
        end
    end
    return count
end

function run()
    local cols, diag1, diag2 = {}, {}, {}
    for i = 0, 6 do cols[i] = false end
    for i = 0, 12 do diag1[i], diag2[i] = false, false end
    return solve(0, 7, cols, diag1, diag2)
end
