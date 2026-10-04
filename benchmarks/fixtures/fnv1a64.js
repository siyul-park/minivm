function run() {
    let hash = 0xcbf29ce484222325n;

    for (let i = 0; i < 1024; i++) {
        hash = BigInt.asIntN(64, hash ^ BigInt(i & 0xff));
        hash = BigInt.asIntN(64, hash * 1099511628211n);
    }

    return hash;
}
