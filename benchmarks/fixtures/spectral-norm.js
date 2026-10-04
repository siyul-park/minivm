function a(i, j) {
    const k = i + j;
    return 1 / ((k * (k + 1)) / 2 + i + 1);
}

function atimes(n, u, out, transpose) {
    for (let i = 0; i < n; i++) {
        let sum = 0;

        for (let j = 0; j < n; j++) {
            const value = transpose ? a(j, i) : a(i, j);
            sum += value * u[j];
        }

        out[i] = sum;
    }
}

function run() {
    const n = 24;
    const u = Array(n).fill(1);
    const v = Array(n).fill(0);
    const tmp = Array(n).fill(0);

    for (let iteration = 0; iteration < 2; iteration++) {
        atimes(n, u, tmp, false);
        atimes(n, tmp, v, true);
        atimes(n, v, tmp, false);
        atimes(n, tmp, u, true);
    }

    let vbv = 0;
    let vv = 0;

    for (let i = 0; i < n; i++) {
        vbv += u[i] * v[i];
        vv += v[i] * v[i];
    }

    return Math.trunc(Math.sqrt(vbv / vv) * 1e9);
}
