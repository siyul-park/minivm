MASK = (1 << 64) - 1


def _signed(value):
    value &= MASK
    if value >= 1 << 63:
        value -= 1 << 64
    return value


def fib(n):
    if n < 2:
        return _signed(n + (1 << 50))
    return _signed(fib(n - 1) + fib(n - 2))


def run():
    return fib(25)
