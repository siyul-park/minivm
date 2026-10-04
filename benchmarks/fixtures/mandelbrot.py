def escape_count(cr, ci, max_iter):
    zr = 0.0
    zi = 0.0
    i = 0
    while i < max_iter:
        zr2 = zr * zr
        zi2 = zi * zi
        if zr2 + zi2 > 4.0:
            return i
        new_zr = zr2 - zi2 + cr
        new_zi = 2.0 * zr * zi + ci
        zr = new_zr
        zi = new_zi
        i = i + 1
    return max_iter


def run():
    width = 16
    height = 16
    max_iter = 50
    x_min = -2.0
    x_max = 1.0
    y_min = -1.5
    y_max = 1.5

    total = 0
    py = 0
    while py < height:
        cy = y_min + (y_max - y_min) * float(py) / float(height - 1)
        px = 0
        while px < width:
            cx = x_min + (x_max - x_min) * float(px) / float(width - 1)
            total = total + escape_count(cx, cy, max_iter)
            px = px + 1
        py = py + 1

    return total
