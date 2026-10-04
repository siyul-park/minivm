local function item_check(t)
    if t.left == nil then
        return t.item
    end
    return t.item + item_check(t.left) - item_check(t.right)
end

local function bottom_up_tree(item, depth)
    if depth > 0 then
        return {
            item = item,
            left = bottom_up_tree(2 * item - 1, depth - 1),
            right = bottom_up_tree(2 * item, depth - 1),
        }
    end
    return {item = item, left = nil, right = nil}
end

function run()
    local min_depth, max_depth = 4, 6
    local result = item_check(bottom_up_tree(0, max_depth + 1))
    local long_lived_tree = bottom_up_tree(0, max_depth)

    local depth = min_depth
    while depth <= max_depth do
        local iterations = 1
        local shift = 0
        while shift < max_depth - depth + min_depth do
            iterations = iterations * 2
            shift = shift + 1
        end

        local acc = 0
        local i = 1
        while i <= iterations do
            acc = acc + item_check(bottom_up_tree(i, depth))
            acc = acc + item_check(bottom_up_tree(-i, depth))
            i = i + 1
        end
        result = result + acc
        depth = depth + 2
    end

    return result + item_check(long_lived_tree)
end
