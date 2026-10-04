def run():
    root = [None]
    for _ in range(1, 128):
        root = [root]
    return 128 + len(root) - 1
