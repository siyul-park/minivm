def run():
    entries = {}
    for index in range(1024):
        entries[index * 7919 % 65521] = index
    for index in range(0, 1024, 4):
        del entries[index * 7919 % 65521]

    total = 0
    for _ in range(32):
        for key in entries:
            total += key + entries[key]

    return total + len(entries)
