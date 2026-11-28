def run():
    entries = {}
    total = 0
    for index in range(16384):
        entries[index * 7919 % 65521] = index
        if index >= 512:
            del entries[(index - 512) * 7919 % 65521]
        if index >= 256:
            total += entries.get((index - 256) * 7919 % 65521, 0)

    return total + len(entries)
