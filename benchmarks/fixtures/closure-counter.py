def run():
    value = [0]

    def next():
        value[0] += 1
        return value[0]

    for _ in range(128):
        value[0] = next()

    return value[0]
