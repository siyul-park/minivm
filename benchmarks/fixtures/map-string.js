function token(index) {
    return String((index * 7919) % 65521) + "-" + String(index);
}

function run() {
    const words = [];
    const probes = [];
    const absent = [];
    for (let index = 0; index < 256; index++) {
        words.push(token(index));
        probes.push(token(index));
        absent.push(token(index + 256));
    }

    const counts = new Map();
    for (let index = 0; index < 256; index++) {
        counts.set(words[index], index);
    }
    counts.set("total", 0);

    let sum = 0;
    for (let round = 0; round < 16384; round++) {
        const probe = probes[(round * 7919) % 256];
        counts.set(probe, counts.get(probe) + 1);
        counts.set("total", counts.get("total") + 1);
        if (counts.has(absent[round % 256])) {
            sum += 1000;
        }
    }
    for (let index = 0; index < 256; index++) {
        sum += counts.get(words[index]);
    }
    return sum + counts.get("total") * 3 + counts.size;
}
