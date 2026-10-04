def fib(n, self):
    if n < 2:
        return n
    return self(n - 1, self) + self(n - 2, self)


def run():
    return fib(20, fib)
