function run() {
    let root = [null];

    for (let index = 1; index < 128; index++) {
        root = [root];
    }

    return 128 + root.length - 1;
}
