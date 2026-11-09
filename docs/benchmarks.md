# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.095 µs · 0B · 0a | 5.452 µs · 0B · 0a | 1.67× | — | 2.231 µs · 1024B · 128a | 38.2 µs · 96288B · 388a | 16.76 µs · 14376B · 256a | 70.04 µs · 78016B · 770a | 13.7 µs · 5712B · 266a | — | 30.83 µs · 2308B · 276a |
| binary-trees | 1.963 ms · 768B · 8a | 603.7 µs · 768B · 8a | 3.25× | — | 281.2 µs · 201936B · 8414a | 3.814 ms · 1468185B · 62877a | 10.65 ms · 6995674B · 92703a | 8.873 ms · 5198705B · 69052a | 27.2 ms · 19457616B · 280714a | — | 28.28 ms · 15128173B · 446106a |
| branch-tree | 1.302 µs · 0B · 0a | 137.4 ns · 0B · 0a | 9.48× | 627.8 ns · 16B · 1a | 160.4 ns · 0B · 0a | 53.22 µs · 95384B · 660a | 15.18 µs · 2464B · 9a | 26.3 µs · 1992B · 196a | 23.7 µs · 2168B · 203a | — | 20.87 µs · 1640B · 219a |
| closure-counter | 5.665 µs · 64B · 2a | 930.9 ns · 64B · 2a | 6.09× | — | 39.81 ns · 0B · 0a | 41.65 µs · 92273B · 261a | 12.78 µs · 151B · 3a | 19.07 µs · 1264B · 13a | 77.06 µs · 58312B · 659a | — | 90.47 µs · 35629B · 922a |
| fannkuch | 827.9 µs · 34608B · 1442a | 266.9 µs · 34608B · 1442a | 3.1× | — | 34.81 µs · 17280B · 720a | 2.013 ms · 456579B · 25621a | 1.553 ms · 443136B · 1460a | 18.96 ms · 14005337B · 164946a | 3.291 ms · 1367677B · 16944a | — | 4.701 ms · 2501425B · 88439a |
| fnv1a64 | 120.9 µs · 16400B · 2049a | 1.148 µs · 16400B · 2049a | 105× | — | 902.6 ns · 0B · 0a | 160.5 µs · 120808B · 3838a | — | 1.419 ms · 608256B · 18475a | 380.2 µs · 162921B · 7429a | — | 154.4 µs · 17282B · 2074a |
| i64-wide-fib | 28.11 ms · 2913456B · 364178a | 681.9 µs · 16B · 1a | 41.2× | — | 239.2 µs · 0B · 0a | 22.96 µs · 90520B · 52a | — | 155.8 ms · 64193360B · 1942306a | 340.1 ms · 253472736B · 4248839a | — | 123.1 ms · 98686592B · 2138708a |
| indirect-recursive-fib | 1.184 ms · 0B · 0a | 60.49 µs · 0B · 0a | 19.6× | 98.97 µs · 8B · 1a | 34.85 µs · 0B · 0a | 2.063 ms · 319345B · 28655a | 1.921 ms · 703B · 2a | 2.532 ms · 4680B · 39a | 11.19 ms · 10158193B · 109494a | — | 23.99 ms · 13061312B · 394061a |
| iterative-fib | 1.103 µs · 0B · 0a | 69.83 ns · 0B · 0a | 15.8× | 85.35 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.01 µs · 90592B · 61a | 1.149 µs · 160B · 0a | 4.212 µs · 368B · 20a | 6.05 µs · 2448B · 88a | — | 6.186 µs · 944B · 81a |
| mandelbrot | 291.4 µs · 0B · 0a | 10.9 µs · 0B · 0a | 26.7× | — | 6.327 µs · 0B · 0a | 942 µs · 292345B · 25008a | 481.6 µs · 169408B · 661a | 8.099 ms · 5558650B · 104191a | 1.478 ms · 324978B · 23643a | — | 1.529 ms · 365397B · 22513a |
| mat-mul | 369.7 µs · 6216B · 6a | 8.773 µs · 6216B · 6a | 42.1× | — | 5.513 µs · 6144B · 3a | 1.36 ms · 431714B · 34871a | 669.4 µs · 143168B · 478a | 1.196 ms · 61304B · 537a | 1.445 ms · 90712B · 9350a | — | 919.9 µs · 36615B · 6036a |
| n-body | 611.8 µs · 504B · 14a | 18.75 µs · 504B · 14a | 32.6× | — | 7.354 µs · 0B · 0a | 1.496 ms · 386897B · 35892a | 1.21 ms · 258913B · 2002a | 3.215 ms · 658435B · 81545a | 2.39 ms · 382715B · 34975a | — | 1.754 ms · 445472B · 20669a |
| n-queens | 427.2 µs · 120B · 6a | 31.63 µs · 120B · 6a | 13.5× | — | 6.014 µs · 0B · 0a | 941 µs · 234865B · 17784a | 570.9 µs · 14423B · 63a | 1.093 ms · 7424B · 35a | 1.432 ms · 410346B · 5147a | — | 1.995 ms · 766001B · 33371a |
| permutation-flips | 128.1 µs · 7680B · 128a | 17.81 µs · 7680B · 128a | 7.2× | — | 1.887 ns · 0B · 0a | 552.5 µs · 292857B · 9856a | 235.4 µs · 78808B · 451a | 524.4 µs · 122504B · 765a | 491.7 µs · 111976B · 2368a | — | 1.001 µs · 528B · 13a |
| recursive-fib-20 | 712.4 µs · 0B · 0a | 47.72 µs · 0B · 0a | 14.9× | 42.76 µs · 8B · 1a | 21.44 µs · 0B · 0a | 1.985 ms · 319345B · 28655a | 2.156 ms · 704B · 2a | 2.961 ms · 4680B · 39a | 11.4 ms · 9807920B · 109494a | — | 10.48 ms · 8302669B · 192847a |
| recursive-fib-35 | 970.2 ms · 0B · 0a | 63.97 ms · 0B · 0a | 15.2× | 58.19 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.648 s · 312797376B · 39088167a | 2.951 s · 971008B · 3793a | 4.044 s · 375360B · 46373a | 15.23 s · 13378014864B · 149349951a | — | — |
| sieve | 19.93 µs · 1048B · 2a | 929.4 ns · 1048B · 2a | 21.4× | 1.025 µs · 8B · 1a | 351 ns · 0B · 0a | 142.3 µs · 122504B · 1611a | 50.71 µs · 18416B · 44a | 83.84 µs · 1872B · 25a | 76.79 µs · 5704B · 30a | — | 80.85 µs · 4368B · 795a |
| sort-stress | 506.3 µs · 5136B · 512a | 12.58 µs · 5136B · 512a | 40.2× | — | 5.796 µs · 1024B · 2a | 1.28 ms · 270945B · 19965a | 850.8 µs · 36568B · 365a | 1.785 ms · 49104B · 3621a | 1.795 ms · 23448B · 2034a | — | 925.4 µs · 17473B · 1708a |
| spectral-norm | 529.4 µs · 648B · 6a | 8.305 µs · 648B · 6a | 63.7× | — | 7.016 µs · 576B · 3a | 2.046 ms · 571371B · 54822a | 1.07 ms · 234784B · 925a | 2.423 ms · 275337B · 33729a | 4.612 ms · 2457135B · 52718a | — | 4.089 ms · 2448002B · 74233a |
| string-build | 720.7 µs · 85408B · 4107a | 611.7 µs · 85408B · 4107a | 1.18× | — | 357 µs · 855890B · 5001a | 2.346 ms · 2211274B · 36101a | 2.263 ms · 1078112B · 13859a | 3.091 ms · 1147385B · 26883a | 2.446 ms · 1326244B · 24469a | — | 3.095 ms · 2372788B · 41432a |
| string-compare | 1.092 ms · 21136B · 1284a | 267.1 µs · 21136B · 1284a | 4.09× | — | 64.62 µs · 9632B · 1585a | 1.681 ms · 296281B · 20528a | 1.134 ms · 59712B · 3431a | 2.318 ms · 50744B · 3848a | 1.825 ms · 209521B · 5520a | — | 2.362 ms · 503611B · 31846a |
| struct-tree-walk | 282.2 µs · 768B · 8a | 107.9 µs · 768B · 8a | 2.62× | — | 1.877 ns · 0B · 0a | 633.7 µs · 458379B · 5114a | 1.358 ms · 818512B · 11253a | 1.082 ms · 558961B · 6149a | 3.448 ms · 2570663B · 34797a | — | 1.192 µs · 600B · 17a |
| tail-ping-pong | 29.27 µs · 0B · 0a | 470.9 ns · 0B · 0a | 62.2× | — | 890.7 ns · 0B · 0a | 104.9 µs · 106112B · 2001a | 35.13 µs · 21856B · 85a | 113.3 µs · 13984B · 1726a | 115.7 µs · 14280B · 1731a | — | 556.4 µs · 348834B · 9016a |
| tail-sum | 29.27 µs · 0B · 0a | 666 ns · 0B · 0a | 43.9× | — | 301.6 ns · 0B · 0a | 106.5 µs · 106112B · 2001a | 35.08 µs · 21856B · 85a | 113.3 µs · 13984B · 1726a | 116.1 µs · 14280B · 1731a | — | 83.99 µs · 4608B · 1017a |
| typed-array-sum | 7.206 µs · 0B · 0a | 261.6 ns · 0B · 0a | 27.5× | 267.5 ns · 8B · 1a | 1.878 ns · 0B · 0a | 44.89 µs · 94208B · 513a | 8.236 µs · 4000B · 15a | 27.45 µs · 2080B · 238a | 17.36 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.1 µs · 18424B · 2046a | 1.394 µs · 18424B · 2046a | 80.4× | — | 360.1 ns · 0B · 0a | 101.1 µs · 107608B · 2188a | — | 957.8 µs · 447252B · 12735a | 418.4 µs · 199937B · 7772a | — | 101.5 µs · 13545B · 1317a |

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
