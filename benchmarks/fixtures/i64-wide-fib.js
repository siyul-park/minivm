function fib(n) {
    if (n < 2n) {
        return BigInt.asIntN(64, n + 1125899906842624n);
    }
    return BigInt.asIntN(64, fib(n - 1n) + fib(n - 2n));
}

function run() {
    return fib(25n);
}
