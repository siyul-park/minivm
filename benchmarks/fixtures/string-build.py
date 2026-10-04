digits = "0123456789"


def number(value):
    if value == 0:
        return digits[0]

    text = ""
    while value > 0:
        text = digits[value % 10] + text
        value //= 10

    return text


def run():
    result = 0
    text = ""

    for index in range(512):
        token = number((index * 2654435761) % 99999)

        for offset in range(len(token)):
            result = (
                result + ord(token[offset]) * (offset + 1)
            ) % 1000000007

        text += token + " "

    return result + len(text)
