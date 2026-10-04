class Node:
    def __init__(self, item, left, right):
        self.item = item
        self.left = left
        self.right = right


def item_check(t):
    if t is None:
        return 0
    if t.left is None:
        return t.item
    return t.item + item_check(t.left) - item_check(t.right)


def bottom_up_tree(item, depth):
    if depth > 0:
        return Node(item, bottom_up_tree(2 * item - 1, depth - 1), bottom_up_tree(2 * item, depth - 1))
    return Node(item, None, None)


def run():
    min_depth = 4
    max_depth = 6
    stretch_tree = bottom_up_tree(0, max_depth + 1)
    checksum = item_check(stretch_tree)

    long_lived_tree = bottom_up_tree(0, max_depth)

    depth = min_depth
    while depth <= max_depth:
        iterations = 1
        shift = 0
        while shift < (max_depth - depth + min_depth):
            iterations = iterations * 2
            shift = shift + 1
        acc = 0
        i = 1
        while i <= iterations:
            t1 = bottom_up_tree(i, depth)
            acc = acc + item_check(t1)
            t2 = bottom_up_tree(0 - i, depth)
            acc = acc + item_check(t2)
            i = i + 1
        checksum = checksum + acc
        depth = depth + 2

    checksum = checksum + item_check(long_lived_tree)
    return checksum
