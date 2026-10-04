function run() {
    const composite = new Int32Array(256);

    for (let value = 2; value * value < 256; value++) {
        for (
            let multiple = value * value;
            multiple < 256;
            multiple += value
        ) {
            composite[multiple] = 1;
        }
    }

    let count = 0;
    for (let value = 2; value < 256; value++) {
        if (composite[value] === 0) {
            count++;
        }
    }

    return count;
}
