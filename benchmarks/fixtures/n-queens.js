function solve(row, n, cols, diag1, diag2) {
    if (row === n) {
        return 1;
    }

    let count = 0;
    for (let col = 0; col < n; col++) {
        const d1 = row - col + n - 1;
        const d2 = row + col;

        if (!cols[col] && !diag1[d1] && !diag2[d2]) {
            cols[col] = diag1[d1] = diag2[d2] = true;
            count += solve(row + 1, n, cols, diag1, diag2);
            cols[col] = diag1[d1] = diag2[d2] = false;
        }
    }

    return count;
}

function run() {
    return solve(
        0,
        7,
        Array(7).fill(false),
        Array(13).fill(false),
        Array(13).fill(false),
    );
}
