def make_list(n, seed):
    xs = [0] * n
    s = seed
    i = 0
    while i < n:
        s = (s * 1103515245 + 12345) % 2147483648
        xs[i] = s % 1000000
        i = i + 1
    return xs


def insertion_sort(xs, n):
    i = 1
    while i < n:
        key = xs[i]
        j = i - 1
        while j >= 0 and xs[j] > key:
            xs[j + 1] = xs[j]
            j = j - 1
        xs[j + 1] = key
        i = i + 1


def run():
    n = 128
    rounds = 2
    checksum = 0
    seed = 1
    r = 0
    while r < rounds:
        xs = make_list(n, seed + r)
        insertion_sort(xs, n)
        i = 0
        while i < n:
            checksum = checksum + xs[i] * (i % 7)
            i = i + 1
        checksum = checksum % 1000000007
        r = r + 1
    return checksum
