function build(d)
    local n = {left=false, right=false}
    if d > 0 then
        n.left = build(d - 1)
        n.right = build(d - 1)
    end
    return n
end
function check(n)
    if not n then return 0 end
    return 1 + check(n.left) + check(n.right)
end
function run()
    return check(build(9))
end
