# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.765 µs · 0B · 0a | 5.339 µs · 0B · 0a | 1.64× | — | 2.234 µs · 1024B · 128a | 37.63 µs · 96288B · 388a | 17.05 µs · 14376B · 256a | 71.28 µs · 78016B · 770a | 14.28 µs · 5712B · 266a | — | 30.95 µs · 2308B · 276a |
| binary-trees | 1.958 ms · 768B · 8a | 598.7 µs · 768B · 8a | 3.27× | — | 293.9 µs · 201936B · 8414a | 3.881 ms · 1468185B · 62877a | 10.52 ms · 6995671B · 92703a | 8.872 ms · 5198703B · 69052a | 27.88 ms · 19457630B · 280714a | — | 29.08 ms · 15128185B · 446106a |
| branch-tree | 1.3 µs · 0B · 0a | 139.8 ns · 0B · 0a | 9.3× | 626.9 ns · 16B · 1a | 160.4 ns · 0B · 0a | 46.83 µs · 95384B · 660a | 15.18 µs · 2464B · 9a | 26.44 µs · 1992B · 196a | 24.65 µs · 2168B · 203a | — | 20.73 µs · 1640B · 219a |
| closure-counter | 5.472 µs · 64B · 2a | 892.1 ns · 64B · 2a | 6.13× | — | 39.81 ns · 0B · 0a | 41.3 µs · 92272B · 261a | 12.46 µs · 151B · 3a | 19.26 µs · 1264B · 13a | 79.84 µs · 58312B · 659a | — | 88.5 µs · 35628B · 922a |
| fannkuch | 824.8 µs · 34608B · 1442a | 270.4 µs · 34608B · 1442a | 3.05× | — | 34.92 µs · 17280B · 720a | 2.014 ms · 456578B · 25621a | 1.541 ms · 443136B · 1460a | 19.51 ms · 14005336B · 164946a | 3.37 ms · 1367676B · 16944a | — | 4.846 ms · 2501445B · 88439a |
| fnv1a64 | 122.8 µs · 16400B · 2049a | 1.205 µs · 16400B · 2049a | 102× | — | 902.3 ns · 0B · 0a | 161.5 µs · 120808B · 3838a | — | 1.503 ms · 608260B · 18475a | 409.6 µs · 162921B · 7429a | — | 153.5 µs · 17282B · 2074a |
| i64-wide-fib | 28.87 ms · 2913456B · 364178a | 732.3 µs · 16B · 1a | 39.4× | — | 238.6 µs · 0B · 0a | 23.17 µs · 90521B · 52a | — | 158.4 ms · 64193448B · 1942307a | 340.8 ms · 253473152B · 4248843a | — | 128.8 ms · 98686592B · 2138708a |
| indirect-recursive-fib | 1.188 ms · 0B · 0a | 63.89 µs · 0B · 0a | 18.6× | 98.86 µs · 8B · 1a | 34.83 µs · 0B · 0a | 2.079 ms · 319345B · 28655a | 1.918 ms · 703B · 2a | 2.563 ms · 4680B · 39a | 11.19 ms · 10158183B · 109494a | — | 24.2 ms · 13061289B · 394061a |
| iterative-fib | 1.103 µs · 0B · 0a | 73.72 ns · 0B · 0a | 15× | 85.03 ns · 8B · 1a | 11.89 ns · 0B · 0a | 24.77 µs · 90592B · 61a | 1.162 µs · 160B · 0a | 4.244 µs · 368B · 20a | 6.149 µs · 2448B · 88a | — | 6.176 µs · 944B · 81a |
| mandelbrot | 291.2 µs · 0B · 0a | 11.08 µs · 0B · 0a | 26.3× | — | 6.324 µs · 0B · 0a | 967.4 µs · 292346B · 25008a | 483.6 µs · 169408B · 661a | 7.816 ms · 5558653B · 104191a | 1.475 ms · 324979B · 23643a | — | 1.53 ms · 365406B · 22513a |
| mat-mul | 367.3 µs · 6216B · 6a | 8.651 µs · 6216B · 6a | 42.5× | — | 5.468 µs · 6144B · 3a | 1.376 ms · 431716B · 34871a | 672 µs · 143168B · 478a | 1.209 ms · 61304B · 537a | 1.462 ms · 90713B · 9350a | — | 922.1 µs · 36615B · 6036a |
| n-body | 607 µs · 504B · 14a | 18.73 µs · 504B · 14a | 32.4× | — | 7.345 µs · 0B · 0a | 1.475 ms · 386898B · 35892a | 1.211 ms · 258913B · 2002a | 3.249 ms · 658436B · 81545a | 2.408 ms · 382714B · 34975a | — | 1.744 ms · 445464B · 20669a |
| n-queens | 428.4 µs · 120B · 6a | 31.88 µs · 120B · 6a | 13.4× | — | 6.032 µs · 0B · 0a | 949.2 µs · 234865B · 17784a | 572 µs · 14423B · 63a | 1.094 ms · 7424B · 35a | 1.417 ms · 410346B · 5147a | — | 1.997 ms · 766001B · 33371a |
| permutation-flips | 127.6 µs · 7680B · 128a | 17.59 µs · 7680B · 128a | 7.26× | — | 1.889 ns · 0B · 0a | 557.7 µs · 292857B · 9856a | 233.9 µs · 78808B · 451a | 525.1 µs · 122504B · 765a | 485 µs · 111976B · 2368a | — | 994.7 ns · 528B · 13a |
| recursive-fib-20 | 712.2 µs · 0B · 0a | 49.72 µs · 0B · 0a | 14.3× | 42.73 µs · 8B · 1a | 21.44 µs · 0B · 0a | 1.959 ms · 319345B · 28655a | 2.157 ms · 704B · 2a | 2.974 ms · 4680B · 39a | 11.17 ms · 9807925B · 109494a | — | 10.53 ms · 8302678B · 192847a |
| recursive-fib-35 | 971 ms · 0B · 0a | 67.77 ms · 0B · 0a | 14.3× | 58.21 ms · 8B · 1a | 29.09 ms · 0B · 0a | 2.629 s · 312797344B · 39088167a | 2.94 s · 971008B · 3793a | 4.047 s · 375360B · 46373a | 15.41 s · 13378015120B · 149349953a | — | — |
| sieve | 19.82 µs · 1048B · 2a | 972.2 ns · 1048B · 2a | 20.4× | 1.035 µs · 8B · 1a | 350.8 ns · 0B · 0a | 136.7 µs · 122504B · 1611a | 50.59 µs · 18416B · 44a | 84.52 µs · 1872B · 25a | 76.39 µs · 5704B · 30a | — | 81.07 µs · 4368B · 795a |
| sort-stress | 502.9 µs · 5136B · 512a | 12.47 µs · 5136B · 512a | 40.3× | — | 5.792 µs · 1024B · 2a | 1.305 ms · 270945B · 19965a | 850.6 µs · 36568B · 365a | 1.788 ms · 49104B · 3621a | 1.785 ms · 23448B · 2034a | — | 932.3 µs · 17476B · 1708a |
| spectral-norm | 533.5 µs · 648B · 6a | 32.66 µs · 648B · 6a | 16.3× | — | 7.051 µs · 576B · 3a | 2.079 ms · 571371B · 54822a | 1.094 ms · 234784B · 925a | 2.457 ms · 275337B · 33729a | 4.718 ms · 2457133B · 52718a | — | 4.307 ms · 2448012B · 74233a |
| string-build | 730.7 µs · 85408B · 4107a | 619.9 µs · 85408B · 4107a | 1.18× | — | 361.5 µs · 855890B · 5001a | 2.389 ms · 2211274B · 36101a | 2.372 ms · 1078109B · 13859a | 3.101 ms · 1147385B · 26883a | 2.503 ms · 1326244B · 24469a | — | 3.125 ms · 2372802B · 41432a |
| string-compare | 1.076 ms · 21136B · 1284a | 267.9 µs · 21136B · 1284a | 4.01× | — | 63.47 µs · 9632B · 1585a | 1.683 ms · 296282B · 20528a | 1.131 ms · 59712B · 3431a | 2.335 ms · 50744B · 3848a | 1.776 ms · 209521B · 5520a | — | 2.367 ms · 503617B · 31846a |
| struct-tree-walk | 283.1 µs · 768B · 8a | 108.8 µs · 768B · 8a | 2.6× | — | 1.876 ns · 0B · 0a | 648.7 µs · 458379B · 5114a | 1.363 ms · 818512B · 11253a | 1.096 ms · 558961B · 6149a | 3.512 ms · 2570670B · 34797a | — | 1.228 µs · 600B · 17a |
| tail-ping-pong | 29.16 µs · 0B · 0a | 512.6 ns · 0B · 0a | 56.9× | — | 890.4 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 35.93 µs · 21856B · 85a | 109.1 µs · 13984B · 1726a | 117.4 µs · 14280B · 1731a | — | 550.7 µs · 348834B · 9016a |
| tail-sum | 29.1 µs · 0B · 0a | 690.1 ns · 0B · 0a | 42.2× | — | 301.9 ns · 0B · 0a | 106.3 µs · 106112B · 2001a | 35.73 µs · 21856B · 85a | 109.8 µs · 13984B · 1726a | 117.8 µs · 14280B · 1731a | — | 84.1 µs · 4608B · 1017a |
| typed-array-sum | 7.353 µs · 0B · 0a | 235.2 ns · 0B · 0a | 31.3× | 266.8 ns · 8B · 1a | 1.877 ns · 0B · 0a | 43.85 µs · 94208B · 513a | 8.262 µs · 4000B · 15a | 27.24 µs · 2080B · 238a | 17.32 µs · 2496B · 246a | — | — |
| xorshift-i64 | 113.3 µs · 18424B · 2046a | 1.371 µs · 18424B · 2046a | 82.7× | — | 360.3 ns · 0B · 0a | 103 µs · 107608B · 2188a | — | 1.004 ms · 447251B · 12735a | 429.6 µs · 199937B · 7772a | — | 101.2 µs · 13545B · 1317a |

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
