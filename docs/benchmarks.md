# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.191 µs · 0B · 0a | 5.352 µs · 0B · 0a | 1.72× | — | 2.345 µs · 1024B · 128a | 37.69 µs · 96288B · 388a | 18.06 µs · 14376B · 256a | 72.6 µs · 78016B · 770a | 14.82 µs · 5712B · 266a | — | 30.84 µs · 2308B · 276a |
| binary-trees | 1.978 ms · 768B · 8a | 594 µs · 768B · 8a | 3.33× | — | 313.2 µs · 201942B · 8414a | 3.986 ms · 1468185B · 62877a | 10.57 ms · 6995687B · 92703a | 9.013 ms · 5198696B · 69052a | 28.12 ms · 19457601B · 280714a | — | 30.03 ms · 15128195B · 446106a |
| branch-tree | 1.313 µs · 0B · 0a | 159 ns · 0B · 0a | 8.26× | 626.4 ns · 16B · 1a | 160.5 ns · 0B · 0a | 46.28 µs · 95384B · 660a | 15.24 µs · 2464B · 9a | 26.64 µs · 1992B · 196a | 24.14 µs · 2168B · 203a | — | 20.66 µs · 1640B · 219a |
| closure-counter | 5.505 µs · 64B · 2a | 914.6 ns · 64B · 2a | 6.02× | — | 40.41 ns · 0B · 0a | 41.7 µs · 92272B · 261a | 13.13 µs · 151B · 3a | 19.17 µs · 1264B · 13a | 82.35 µs · 58312B · 659a | — | 92.99 µs · 35629B · 922a |
| coroutine-gen | 77.87 µs · 128B · 2a | 77.64 µs · 128B · 2a | 1× | — | 42.49 µs · 208B · 7a | — | 329.6 µs · 149936B · 649a | 596.9 µs · 330649B · 6719a | 201.9 µs · 91800B · 3646a | — | — |
| fannkuch | 828.1 µs · 34608B · 1442a | 234.6 µs · 34608B · 1442a | 3.53× | — | 35.5 µs · 17280B · 720a | 2.074 ms · 456578B · 25621a | 1.531 ms · 443135B · 1460a | 19.37 ms · 14005334B · 164946a | 3.37 ms · 1367677B · 16944a | — | 4.968 ms · 2501444B · 88439a |
| fnv1a64 | 126.7 µs · 16400B · 2049a | 1.159 µs · 16400B · 2049a | 109× | — | 902.9 ns · 0B · 0a | 164.8 µs · 120808B · 3838a | — | 1.508 ms · 608255B · 18475a | 405.2 µs · 162921B · 7429a | — | 155.8 µs · 17282B · 2074a |
| host-call | 744 µs · 196592B · 14073a | 748 µs · 196592B · 14073a | 0.995× | — | 3.313 µs · 0B · 0a | 183.4 µs · 139264B · 3073a | 132.8 µs · 22536B · 1079a | 814.7 µs · 158048B · 14585a | 108.4 µs · 14920B · 1799a | — | 589.7 µs · 340636B · 9231a |
| i64-wide-fib | 28.82 ms · 2913456B · 364178a | 681.6 µs · 16B · 1a | 42.3× | — | 239.6 µs · 0B · 0a | 23.2 µs · 90520B · 52a | — | 160.2 ms · 64193328B · 1942305a | 353.1 ms · 253472864B · 4248842a | — | 133.7 ms · 98686634B · 2138708a |
| indirect-recursive-fib | 1.184 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.6× | 99.08 µs · 8B · 1a | 34.92 µs · 0B · 0a | 2.116 ms · 319345B · 28655a | 1.951 ms · 703B · 2a | 2.547 ms · 4680B · 39a | 11.8 ms · 10158185B · 109494a | — | 25.24 ms · 13061318B · 394061a |
| iterative-fib | 1.119 µs · 0B · 0a | 84.43 ns · 0B · 0a | 13.3× | 85.73 ns · 8B · 1a | 12.45 ns · 0B · 0a | 24.54 µs · 90592B · 61a | 1.155 µs · 160B · 0a | 4.24 µs · 368B · 20a | 6.371 µs · 2448B · 88a | — | 6.199 µs · 944B · 81a |
| mandelbrot | 290.6 µs · 0B · 0a | 10.93 µs · 0B · 0a | 26.6× | — | 6.324 µs · 0B · 0a | 972.4 µs · 292346B · 25008a | 486.8 µs · 169408B · 661a | 8.24 ms · 5558664B · 104191a | 1.528 ms · 324978B · 23643a | — | 1.561 ms · 365402B · 22513a |
| map-ops | 74.29 µs · 9584B · 5a | 74.47 µs · 9584B · 5a | 0.998× | — | 10.82 µs · 4904B · 3a | — | 278 µs · 114328B · 198a | 388.9 µs · 66728B · 4078a | — | — | 1.185 ms · 501515B · 15813a |
| mat-mul | 372.9 µs · 6216B · 6a | 8.606 µs · 6216B · 6a | 43.3× | — | 5.491 µs · 6144B · 3a | 1.391 ms · 431715B · 34871a | 673.5 µs · 143168B · 478a | 1.195 ms · 61305B · 537a | 1.46 ms · 90712B · 9350a | — | 921 µs · 36612B · 6036a |
| n-body | 603.5 µs · 504B · 14a | 18.96 µs · 504B · 14a | 31.8× | — | 7.358 µs · 0B · 0a | 1.526 ms · 386897B · 35892a | 1.228 ms · 258913B · 2002a | 3.378 ms · 658435B · 81545a | 2.459 ms · 382714B · 34975a | — | 1.758 ms · 445464B · 20669a |
| n-queens | 431.6 µs · 120B · 6a | 31.53 µs · 120B · 6a | 13.7× | — | 6.008 µs · 0B · 0a | 968.4 µs · 234865B · 17784a | 568.5 µs · 14423B · 63a | 1.091 ms · 7424B · 35a | 1.45 ms · 410345B · 5147a | — | 2.03 ms · 766011B · 33371a |
| permutation-flips | 128.3 µs · 7680B · 128a | 17.42 µs · 7680B · 128a | 7.37× | — | 1.874 ns · 0B · 0a | 571.9 µs · 292857B · 9856a | 237.5 µs · 78808B · 451a | 528.4 µs · 122504B · 765a | 501.4 µs · 111976B · 2368a | — | 1.054 µs · 528B · 13a |
| recursive-fib-20 | 712.4 µs · 0B · 0a | 47.09 µs · 0B · 0a | 15.1× | 42.72 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.987 ms · 319346B · 28655a | 2.215 ms · 704B · 2a | 2.961 ms · 4680B · 39a | 11.59 ms · 9807923B · 109494a | — | 11.16 ms · 8302703B · 192848a |
| recursive-fib-35 | 968.4 ms · 0B · 0a | 63.97 ms · 0B · 0a | 15.1× | 58.21 ms · 8B · 1a | 29.2 ms · 0B · 0a | 2.652 s · 312797344B · 39088167a | 3.022 s · 971008B · 3793a | 4.032 s · 375360B · 46373a | 15.95 s · 13378016912B · 149349963a | — | — |
| sieve | 20.03 µs · 1048B · 2a | 934.6 ns · 1048B · 2a | 21.4× | 1.037 µs · 8B · 1a | 371.8 ns · 0B · 0a | 138.7 µs · 122504B · 1611a | 50.74 µs · 18416B · 44a | 84.14 µs · 1872B · 25a | 77.14 µs · 5704B · 30a | — | 80.6 µs · 4368B · 795a |
| sort-stress | 505.3 µs · 5136B · 512a | 12.68 µs · 5136B · 512a | 39.9× | — | 5.347 µs · 1024B · 2a | 1.341 ms · 270946B · 19965a | 844.9 µs · 36568B · 365a | 1.799 ms · 49104B · 3621a | 1.786 ms · 23448B · 2034a | — | 932.2 µs · 17473B · 1708a |
| spectral-norm | 533.8 µs · 648B · 6a | 8.392 µs · 648B · 6a | 63.6× | — | 7.049 µs · 576B · 3a | 2.141 ms · 571371B · 54822a | 1.087 ms · 234784B · 925a | 2.457 ms · 275337B · 33729a | 4.744 ms · 2457135B · 52718a | — | 4.309 ms · 2448025B · 74233a |
| string-build | 738.3 µs · 85408B · 4107a | 612.2 µs · 85408B · 4107a | 1.21× | — | 365.3 µs · 855889B · 5001a | 2.414 ms · 2211276B · 36101a | 2.307 ms · 1078111B · 13859a | 3.124 ms · 1147384B · 26883a | 2.489 ms · 1326244B · 24469a | — | 3.132 ms · 2372788B · 41432a |
| string-compare | 1.089 ms · 21136B · 1284a | 238 µs · 21136B · 1284a | 4.58× | — | 65.45 µs · 9632B · 1585a | 1.702 ms · 296282B · 20528a | 1.133 ms · 59712B · 3431a | 2.322 ms · 50744B · 3848a | 1.865 ms · 209521B · 5520a | — | 2.421 ms · 503608B · 31846a |
| struct-tree-walk | 284.5 µs · 768B · 8a | 107.1 µs · 768B · 8a | 2.66× | — | 1.88 ns · 0B · 0a | 653.4 µs · 458379B · 5114a | 1.379 ms · 818512B · 11253a | 1.094 ms · 558960B · 6149a | 3.552 ms · 2570663B · 34797a | — | 1.236 µs · 600B · 17a |
| tail-ping-pong | 28.91 µs · 0B · 0a | 484.5 ns · 0B · 0a | 59.7× | — | 890.9 ns · 0B · 0a | 107.8 µs · 106112B · 2001a | 36.24 µs · 21856B · 85a | 113.6 µs · 13984B · 1726a | 120.1 µs · 14280B · 1731a | — | 574.2 µs · 348833B · 9016a |
| tail-sum | 28.85 µs · 0B · 0a | 684.3 ns · 0B · 0a | 42.2× | — | 302.6 ns · 0B · 0a | 109.5 µs · 106112B · 2001a | 36.23 µs · 21856B · 85a | 115 µs · 13984B · 1726a | 120.3 µs · 14280B · 1731a | — | 83.88 µs · 4608B · 1017a |
| typed-array-sum | 7.263 µs · 0B · 0a | 265.8 ns · 0B · 0a | 27.3× | 267 ns · 8B · 1a | 1.875 ns · 0B · 0a | 44.55 µs · 94208B · 513a | 8.337 µs · 4000B · 15a | 27.73 µs · 2080B · 238a | 17.72 µs · 2496B · 246a | — | — |
| xorshift-i64 | 116.2 µs · 18424B · 2046a | 1.391 µs · 18424B · 2046a | 83.6× | — | 356.7 ns · 0B · 0a | 104.1 µs · 107608B · 2188a | — | 1.046 ms · 447255B · 12736a | 430.8 µs · 199937B · 7772a | — | 102.3 µs · 13545B · 1317a |

<!-- benchmark-table:end -->

## Method

The timed loop excludes fixture loading, compilation, module construction, function lookup, and correctness checks where the runtime permits it. `quick`, `standard`, and `deep` use 100 ms, 300 ms, and 1 s target times; `—` denotes an unavailable or intentionally inapplicable runtime.

CI publishes this table from a shared `ubuntu-24.04-arm` runner: `standard` on each code push to `main`, `deep` weekly. Read it as ratios between runtimes in one run. Differences between two runs include runner noise, and numbers from another machine are not comparable. Use `make benchmark-diff` to judge a change.

## Ownership

- `benchmarks/registry` owns benchmark metadata and runtime source attachment.
- `benchmarks/fixtures` owns benchmark source and native implementations.
- `benchmarks/cmd/benchreport` owns this generated table.

## Related

- `../benchmarks/README.md` — fixture, registry, runtime, and measurement contract
- `writing.md` — Markdown document rules
- `testing.md` — test structure and validation
