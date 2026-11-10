def run():
    acc = 0
    for index in range(1024):
        acc = mix(acc, index)
    return acc
