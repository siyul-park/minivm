function run() {
    const entries = new Map();
    let sum = 0;
    for (let index = 0; index < 16384; index++) {
        entries.set((index * 7919) % 65521, index);
        if (index >= 512) {
            entries.delete(((index - 512) * 7919) % 65521);
        }
        if (index >= 256) {
            sum += entries.get(((index - 256) * 7919) % 65521) ?? 0;
        }
    }
    return sum + entries.size;
}
