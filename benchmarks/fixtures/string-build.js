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
    let result = 0;
    let text = "";

    for (let i = 0; i < 512; i++) {
        const token = digits((i * 2654435761) % 99999);

        for (let j = 0; j < token.length; j++) {
            result = (
                result
                + token.charCodeAt(j) * (j + 1)
            ) % 1000000007;
        }

        text += token + " ";
    }

    return result + text.length;
}
