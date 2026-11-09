# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.282 µs · 0B · 0a | 5.378 µs · 0B · 0a | 1.73× | — | 2.185 µs · 1024B · 128a | 39.26 µs · 96288B · 388a | 17.46 µs · 14376B · 256a | 84.42 µs · 78016B · 770a | 15.16 µs · 5712B · 266a | — | 31.06 µs · 2308B · 276a |
| binary-trees | 1.976 ms · 768B · 8a | 597.2 µs · 768B · 8a | 3.31× | — | 296.7 µs · 201936B · 8414a | 3.899 ms · 1468183B · 62877a | 10.5 ms · 6995671B · 92703a | 8.838 ms · 5198696B · 69052a | 27.64 ms · 19457610B · 280714a | — | 28.9 ms · 15128169B · 446106a |
| branch-tree | 1.339 µs · 0B · 0a | 138.1 ns · 0B · 0a | 9.7× | 625.7 ns · 16B · 1a | 160.8 ns · 0B · 0a | 46.72 µs · 95384B · 660a | 15.07 µs · 2464B · 9a | 26.32 µs · 1992B · 196a | 24.02 µs · 2168B · 203a | — | 20.72 µs · 1640B · 219a |
| closure-counter | 5.486 µs · 64B · 2a | 900.8 ns · 64B · 2a | 6.09× | — | 39.8 ns · 0B · 0a | 42.04 µs · 92272B · 261a | 12.94 µs · 151B · 3a | 18.98 µs · 1264B · 13a | 78.26 µs · 58312B · 659a | — | 90.26 µs · 35628B · 922a |
| fannkuch | 827.3 µs · 34608B · 1442a | 277.4 µs · 34608B · 1442a | 2.98× | — | 35.25 µs · 17280B · 720a | 2.032 ms · 456578B · 25621a | 1.546 ms · 443135B · 1460a | 19.59 ms · 14005325B · 164946a | 3.391 ms · 1367676B · 16944a | — | 4.786 ms · 2501437B · 88439a |
| fnv1a64 | 119.6 µs · 16400B · 2049a | 1.155 µs · 16400B · 2049a | 104× | — | 902.4 ns · 0B · 0a | 161.4 µs · 120808B · 3838a | — | 1.475 ms · 608249B · 18475a | 397.6 µs · 162921B · 7429a | — | 153.4 µs · 17282B · 2074a |
| i64-wide-fib | 27.91 ms · 2913440B · 364178a | 681 µs · 16B · 1a | 41× | — | 239.3 µs · 0B · 0a | 23.51 µs · 90520B · 52a | — | 161.9 ms · 64193200B · 1942305a | 346.6 ms · 253472736B · 4248841a | — | 129.4 ms · 98686581B · 2138708a |
| indirect-recursive-fib | 1.188 ms · 0B · 0a | 60.46 µs · 0B · 0a | 19.6× | 99.02 µs · 8B · 1a | 35.07 µs · 0B · 0a | 2.092 ms · 319345B · 28655a | 1.918 ms · 703B · 2a | 2.513 ms · 4680B · 39a | 11.47 ms · 10158192B · 109494a | — | 24.12 ms · 13061290B · 394061a |
| iterative-fib | 1.103 µs · 0B · 0a | 70.76 ns · 0B · 0a | 15.6× | 85.09 ns · 8B · 1a | 11.91 ns · 0B · 0a | 23.79 µs · 90592B · 61a | 1.152 µs · 160B · 0a | 4.218 µs · 368B · 20a | 6.157 µs · 2448B · 88a | — | 6.131 µs · 944B · 81a |
| mandelbrot | 291.5 µs · 0B · 0a | 10.92 µs · 0B · 0a | 26.7× | — | 6.346 µs · 0B · 0a | 945.5 µs · 292345B · 25008a | 477.7 µs · 169408B · 661a | 8.177 ms · 5558657B · 104191a | 1.474 ms · 324977B · 23643a | — | 1.527 ms · 365400B · 22513a |
| mat-mul | 370.8 µs · 6216B · 6a | 8.611 µs · 6216B · 6a | 43.1× | — | 5.456 µs · 6144B · 3a | 1.397 ms · 431714B · 34871a | 659.8 µs · 143168B · 478a | 1.2 ms · 61304B · 537a | 1.442 ms · 90712B · 9350a | — | 918.9 µs · 36613B · 6036a |
| n-body | 606.5 µs · 504B · 14a | 18.69 µs · 504B · 14a | 32.4× | — | 7.345 µs · 0B · 0a | 1.495 ms · 386898B · 35892a | 1.219 ms · 258914B · 2002a | 3.251 ms · 658436B · 81545a | 2.384 ms · 382715B · 34975a | — | 1.743 ms · 445473B · 20669a |
| n-queens | 431.4 µs · 120B · 6a | 31.39 µs · 120B · 6a | 13.7× | — | 6.012 µs · 0B · 0a | 951.9 µs · 234865B · 17784a | 566.4 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.397 ms · 410346B · 5147a | — | 2.04 ms · 766003B · 33371a |
| permutation-flips | 128.1 µs · 7680B · 128a | 17.65 µs · 7680B · 128a | 7.26× | — | 1.875 ns · 0B · 0a | 556.1 µs · 292857B · 9856a | 233 µs · 78808B · 451a | 526.6 µs · 122504B · 765a | 487.3 µs · 111976B · 2368a | — | 1.006 µs · 528B · 13a |
| recursive-fib-20 | 710.6 µs · 0B · 0a | 47.24 µs · 0B · 0a | 15× | 42.78 µs · 8B · 1a | 21.54 µs · 0B · 0a | 1.969 ms · 319345B · 28655a | 2.164 ms · 704B · 2a | 2.976 ms · 4680B · 39a | 11.37 ms · 9807916B · 109494a | — | 10.52 ms · 8302712B · 192848a |
| recursive-fib-35 | 968.5 ms · 0B · 0a | 63.96 ms · 0B · 0a | 15.1× | 58.22 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.647 s · 312797312B · 39088168a | 2.953 s · 971008B · 3793a | 4.045 s · 375360B · 46373a | 15.65 s · 13378016096B · 149349944a | — | — |
| sieve | 19.88 µs · 1048B · 2a | 938.2 ns · 1048B · 2a | 21.2× | 1.036 µs · 8B · 1a | 351 ns · 0B · 0a | 138.5 µs · 122504B · 1611a | 50.3 µs · 18416B · 44a | 83.85 µs · 1872B · 25a | 76.28 µs · 5704B · 30a | — | 81.76 µs · 4368B · 795a |
| sort-stress | 506.7 µs · 5136B · 512a | 12.57 µs · 5136B · 512a | 40.3× | — | 5.809 µs · 1024B · 2a | 1.26 ms · 270945B · 19965a | 849.4 µs · 36568B · 365a | 1.792 ms · 49104B · 3621a | 1.783 ms · 23448B · 2034a | — | 927.3 µs · 17474B · 1708a |
| spectral-norm | 535.1 µs · 648B · 6a | 8.32 µs · 648B · 6a | 64.3× | — | 7.023 µs · 576B · 3a | 2.025 ms · 571370B · 54822a | 1.073 ms · 234785B · 925a | 2.421 ms · 275338B · 33729a | 4.656 ms · 2457136B · 52718a | — | 4.099 ms · 2448009B · 74233a |
| string-build | 722.9 µs · 85408B · 4107a | 579.1 µs · 85408B · 4107a | 1.25× | — | 370.4 µs · 855892B · 5001a | 2.388 ms · 2211277B · 36101a | 2.302 ms · 1078112B · 13859a | 3.111 ms · 1147384B · 26883a | 2.44 ms · 1326243B · 24469a | — | 3.075 ms · 2372792B · 41432a |
| string-compare | 1.1 ms · 21136B · 1284a | 274.1 µs · 21136B · 1284a | 4.01× | — | 62.21 µs · 9632B · 1585a | 1.666 ms · 296281B · 20528a | 1.129 ms · 59712B · 3431a | 2.314 ms · 50744B · 3848a | 1.823 ms · 209522B · 5520a | — | 2.352 ms · 503614B · 31846a |
| struct-tree-walk | 283.9 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.63× | — | 1.875 ns · 0B · 0a | 654.8 µs · 458379B · 5114a | 1.347 ms · 818514B · 11253a | 1.098 ms · 558961B · 6149a | 3.48 ms · 2570664B · 34797a | — | 1.17 µs · 600B · 17a |
| tail-ping-pong | 29.67 µs · 0B · 0a | 467.5 ns · 0B · 0a | 63.5× | — | 890.9 ns · 0B · 0a | 106.5 µs · 106112B · 2001a | 35 µs · 21855B · 85a | 111.2 µs · 13984B · 1726a | 114.5 µs · 14280B · 1731a | — | 539.2 µs · 348835B · 9016a |
| tail-sum | 29.6 µs · 0B · 0a | 662.2 ns · 0B · 0a | 44.7× | — | 302.7 ns · 0B · 0a | 107 µs · 106112B · 2001a | 35.34 µs · 21856B · 85a | 112.7 µs · 13984B · 1726a | 115.6 µs · 14280B · 1731a | — | 84.67 µs · 4608B · 1017a |
| typed-array-sum | 7.22 µs · 0B · 0a | 252.6 ns · 0B · 0a | 28.6× | 266.5 ns · 8B · 1a | 1.888 ns · 0B · 0a | 45.16 µs · 94208B · 513a | 8.38 µs · 4000B · 15a | 27.45 µs · 2080B · 238a | 17.14 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.1 µs · 18424B · 2046a | 1.487 µs · 18424B · 2046a | 74.1× | — | 360.2 ns · 0B · 0a | 105.9 µs · 107608B · 2188a | — | 977.1 µs · 447252B · 12735a | 423.3 µs · 199937B · 7772a | — | 101.4 µs · 13546B · 1317a |

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
