# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.811 µs · 0B · 0a | 5.393 µs · 0B · 0a | 1.63× | — | 2.222 µs · 1024B · 128a | 37.63 µs · 96289B · 388a | 17.06 µs · 14375B · 256a | 71.18 µs · 78016B · 770a | 14.7 µs · 5712B · 266a | — | 30.62 µs · 2308B · 276a |
| binary-trees | 1.995 ms · 768B · 8a | 599.3 µs · 768B · 8a | 3.33× | — | 296.5 µs · 201936B · 8414a | 3.91 ms · 1468184B · 62877a | 10.65 ms · 6995674B · 92703a | 8.958 ms · 5198699B · 69052a | 27.78 ms · 19457578B · 280714a | — | 30.06 ms · 15128166B · 446106a |
| branch-tree | 1.275 µs · 0B · 0a | 141.6 ns · 0B · 0a | 9× | 625.6 ns · 16B · 1a | 160.5 ns · 0B · 0a | 45.9 µs · 95384B · 660a | 15.06 µs · 2463B · 9a | 26.49 µs · 1992B · 196a | 24.86 µs · 2168B · 203a | — | 20.79 µs · 1640B · 219a |
| closure-counter | 5.553 µs · 64B · 2a | 903 ns · 64B · 2a | 6.15× | — | 39.81 ns · 0B · 0a | 42.96 µs · 92272B · 261a | 12.63 µs · 151B · 3a | 19.4 µs · 1264B · 13a | 79.47 µs · 58312B · 659a | — | 92.15 µs · 35628B · 922a |
| fannkuch | 826.6 µs · 34608B · 1442a | 274.8 µs · 34608B · 1442a | 3.01× | — | 35.39 µs · 17280B · 720a | 2.032 ms · 456578B · 25621a | 1.549 ms · 443136B · 1460a | 19.34 ms · 14005354B · 164946a | 3.397 ms · 1367679B · 16944a | — | 4.868 ms · 2501427B · 88439a |
| fnv1a64 | 123 µs · 16400B · 2049a | 1.148 µs · 16400B · 2049a | 107× | — | 903.5 ns · 0B · 0a | 161.8 µs · 120808B · 3838a | — | 1.453 ms · 608258B · 18475a | 393.2 µs · 162921B · 7429a | — | 153.7 µs · 17282B · 2074a |
| i64-wide-fib | 28.15 ms · 2913456B · 364178a | 681.9 µs · 16B · 1a | 41.3× | — | 238.5 µs · 0B · 0a | 23.74 µs · 90520B · 52a | — | 159.6 ms · 64193232B · 1942305a | 349.2 ms · 253472512B · 4248840a | — | 133.2 ms · 98686592B · 2138708a |
| indirect-recursive-fib | 1.188 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.6× | 98.95 µs · 8B · 1a | 34.78 µs · 0B · 0a | 2.09 ms · 319345B · 28655a | 1.92 ms · 703B · 2a | 2.554 ms · 4680B · 39a | 11.48 ms · 10158185B · 109494a | — | 24.53 ms · 13061340B · 394061a |
| iterative-fib | 1.103 µs · 0B · 0a | 71.72 ns · 0B · 0a | 15.4× | 85.81 ns · 8B · 1a | 11.9 ns · 0B · 0a | 25.18 µs · 90592B · 61a | 1.161 µs · 160B · 0a | 4.288 µs · 368B · 20a | 6.19 µs · 2448B · 88a | — | 6.217 µs · 944B · 81a |
| mandelbrot | 300 µs · 0B · 0a | 10.89 µs · 0B · 0a | 27.5× | — | 6.344 µs · 0B · 0a | 967.6 µs · 292345B · 25008a | 496.2 µs · 169408B · 661a | 8.217 ms · 5558646B · 104191a | 1.513 ms · 324978B · 23643a | — | 1.553 ms · 365401B · 22513a |
| mat-mul | 367.5 µs · 6216B · 6a | 8.825 µs · 6216B · 6a | 41.6× | — | 5.857 µs · 6144B · 3a | 1.384 ms · 431714B · 34871a | 684.3 µs · 143168B · 478a | 1.219 ms · 61304B · 537a | 1.485 ms · 90712B · 9350a | — | 923.5 µs · 36614B · 6036a |
| n-body | 607.4 µs · 504B · 14a | 18.62 µs · 504B · 14a | 32.6× | — | 7.343 µs · 0B · 0a | 1.535 ms · 386898B · 35892a | 1.233 ms · 258913B · 2002a | 3.37 ms · 658436B · 81545a | 2.499 ms · 382714B · 34975a | — | 1.769 ms · 445465B · 20669a |
| n-queens | 428.8 µs · 120B · 6a | 31.65 µs · 120B · 6a | 13.5× | — | 6.022 µs · 0B · 0a | 969.7 µs · 234865B · 17784a | 568.3 µs · 14423B · 63a | 1.106 ms · 7424B · 35a | 1.445 ms · 410346B · 5147a | — | 2.014 ms · 766005B · 33371a |
| permutation-flips | 129.2 µs · 7680B · 128a | 18.19 µs · 7680B · 128a | 7.1× | — | 1.889 ns · 0B · 0a | 566 µs · 292857B · 9856a | 238.6 µs · 78808B · 451a | 534.5 µs · 122504B · 765a | 495.4 µs · 111976B · 2368a | — | 1.09 µs · 528B · 13a |
| recursive-fib-20 | 710.8 µs · 0B · 0a | 47.32 µs · 0B · 0a | 15× | 42.78 µs · 8B · 1a | 21.56 µs · 0B · 0a | 2.008 ms · 319345B · 28655a | 2.152 ms · 704B · 2a | 3.023 ms · 4680B · 39a | 11.55 ms · 9807923B · 109494a | — | 11.21 ms · 8302667B · 192847a |
| recursive-fib-35 | 971 ms · 0B · 0a | 64.05 ms · 0B · 0a | 15.2× | 58.18 ms · 8B · 1a | 29.08 ms · 0B · 0a | 2.691 s · 312797456B · 39088168a | 2.94 s · 971008B · 3793a | 4.056 s · 375360B · 46373a | 15.96 s · 13378016816B · 149349991a | — | — |
| sieve | 19.97 µs · 1048B · 2a | 972 ns · 1048B · 2a | 20.5× | 1.047 µs · 8B · 1a | 350.6 ns · 0B · 0a | 139.7 µs · 122504B · 1611a | 51.01 µs · 18416B · 44a | 84.94 µs · 1872B · 25a | 77.71 µs · 5704B · 30a | — | 81.49 µs · 4368B · 795a |
| sort-stress | 508.6 µs · 5136B · 512a | 12.71 µs · 5136B · 512a | 40× | — | 5.952 µs · 1024B · 2a | 1.302 ms · 270945B · 19965a | 855.7 µs · 36567B · 365a | 1.819 ms · 49104B · 3621a | 1.788 ms · 23448B · 2034a | — | 922.6 µs · 17474B · 1708a |
| spectral-norm | 531.4 µs · 648B · 6a | 29.9 µs · 648B · 6a | 17.8× | — | 7.138 µs · 576B · 3a | 2.095 ms · 571371B · 54822a | 1.098 ms · 234784B · 925a | 2.506 ms · 275338B · 33729a | 4.729 ms · 2457135B · 52718a | — | 4.29 ms · 2448018B · 74233a |
| string-build | 742 µs · 85408B · 4107a | 628.4 µs · 85408B · 4107a | 1.18× | — | 430.1 µs · 855890B · 5001a | 2.494 ms · 2211276B · 36101a | 2.365 ms · 1078109B · 13859a | 3.229 ms · 1147384B · 26883a | 2.52 ms · 1326242B · 24469a | — | 3.219 ms · 2372790B · 41432a |
| string-compare | 1.091 ms · 21136B · 1284a | 270.2 µs · 21136B · 1284a | 4.04× | — | 64.39 µs · 9632B · 1585a | 1.706 ms · 296281B · 20528a | 1.143 ms · 59712B · 3431a | 2.339 ms · 50744B · 3848a | 1.853 ms · 209521B · 5520a | — | 2.378 ms · 503617B · 31846a |
| struct-tree-walk | 283.8 µs · 768B · 8a | 108.3 µs · 768B · 8a | 2.62× | — | 1.875 ns · 0B · 0a | 659.1 µs · 458379B · 5114a | 1.388 ms · 818515B · 11253a | 1.104 ms · 558960B · 6149a | 3.58 ms · 2570663B · 34797a | — | 1.272 µs · 600B · 17a |
| tail-ping-pong | 29.47 µs · 0B · 0a | 474.4 ns · 0B · 0a | 62.1× | — | 890.9 ns · 0B · 0a | 109.7 µs · 106112B · 2001a | 37.39 µs · 21855B · 85a | 111.9 µs · 13984B · 1726a | 120.6 µs · 14280B · 1731a | — | 554.6 µs · 348832B · 9016a |
| tail-sum | 29.71 µs · 0B · 0a | 671.9 ns · 0B · 0a | 44.2× | — | 301.6 ns · 0B · 0a | 110.2 µs · 106112B · 2001a | 36.21 µs · 21856B · 85a | 112.1 µs · 13984B · 1726a | 120.8 µs · 14280B · 1731a | — | 84.96 µs · 4608B · 1017a |
| typed-array-sum | 7.218 µs · 0B · 0a | 261.8 ns · 0B · 0a | 27.6× | 269.1 ns · 8B · 1a | 1.874 ns · 0B · 0a | 46.9 µs · 94208B · 513a | 8.521 µs · 4000B · 15a | 27.39 µs · 2080B · 238a | 17.39 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.7 µs · 18424B · 2046a | 1.452 µs · 18424B · 2046a | 76.9× | — | 359.9 ns · 0B · 0a | 104.6 µs · 107608B · 2188a | — | 1.034 ms · 447249B · 12735a | 435 µs · 199937B · 7772a | — | 101.6 µs · 13545B · 1317a |

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
