function escape(cr, ci, maxIter) {
    let zr = 0;
    let zi = 0;

    for (let i = 0; i < maxIter; i++) {
        const zr2 = zr * zr;
        const zi2 = zi * zi;
        if (zr2 + zi2 > 4) {
            return i;
        }
        [zr, zi] = [zr2 - zi2 + cr, 2 * zr * zi + ci];
    }

    return maxIter;
}

function run() {
    let total = 0;

    for (let py = 0; py < 16; py++) {
        const cy = -1.5 + 3 * py / 15;

        for (let px = 0; px < 16; px++) {
            total += escape(-2 + 3 * px / 15, cy, 50);
        }
    }

    return total;
}
