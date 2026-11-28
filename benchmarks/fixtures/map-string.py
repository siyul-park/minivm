def _token(index):
    return str(index * 7919 % 65521) + "-" + str(index)


def run():
    words = [_token(index) for index in range(256)]
    probes = [_token(index) for index in range(256)]
    absent = [_token(index + 256) for index in range(256)]

    counts = {}
    for index in range(256):
        counts[words[index]] = index
    counts["total"] = 0

    total = 0
    for round in range(16384):
        probe = probes[round * 7919 % 256]
        counts[probe] = counts[probe] + 1
        counts["total"] = counts["total"] + 1
        if absent[round % 256] in counts:
            total += 1000
    for index in range(256):
        total += counts[words[index]]

    return total + counts["total"] * 3 + len(counts)
