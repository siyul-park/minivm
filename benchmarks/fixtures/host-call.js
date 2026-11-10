function run() {
    let acc = 0;
    for (let index = 0; index < 1024; index++) {
        acc = mix(acc, index);
    }
    return acc;
}
