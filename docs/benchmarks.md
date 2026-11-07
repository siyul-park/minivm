# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.89 µs · 0B · 0a | 8.898 µs · 0B · 0a | 0.999× | — | 2.213 µs · 1024B · 128a | 39.05 µs · 96288B · 388a | 17.42 µs · 14376B · 256a | 74.51 µs · 78016B · 770a | 15.68 µs · 5712B · 266a | 5.819 µs · 0B · 0a | 24.33 µs · 1744B · 148a |
| binary-trees | 1.98 ms · 768B · 8a | 1.206 ms · 768B · 8a | 1.64× | — | 297.5 µs · 201936B · 8414a | 3.979 ms · 1468186B · 62877a | 11.07 ms · 6995678B · 92703a | 9.302 ms · 5198702B · 69052a | 28.64 ms · 19457585B · 280714a | 2.135 ms · 120B · 0a | 29.4 ms · 15127309B · 446011a |
| branch-tree | 1.303 µs · 0B · 0a | 138.1 ns · 0B · 0a | 9.44× | 626.4 ns · 16B · 1a | 160.7 ns · 0B · 0a | 47.01 µs · 95385B · 660a | 15.21 µs · 2464B · 9a | 26.67 µs · 1992B · 196a | 25.08 µs · 2168B · 203a | 9.464 µs · 0B · 0a | 14.91 µs · 1252B · 122a |
| closure-counter | 5.697 µs · 64B · 2a | 925.6 ns · 64B · 2a | 6.15× | — | 39.85 ns · 0B · 0a | 42.06 µs · 92272B · 261a | 12.71 µs · 151B · 3a | 19.18 µs · 1264B · 13a | 81.56 µs · 58312B · 659a | 8.581 µs · 0B · 0a | 83.4 µs · 35048B · 793a |
| fannkuch | 823.6 µs · 34608B · 1442a | 262.8 µs · 34608B · 1442a | 3.13× | — | 35.57 µs · 17280B · 720a | 2.062 ms · 456579B · 25621a | 1.556 ms · 443135B · 1460a | 19.46 ms · 14005344B · 164946a | 3.412 ms · 1367685B · 16944a | 620.1 µs · 34B · 0a | 4.638 ms · 2491452B · 85959a |
| fnv1a64 | 118.8 µs · 16400B · 2049a | 1.198 µs · 16400B · 2049a | 99.2× | — | 902.9 ns · 0B · 0a | 162.9 µs · 120808B · 3838a | — | 1.444 ms · 608252B · 18475a | 408.7 µs · 162921B · 7429a | 137.2 µs · 8B · 0a | 102.1 µs · 9105B · 1050a |
| i64-wide-fib | 27.61 ms · 2913456B · 364178a | 861.2 µs · 16B · 1a | 32.1× | — | 239.5 µs · 0B · 0a | 24.33 µs · 90520B · 52a | — | 158 ms · 64193240B · 1942305a | 360.5 ms · 253472752B · 4248841a | 34.97 ms · 2388B · 6a | 123.9 ms · 98686581B · 2138708a |
| indirect-recursive-fib | 1.193 ms · 0B · 0a | 76.71 µs · 0B · 0a | 15.5× | 99.44 µs · 8B · 1a | 35.36 µs · 0B · 0a | 2.096 ms · 319345B · 28655a | 1.93 ms · 704B · 2a | 2.545 ms · 4680B · 39a | 12.01 ms · 10158185B · 109494a | 928.8 µs · 52B · 0a | 24.14 ms · 13061340B · 394061a |
| iterative-fib | 1.109 µs · 0B · 0a | 70.46 ns · 0B · 0a | 15.7× | 85.71 ns · 8B · 1a | 11.89 ns · 0B · 0a | 27.31 µs · 90592B · 61a | 1.181 µs · 160B · 0a | 4.284 µs · 368B · 20a | 6.524 µs · 2448B · 88a | 856.3 ns · 0B · 0a | 4.485 µs · 784B · 50a |
| mandelbrot | 294.9 µs · 0B · 0a | 11.09 µs · 0B · 0a | 26.6× | — | 6.321 µs · 0B · 0a | 971 µs · 292345B · 25008a | 495.7 µs · 169408B · 661a | 8.477 ms · 5558642B · 104191a | 1.517 ms · 324978B · 23643a | 367.6 µs · 21B · 0a | 1.277 ms · 324085B · 19107a |
| mat-mul | 370.3 µs · 6216B · 6a | 8.858 µs · 6216B · 6a | 41.8× | — | 5.977 µs · 6144B · 3a | 1.391 ms · 431714B · 34871a | 686.6 µs · 143168B · 478a | 1.217 ms · 61304B · 537a | 1.458 ms · 90712B · 9350a | 374.7 µs · 21B · 0a | 620.9 µs · 16875B · 1134a |
| n-body | 610.2 µs · 504B · 14a | 18.14 µs · 504B · 14a | 33.6× | — | 7.401 µs · 0B · 0a | 1.521 ms · 386897B · 35892a | 1.241 ms · 258913B · 2002a | 3.375 ms · 658435B · 81545a | 2.46 ms · 382716B · 34975a | 499.2 µs · 29B · 0a | 1.618 ms · 400491B · 18248a |
| n-queens | 428.4 µs · 120B · 6a | 32.92 µs · 120B · 6a | 13× | — | 6.024 µs · 0B · 0a | 977 µs · 234865B · 17784a | 571.8 µs · 14423B · 63a | 1.1 ms · 7424B · 35a | 1.485 ms · 410346B · 5147a | 283.9 µs · 16B · 0a | 1.779 ms · 749461B · 29235a |
| permutation-flips | 128.7 µs · 7680B · 128a | 22.7 µs · 7680B · 128a | 5.67× | — | 1.892 ns · 0B · 0a | 572.3 µs · 292858B · 9856a | 241 µs · 78808B · 451a | 543 µs · 122504B · 765a | 517.6 µs · 111976B · 2368a | 81.62 µs · 5B · 0a | 1.05 µs · 528B · 13a |
| recursive-fib-20 | 711.5 µs · 0B · 0a | 60.94 µs · 0B · 0a | 11.7× | 42.79 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.97 ms · 319345B · 28655a | 2.19 ms · 703B · 2a | 2.987 ms · 4680B · 39a | 12.02 ms · 9807934B · 109494a | 883.2 µs · 52B · 0a | 10.78 ms · 8302723B · 192848a |
| recursive-fib-35 | 967.4 ms · 0B · 0a | 82.83 ms · 0B · 0a | 11.7× | 58.18 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.651 s · 312797552B · 39088168a | 2.952 s · 971008B · 3793a | 4.033 s · 375360B · 46373a | 16 s · 13378015552B · 149349969a | 1.209 s · 19088B · 52a | — |
| sieve | 19.96 µs · 1048B · 2a | 965.7 ns · 1048B · 2a | 20.7× | 1.044 µs · 8B · 1a | 350.8 ns · 0B · 0a | 144.1 µs · 122504B · 1611a | 52.81 µs · 18416B · 44a | 84.36 µs · 1872B · 25a | 80.73 µs · 5704B · 30a | 18.95 µs · 1B · 0a | 40.62 µs · 1256B · 43a |
| sort-stress | 505.5 µs · 5136B · 512a | 12.68 µs · 5136B · 512a | 39.9× | — | 5.993 µs · 1024B · 2a | 1.302 ms · 270945B · 19965a | 854.3 µs · 36568B · 365a | 1.807 ms · 49104B · 3621a | 1.796 ms · 23448B · 2034a | 511.6 µs · 29B · 0a | 880.1 µs · 13121B · 935a |
| spectral-norm | 533.5 µs · 648B · 6a | 35.03 µs · 648B · 6a | 15.2× | — | 7.136 µs · 576B · 3a | 2.024 ms · 571371B · 54822a | 1.103 ms · 234784B · 925a | 2.473 ms · 275337B · 33729a | 4.735 ms · 2457136B · 52718a | 829.8 µs · 47B · 0a | 3.784 ms · 2443907B · 69364a |
| string-build | 724 µs · 85408B · 4107a | 591.8 µs · 85408B · 4107a | 1.22× | — | 495.8 µs · 855890B · 5001a | 2.692 ms · 2211272B · 36101a | 2.465 ms · 1078110B · 13859a | 3.2 ms · 1147383B · 26883a | 2.594 ms · 1326241B · 24469a | 550.1 µs · 31B · 0a | 3.135 ms · 2348541B · 38417a |
| struct-tree-walk | 281.9 µs · 768B · 8a | 182.3 µs · 768B · 8a | 1.55× | — | 1.891 ns · 0B · 0a | 697 µs · 458379B · 5114a | 1.343 ms · 818513B · 11253a | 1.096 ms · 558961B · 6149a | 3.551 ms · 2570667B · 34797a | 246.5 µs · 14B · 0a | 1.138 µs · 608B · 17a |
| tail-ping-pong | 29.81 µs · 0B · 0a | 511.5 ns · 0B · 0a | 58.3× | — | 890.8 ns · 0B · 0a | 111.4 µs · 106112B · 2001a | 37.14 µs · 21855B · 85a | 112.8 µs · 13984B · 1726a | 117 µs · 14280B · 1731a | 46.12 µs · 2B · 0a | 527.2 µs · 348834B · 9016a |
| tail-sum | 29.51 µs · 0B · 0a | 646.9 ns · 0B · 0a | 45.6× | — | 301.5 ns · 0B · 0a | 112.9 µs · 106112B · 2001a | 37.51 µs · 21856B · 85a | 113.9 µs · 13984B · 1726a | 117.5 µs · 14280B · 1731a | 46.32 µs · 2B · 0a | 30.01 µs · 584B · 16a |
| typed-array-sum | 7.412 µs · 0B · 0a | 228.6 ns · 0B · 0a | 32.4× | 267.9 ns · 8B · 1a | 1.901 ns · 0B · 0a | 52.01 µs · 94208B · 513a | 8.756 µs · 4000B · 15a | 27.47 µs · 2080B · 238a | 17.92 µs · 2496B · 246a | 6.258 µs · 0B · 0a | — |
| xorshift-i64 | 110.2 µs · 18424B · 2046a | 1.534 µs · 18424B · 2046a | 71.9× | — | 360.3 ns · 0B · 0a | 106.2 µs · 107608B · 2188a | — | 921.5 µs · 447254B · 12736a | 412.9 µs · 199938B · 7772a | 91.88 µs · 5B · 0a | 62.66 µs · 7361B · 547a |

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
