local function escape(cr, ci, max_iter)
    local zr, zi = 0.0, 0.0
    for i = 0, max_iter - 1 do
        local zr2, zi2 = zr * zr, zi * zi
        if zr2 + zi2 > 4.0 then return i end
        local new_zr = zr2 - zi2 + cr
        local new_zi = 2.0 * zr * zi + ci
        zr = new_zr
        zi = new_zi
    end
    return max_iter
end

function run()
    local total = 0
    for py = 0, 15 do
        local cy = -1.5 + 3.0 * py / 15.0
        for px = 0, 15 do
            local cx = -2.0 + 3.0 * px / 15.0
            total = total + escape(cx, cy, 50)
        end
    end
    return total
end
