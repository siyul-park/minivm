function build(d) {
    const node = {left: null, right: null};

    if (d > 0) {
        node.left = build(d - 1);
        node.right = build(d - 1);
    }

    return node;
}

function check(node) {
    if (node === null) {
        return 0;
    }
    return 1 + check(node.left) + check(node.right);
}

function run() {
    return check(build(9));
}
