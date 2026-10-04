function fib(n, self) {
    if (n < 2) {
        return n;
    }
    return self(n - 1, self) + self(n - 2, self);
}

function run() {
    return fib(20, fib);
}
