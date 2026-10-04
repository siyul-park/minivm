function run() {
    let current = 0;
    let next = 1;

    for (let index = 0; index < 30; index++) {
        const sum = current + next;
        current = next;
        next = sum;
    }

    return current;
}
