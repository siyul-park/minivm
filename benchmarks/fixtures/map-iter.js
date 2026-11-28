function run() {
    const entries = new Map();
    for (let index = 0; index < 1024; index++) {
        entries.set((index * 7919) % 65521, index);
    }
    for (let index = 0; index < 1024; index += 4) {
        entries.delete((index * 7919) % 65521);
    }

    let sum = 0;
    for (let pass = 0; pass < 32; pass++) {
        for (const key of entries.keys()) {
            sum += key + entries.get(key);
        }
    }
    return sum + entries.size;
}
