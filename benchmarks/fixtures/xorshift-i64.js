function run() {
    let state = 2463534242n;
    let total = 0n;

    for (let i = 0; i < 256; i++) {
        state = BigInt.asIntN(
            64,
            state ^ BigInt.asIntN(64, state << 13n),
        );
        state = BigInt.asIntN(
            64,
            state ^ (BigInt.asUintN(64, state) >> 7n),
        );
        state = BigInt.asIntN(
            64,
            state ^ BigInt.asIntN(64, state << 17n),
        );
        total = BigInt.asIntN(64, total + state);
    }

    return total;
}
