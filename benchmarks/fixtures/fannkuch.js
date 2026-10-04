function countFlips(perm) {
    const a = perm.slice();
    let flips = 0;

    for (let k = a[0]; k !== 0; k = a[0]) {
        for (let i = 0, j = k; i < j; i++, j--) {
            [a[i], a[j]] = [a[j], a[i]];
        }
        flips++;
    }

    return flips;
}

function permute(a, k, permcount, checksum, maxflips) {
    if (k === 1) {
        const flips = countFlips(a);
        maxflips = Math.max(maxflips, flips);
        checksum += permcount % 2 === 0 ? flips : -flips;
        return [permcount + 1, checksum, maxflips];
    }

    for (let i = 0; i < k; i++) {
        [permcount, checksum, maxflips] = permute(
            a,
            k - 1,
            permcount,
            checksum,
            maxflips,
        );

        if (k % 2 === 0) {
            [a[i], a[k - 1]] = [a[k - 1], a[i]];
        } else {
            [a[0], a[k - 1]] = [a[k - 1], a[0]];
        }
    }

    return [permcount, checksum, maxflips];
}

function run() {
    const a = Array.from({length: 6}, (_, i) => i);
    const [, checksum, maxflips] = permute(a, 6, 0, 0, 0);
    return checksum * 1000 + maxflips;
}
