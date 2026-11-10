function run() {
    function* generate() {
        for (let index = 0; index < 512; index++) {
            yield (index * index + 7 * index) % 1009;
        }
    }

    const generator = generate();
    let sum = 0;
    for (let step = generator.next(); !step.done; step = generator.next()) {
        sum = (sum + step.value) % 65521;
    }

    return sum;
}
