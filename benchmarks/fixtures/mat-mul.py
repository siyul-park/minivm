def matmul(n, a, b, out):
    i = 0
    while i < n:
        j = 0
        while j < n:
            s = 0.0
            k = 0
            while k < n:
                s = s + a[i * n + k] * b[k * n + j]
                k = k + 1
            out[i * n + j] = s
            j = j + 1
        i = i + 1


def run():
    n = 16
    a = [0.0] * (n * n)
    b = [0.0] * (n * n)
    out = [0.0] * (n * n)

    i = 0
    while i < n:
        j = 0
        while j < n:
            a[i * n + j] = float((i * 7 + j * 3) % 13) - 6.0
            b[i * n + j] = float((i * 5 + j * 11) % 17) - 8.0
            j = j + 1
        i = i + 1

    matmul(n, a, b, out)

    checksum = 0.0
    idx = 0
    while idx < n * n:
        checksum = checksum + out[idx]
        idx = idx + 1

    return int(checksum * 1e6)
