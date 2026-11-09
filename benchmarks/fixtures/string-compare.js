function digits(n) {
    if (n === 0) {
        return "0";
    }

    let text = "";
    while (n > 0) {
        const digit = n % 10;
        text = String.fromCharCode(48 + digit) + text;
        n = Math.trunc(n / 10);
    }

    return text;
}

function run() {
    const words = [];
    for (let i = 0; i < 64; i++) {
        words.push(digits((i * 2654435761) % 997));
    }

    let count = 0;
    for (let r = 0; r < 256; r++) {
        const key = digits((r * 31) % 997);
        for (let i = 0; i < 64; i++) {
            if (words[i] === key) {
                count += i + 1;
            }
        }
    }

    return count;
}
