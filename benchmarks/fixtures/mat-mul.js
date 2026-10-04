function mul(n, a, b, out) {
    for (let i = 0; i < n; i++) {
        for (let j = 0; j < n; j++) {
            let sum = 0;
            for (let k = 0; k < n; k++) {
                sum += a[i * n + k] * b[k * n + j];
            }
            out[i * n + j] = sum;
        }
    }
}

function run() {
    const n = 16;
    const a = Array(n * n).fill(0);
    const b = Array(n * n).fill(0);
    const out = Array(n * n).fill(0);

    for (let i = 0; i < n; i++) {
        for (let j = 0; j < n; j++) {
            a[i * n + j] = (i * 7 + j * 3) % 13 - 6;
            b[i * n + j] = (i * 5 + j * 11) % 17 - 8;
        }
    }

    mul(n, a, b, out);
    return Math.trunc(
        out.reduce((sum, value) => sum + value, 0) * 1e6,
    );
}
