def run():
    total = 0
    for i in range(2048):
        total = total * 31 + 2 * (i % 1024) * (i % 97) + i
        total = (total + 2147483648) % 4294967296 - 2147483648
    return total
