def solve(row, n, cols, diag1, diag2):
    if row == n:
        return 1

    count = 0
    for col in range(n):
        d1 = row - col + n - 1
        d2 = row + col
        if not cols[col] and not diag1[d1] and not diag2[d2]:
            cols[col] = True
            diag1[d1] = True
            diag2[d2] = True
            count += solve(row + 1, n, cols, diag1, diag2)
            cols[col] = False
            diag1[d1] = False
            diag2[d2] = False

    return count


def run():
    return solve(
        0,
        7,
        [False] * 7,
        [False] * 13,
        [False] * 13,
    )
