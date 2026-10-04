def run():
    total = 0
    for index in range(96):
        threshold = (index * 17 + 11) % 97
        if 37 < threshold:
            total += index % 7 + 1
        else:
            total += index % 5 + 2
    return total
