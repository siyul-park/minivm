function run() {
    const base = 134217728;
    const entries = new Map();
    for (let index = 0; index < 256; index++) {
        entries.set(base + ((index * 7919) % 65521), index);
    }

    let sum = 0;
    for (let index = 0; index < 256; index++) {
        const key = base + ((index * 7919) % 65521) + (index % 2) * 65521;
        const value = entries.get(key);
        if (value !== undefined) {
            sum += value + 1;
        }
    }
    for (let index = 0; index < 256; index += 2) {
        entries.set(base + ((index * 7919) % 65521), index * 3 + 1);
    }
    for (let index = 0; index < 256; index += 4) {
        entries.delete(base + ((index * 7919) % 65521));
    }
    for (let index = 0; index < 256; index++) {
        sum += entries.get(base + ((index * 7919) % 65521)) ?? 0;
    }

    return sum * 1024 + entries.size;
}
