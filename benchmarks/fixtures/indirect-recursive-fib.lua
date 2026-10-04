function fib(n, self)
    if n < 2 then
        return n
    end
    return self(n - 1, self) + self(n - 2, self)
end

function run()
    return fib(20, fib)
end
