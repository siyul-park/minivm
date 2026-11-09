# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.768 µs · 0B · 0a | 5.326 µs · 0B · 0a | 1.65× | — | 2.174 µs · 1024B · 128a | 38.61 µs · 96288B · 388a | 18.09 µs · 14376B · 256a | 73.12 µs · 78016B · 770a | 13.8 µs · 5712B · 266a | — | 30.8 µs · 2308B · 276a |
| binary-trees | 1.96 ms · 768B · 8a | 603.9 µs · 768B · 8a | 3.25× | — | 286.5 µs · 201936B · 8414a | 3.797 ms · 1468186B · 62877a | 10.57 ms · 6995694B · 92703a | 8.837 ms · 5198696B · 69052a | 27.55 ms · 19457590B · 280714a | — | 28.57 ms · 15128165B · 446106a |
| branch-tree | 1.296 µs · 0B · 0a | 145 ns · 0B · 0a | 8.94× | 627 ns · 16B · 1a | 160.4 ns · 0B · 0a | 61.36 µs · 95384B · 660a | 15.95 µs · 2464B · 9a | 26.65 µs · 1992B · 196a | 24.24 µs · 2168B · 203a | — | 20.49 µs · 1640B · 219a |
| closure-counter | 5.489 µs · 64B · 2a | 921.9 ns · 64B · 2a | 5.95× | — | 39.78 ns · 0B · 0a | 45.7 µs · 92272B · 261a | 12.68 µs · 151B · 3a | 19.16 µs · 1264B · 13a | 78.65 µs · 58312B · 659a | — | 87.72 µs · 35628B · 922a |
| fannkuch | 824.5 µs · 34608B · 1442a | 264.8 µs · 34608B · 1442a | 3.11× | — | 34.85 µs · 17280B · 720a | 2.014 ms · 456578B · 25621a | 1.548 ms · 443136B · 1460a | 19.1 ms · 14005351B · 164946a | 3.346 ms · 1367679B · 16944a | — | 4.693 ms · 2501435B · 88439a |
| fnv1a64 | 120.4 µs · 16400B · 2049a | 1.239 µs · 16400B · 2049a | 97.2× | — | 903 ns · 0B · 0a | 164.5 µs · 120808B · 3838a | — | 1.416 ms · 608255B · 18475a | 389 µs · 162921B · 7429a | — | 153.1 µs · 17282B · 2074a |
| i64-wide-fib | 28.14 ms · 2913440B · 364178a | 776.7 µs · 16B · 1a | 36.2× | — | 239.1 µs · 0B · 0a | 24.35 µs · 90520B · 52a | — | 154.4 ms · 64193216B · 1942305a | 350.9 ms · 253472768B · 4248841a | — | 122.2 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 1.187 ms · 0B · 0a | 66.23 µs · 0B · 0a | 17.9× | 98.76 µs · 8B · 1a | 35.12 µs · 0B · 0a | 2.087 ms · 319345B · 28655a | 2.022 ms · 704B · 2a | 2.538 ms · 4680B · 39a | 11.82 ms · 10158189B · 109494a | — | 24.04 ms · 13061284B · 394061a |
| iterative-fib | 1.103 µs · 0B · 0a | 74.97 ns · 0B · 0a | 14.7× | 85.09 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.75 µs · 90592B · 61a | 1.155 µs · 160B · 0a | 4.191 µs · 368B · 20a | 5.987 µs · 2448B · 88a | — | 6.137 µs · 944B · 81a |
| mandelbrot | 290.6 µs · 0B · 0a | 11.07 µs · 0B · 0a | 26.3× | — | 6.349 µs · 0B · 0a | 919.9 µs · 292345B · 25008a | 486.1 µs · 169408B · 661a | 8.094 ms · 5558655B · 104191a | 1.449 ms · 324978B · 23643a | — | 1.514 ms · 365400B · 22513a |
| mat-mul | 367.1 µs · 6216B · 6a | 8.602 µs · 6216B · 6a | 42.7× | — | 5.466 µs · 6144B · 3a | 1.338 ms · 431715B · 34871a | 673.7 µs · 143168B · 478a | 1.201 ms · 61304B · 537a | 1.459 ms · 90713B · 9350a | — | 916 µs · 36613B · 6036a |
| n-body | 605.1 µs · 504B · 14a | 18.66 µs · 504B · 14a | 32.4× | — | 7.356 µs · 0B · 0a | 1.463 ms · 386899B · 35892a | 1.215 ms · 258913B · 2002a | 3.195 ms · 658435B · 81545a | 2.367 ms · 382715B · 34975a | — | 1.746 ms · 445465B · 20669a |
| n-queens | 438.3 µs · 120B · 6a | 32.06 µs · 120B · 6a | 13.7× | — | 6.007 µs · 0B · 0a | 941.2 µs · 234865B · 17784a | 571.9 µs · 14423B · 63a | 1.095 ms · 7424B · 35a | 1.421 ms · 410345B · 5147a | — | 1.99 ms · 766007B · 33371a |
| permutation-flips | 127.8 µs · 7680B · 128a | 17.15 µs · 7680B · 128a | 7.45× | — | 1.876 ns · 0B · 0a | 554.5 µs · 292858B · 9856a | 232.9 µs · 78808B · 451a | 518.1 µs · 122504B · 765a | 481.3 µs · 111976B · 2368a | — | 968.5 ns · 528B · 13a |
| recursive-fib-20 | 710.3 µs · 0B · 0a | 53.33 µs · 0B · 0a | 13.3× | 42.74 µs · 8B · 1a | 21.43 µs · 0B · 0a | 1.992 ms · 319346B · 28655a | 2.158 ms · 704B · 2a | 2.965 ms · 4680B · 39a | 11.64 ms · 9807907B · 109494a | — | 10.5 ms · 8302706B · 192848a |
| recursive-fib-35 | 972.5 ms · 0B · 0a | 72.47 ms · 0B · 0a | 13.4× | 58.21 ms · 8B · 1a | 29.06 ms · 0B · 0a | 2.644 s · 312797216B · 39088167a | 2.941 s · 971008B · 3793a | 4.03 s · 375360B · 46373a | 15.61 s · 13378015840B · 149349947a | — | — |
| sieve | 19.79 µs · 1048B · 2a | 947.3 ns · 1048B · 2a | 20.9× | 1.019 µs · 8B · 1a | 351 ns · 0B · 0a | 139.3 µs · 122504B · 1611a | 50.87 µs · 18416B · 44a | 84.07 µs · 1872B · 25a | 77.08 µs · 5704B · 30a | — | 80.85 µs · 4368B · 795a |
| sort-stress | 505 µs · 5136B · 512a | 12.51 µs · 5136B · 512a | 40.4× | — | 5.814 µs · 1024B · 2a | 1.252 ms · 270945B · 19965a | 851.9 µs · 36568B · 365a | 1.789 ms · 49104B · 3621a | 1.787 ms · 23448B · 2034a | — | 925.7 µs · 17474B · 1708a |
| spectral-norm | 529.6 µs · 648B · 6a | 33.44 µs · 648B · 6a | 15.8× | — | 7.033 µs · 576B · 3a | 1.983 ms · 571370B · 54822a | 1.099 ms · 234784B · 925a | 2.42 ms · 275337B · 33729a | 4.499 ms · 2457135B · 52718a | — | 4.038 ms · 2448009B · 74233a |
| string-build | 713.8 µs · 85408B · 4107a | 614.5 µs · 85408B · 4107a | 1.16× | — | 378.5 µs · 855891B · 5001a | 2.45 ms · 2211272B · 36101a | 2.327 ms · 1078112B · 13859a | 3.105 ms · 1147384B · 26883a | 2.463 ms · 1326241B · 24469a | — | 3.09 ms · 2372772B · 41432a |
| string-compare | 1.08 ms · 21136B · 1284a | 265.9 µs · 21136B · 1284a | 4.06× | — | 62.1 µs · 9632B · 1585a | 1.664 ms · 296281B · 20528a | 1.136 ms · 59712B · 3431a | 2.323 ms · 50744B · 3848a | 1.8 ms · 209520B · 5520a | — | 2.36 ms · 503612B · 31846a |
| struct-tree-walk | 281.6 µs · 768B · 8a | 109.3 µs · 768B · 8a | 2.58× | — | 1.878 ns · 0B · 0a | 644.7 µs · 458379B · 5114a | 1.371 ms · 818512B · 11253a | 1.087 ms · 558961B · 6149a | 3.472 ms · 2570663B · 34797a | — | 1.141 µs · 600B · 17a |
| tail-ping-pong | 29.07 µs · 0B · 0a | 510.3 ns · 0B · 0a | 57× | — | 890.4 ns · 0B · 0a | 107.2 µs · 106112B · 2001a | 35.65 µs · 21856B · 85a | 110.5 µs · 13984B · 1726a | 115.7 µs · 14280B · 1731a | — | 528.5 µs · 348831B · 9016a |
| tail-sum | 29.25 µs · 0B · 0a | 664.9 ns · 0B · 0a | 44× | — | 302.2 ns · 0B · 0a | 107.2 µs · 106112B · 2001a | 36.1 µs · 21856B · 85a | 110.5 µs · 13984B · 1726a | 115.5 µs · 14280B · 1731a | — | 84.09 µs · 4608B · 1017a |
| typed-array-sum | 7.177 µs · 0B · 0a | 235.8 ns · 0B · 0a | 30.4× | 267.5 ns · 8B · 1a | 1.888 ns · 0B · 0a | 45.53 µs · 94208B · 513a | 8.333 µs · 4000B · 15a | 27.11 µs · 2080B · 238a | 17.09 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.2 µs · 18424B · 2046a | 1.444 µs · 18424B · 2046a | 76.3× | — | 360.1 ns · 0B · 0a | 103.4 µs · 107608B · 2188a | — | 921.9 µs · 447254B · 12736a | 402.4 µs · 199937B · 7772a | — | 101 µs · 13545B · 1317a |

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
