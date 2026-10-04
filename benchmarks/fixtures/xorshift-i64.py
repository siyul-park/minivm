def run():
    state = 2463534242
    mask = (1 << 64) - 1
    total = 0
    for _ in range(256):
        state = (state ^ (state << 13)) & mask
        state = (state ^ (state >> 7)) & mask
        state = (state ^ (state << 17)) & mask
        if state >= 1 << 63:
            signed = state - (1 << 64)
        else:
            signed = state
        total = (total + signed) & mask
    if total >= 1 << 63:
        total -= 1 << 64
    return total
