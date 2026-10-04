def run():
    composite = [0] * 256

    value = 2
    while value * value < 256:
        multiple = value * value
        while multiple < 256:
            composite[multiple] = 1
            multiple += value
        value += 1

    count = 0
    for value in range(2, 256):
        if composite[value] == 0:
            count += 1

    return count
