def count_flips(perm):
    a = perm[:]
    flips = 0
    k = a[0]
    while k != 0:
        i = 0
        j = k
        while i < j:
            t = a[i]
            a[i] = a[j]
            a[j] = t
            i = i + 1
            j = j - 1
        flips = flips + 1
        k = a[0]
    return flips


def permute(a, k, permcount, checksum, maxflips):
    if k == 1:
        flips = count_flips(a)
        if flips > maxflips:
            maxflips = flips
        if permcount % 2 == 0:
            checksum = checksum + flips
        else:
            checksum = checksum - flips
        return (permcount + 1, checksum, maxflips)
    i = 0
    while i < k:
        permcount, checksum, maxflips = permute(a, k - 1, permcount, checksum, maxflips)
        if k % 2 == 0:
            tmp = a[i]
            a[i] = a[k - 1]
            a[k - 1] = tmp
        else:
            tmp = a[0]
            a[0] = a[k - 1]
            a[k - 1] = tmp
        i = i + 1
    return (permcount, checksum, maxflips)


def run():
    n = 6
    a = [0] * n
    i = 0
    while i < n:
        a[i] = i
        i = i + 1
    permcount, checksum, maxflips = permute(a, n, 0, 0, 0)
    return checksum * 1000 + maxflips
