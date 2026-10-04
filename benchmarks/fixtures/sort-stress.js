function makeList(n, seed) {
    const xs = new Array(n);
    let state = seed;

    for (let i = 0; i < n; i++) {
        const high = Math.trunc(state / 65536);
        const low = state - high * 65536;
        state = (
            (1103515245 * low + 12345)
            + ((1103515245 * high) % 32768) * 65536
        ) % 2147483648;
        xs[i] = state % 1000000;
    }

    return xs;
}

function sort(xs) {
    for (let i = 1; i < xs.length; i++) {
        const key = xs[i];
        let j = i - 1;

        while (j >= 0 && xs[j] > key) {
            xs[j + 1] = xs[j];
            j--;
        }

        xs[j + 1] = key;
    }
}

function run() {
    const n = 128;
    let result = 0;

    for (let round = 0; round < 2; round++) {
        const xs = makeList(n, 1 + round);
        sort(xs);

        for (let i = 0; i < n; i++) {
            result += xs[i] * (i % 7);
        }
        result %= 1000000007;
    }

    return result;
}
