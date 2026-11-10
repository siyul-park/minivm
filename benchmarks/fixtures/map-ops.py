def run():
    entries = {}
    for index in range(256):
        entries[134217728 + index * 7919 % 65521] = index

    total = 0
    for index in range(256):
        value = entries.get(134217728 + index * 7919 % 65521 + index % 2 * 65521)
        if value is not None:
            total += value + 1
    for index in range(0, 256, 2):
        entries[134217728 + index * 7919 % 65521] = index * 3 + 1
    for index in range(0, 256, 4):
        del entries[134217728 + index * 7919 % 65521]
    for index in range(256):
        total += entries.get(134217728 + index * 7919 % 65521, 0)

    return total * 1024 + len(entries)
