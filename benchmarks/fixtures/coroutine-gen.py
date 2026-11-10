def run():
    def generate():
        for index in range(512):
            yield (index * index + 7 * index) % 1009

    total = 0
    for value in generate():
        total = (total + value) % 65521

    return total
