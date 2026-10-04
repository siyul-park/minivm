function itemCheck(tree) {
    if (tree.left === null) {
        return tree.item;
    }
    return tree.item + itemCheck(tree.left) - itemCheck(tree.right);
}

function bottomUpTree(item, depth) {
    if (depth > 0) {
        return {
            item,
            left: bottomUpTree(2 * item - 1, depth - 1),
            right: bottomUpTree(2 * item, depth - 1),
        };
    }

    return {
        item,
        left: null,
        right: null,
    };
}

function run() {
    const minDepth = 4;
    const maxDepth = 6;
    let result = itemCheck(bottomUpTree(0, maxDepth + 1));
    const longLivedTree = bottomUpTree(0, maxDepth);

    for (let depth = minDepth; depth <= maxDepth; depth += 2) {
        let iterations = 1;
        for (
            let shift = 0;
            shift < maxDepth - depth + minDepth;
            shift++
        ) {
            iterations *= 2;
        }

        let total = 0;
        for (let index = 1; index <= iterations; index++) {
            total += itemCheck(bottomUpTree(index, depth));
            total += itemCheck(bottomUpTree(-index, depth));
        }
        result += total;
    }

    return result + itemCheck(longLivedTree);
}
