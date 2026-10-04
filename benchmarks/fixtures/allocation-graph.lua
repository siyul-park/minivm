function run()
    local root = {false}
    for _ = 2, 128 do
        root = {root}
    end
    return 128 + #root - 1
end
