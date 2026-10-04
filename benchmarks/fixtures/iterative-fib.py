def run():
    current = 0
    next = 1
    for _ in range(30):
        current, next = next, current + next
    return current
