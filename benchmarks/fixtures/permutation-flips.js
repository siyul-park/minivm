function walk(depth) {
    if (depth === 0) {
        return 0;
    }

    const array = [];
    for (let index = 0; index < 24; index++) {
        array[index] = 24 - 1 - index;
    }

    let low = 0;
    let high = 24 - 1;
    while (low < high) {
        const value = array[low];
        array[low] = array[high];
        array[high] = value;
        low++;
        high--;
    }

    return array[24 - 1] + walk(depth - 1);
}

function run() {
    return walk(64);
}
