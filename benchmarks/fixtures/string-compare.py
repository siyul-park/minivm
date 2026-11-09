digits = "0123456789"


def _number(value):
    if value == 0:
        return digits[0]

    text = ""
    while value > 0:
        text = digits[value % 10] + text
        value //= 10

    return text


def run():
    words = [_number((index * 2654435761) % 997) for index in range(64)]

    count = 0
    for r in range(256):
        key = _number((r * 31) % 997)
        for index in range(64):
            if words[index] == key:
                count += index + 1

    return count
