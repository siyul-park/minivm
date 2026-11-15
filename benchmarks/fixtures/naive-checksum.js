function run() {
    let sum = 0;

    for (let i = 0; i < 2048; i++) {
        sum = (Math.imul(sum, 31) + 2 * (i % 1024) * (i % 97) + i) | 0;
    }

    return sum;
}
