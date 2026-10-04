def walk(depth):
    if depth == 0:
        return 0

    array = [0] * 24
    for index in range(24):
        array[index] = 24 - 1 - index

    low = 0
    high = 24 - 1
    while low < high:
        array[low], array[high] = array[high], array[low]
        low += 1
        high -= 1

    return array[24 - 1] + walk(depth - 1)


def run():
    return walk(64)
