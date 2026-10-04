function run() {
    let total = 0;

    for (let index = 0; index < 96; index++) {
        const threshold = (index * 17 + 11) % 97;
        if (37 < threshold) {
            total += index % 7 + 1;
        } else {
            total += index % 5 + 2;
        }
    }

    return total;
}
