class Node:
    def __init__(self):
        self.left = None
        self.right = None


def build(depth):
    node = Node()
    if depth > 0:
        node.left = build(depth - 1)
        node.right = build(depth - 1)
    return node


def check(node):
    if node is None:
        return 0
    return 1 + check(node.left) + check(node.right)


def run():
    return check(build(9))
