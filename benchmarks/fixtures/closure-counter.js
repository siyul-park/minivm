function run() {
    let value = 0;
    const next = () => ++value;

    for (let index = 0; index < 128; index++) {
        value = next();
    }

    return value;
}
