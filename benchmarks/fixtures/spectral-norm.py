import math


def eval_a(i, j):
    return 1.0 / float((i + j) * (i + j + 1) // 2 + i + 1)


def eval_a_times_u(n, u, out):
    i = 0
    while i < n:
        s = 0.0
        j = 0
        while j < n:
            s = s + eval_a(i, j) * u[j]
            j = j + 1
        out[i] = s
        i = i + 1


def eval_at_times_u(n, u, out):
    i = 0
    while i < n:
        s = 0.0
        j = 0
        while j < n:
            s = s + eval_a(j, i) * u[j]
            j = j + 1
        out[i] = s
        i = i + 1


def eval_ata_times_u(n, u, out, tmp):
    eval_a_times_u(n, u, tmp)
    eval_at_times_u(n, tmp, out)


def run():
    n = 24
    u = [1.0] * n
    v = [0.0] * n
    tmp = [0.0] * n

    it = 0
    while it < 2:
        eval_ata_times_u(n, u, v, tmp)
        eval_ata_times_u(n, v, u, tmp)
        it = it + 1

    vbv = 0.0
    vv = 0.0
    i = 0
    while i < n:
        vbv = vbv + u[i] * v[i]
        vv = vv + v[i] * v[i]
        i = i + 1

    result = math.sqrt(vbv / vv)
    return int(result * 1e9)
