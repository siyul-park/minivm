def run():
    h = 0xcbf29ce484222325
    mask = (1 << 64) - 1
    for value in range(1024):
        h ^= value & 0xff
        h = (h * 1099511628211) & mask
    if h >= 1 << 63:
        h -= 1 << 64
    return h
