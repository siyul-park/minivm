# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.93 µs · 0B · 0a | 5.322 µs · 0B · 0a | 1.68× | — | 2.123 µs · 1024B · 128a | 37.71 µs · 96288B · 388a | 17.05 µs · 14376B · 256a | 74.72 µs · 78016B · 770a | 15.79 µs · 5712B · 266a | 5.873 µs · 0B · 0a | 23.54 µs · 1744B · 148a |
| binary-trees | 1.973 ms · 768B · 8a | 605.3 µs · 768B · 8a | 3.26× | — | 290.9 µs · 201936B · 8414a | 3.928 ms · 1468184B · 62877a | 10.59 ms · 6995693B · 92703a | 8.948 ms · 5198700B · 69052a | 28.09 ms · 19457605B · 280714a | 2.137 ms · 120B · 0a | 29.02 ms · 15127313B · 446011a |
| branch-tree | 1.312 µs · 0B · 0a | 139.1 ns · 0B · 0a | 9.43× | 627.3 ns · 16B · 1a | 160.8 ns · 0B · 0a | 48.59 µs · 95385B · 660a | 15.18 µs · 2464B · 9a | 26.46 µs · 1992B · 196a | 24.17 µs · 2168B · 203a | 9.452 µs · 0B · 0a | 15.05 µs · 1252B · 122a |
| closure-counter | 5.489 µs · 64B · 2a | 941.7 ns · 64B · 2a | 5.83× | — | 39.83 ns · 0B · 0a | 40.88 µs · 92272B · 261a | 12.59 µs · 151B · 3a | 19.15 µs · 1264B · 13a | 78.3 µs · 58312B · 659a | 8.656 µs · 0B · 0a | 83.3 µs · 35048B · 793a |
| fannkuch | 824.1 µs · 34608B · 1442a | 268.4 µs · 34608B · 1442a | 3.07× | — | 35.14 µs · 17280B · 720a | 2.002 ms · 456578B · 25621a | 1.529 ms · 443136B · 1460a | 19.12 ms · 14005333B · 164946a | 3.301 ms · 1367678B · 16944a | 619.5 µs · 37B · 0a | 4.619 ms · 2491463B · 85959a |
| fnv1a64 | 121.7 µs · 16400B · 2049a | 1.197 µs · 16400B · 2049a | 102× | — | 903.5 ns · 0B · 0a | 159.4 µs · 120808B · 3838a | — | 1.419 ms · 608253B · 18475a | 392.8 µs · 162921B · 7429a | 137.5 µs · 8B · 0a | 101.5 µs · 9105B · 1050a |
| i64-wide-fib | 28.33 ms · 2913456B · 364178a | 861.5 µs · 16B · 1a | 32.9× | — | 238.7 µs · 0B · 0a | 22.65 µs · 90520B · 52a | — | 156.4 ms · 64193456B · 1942307a | 331.9 ms · 253472816B · 4248842a | 34.73 ms · 2386B · 6a | 120.9 ms · 98686576B · 2138708a |
| indirect-recursive-fib | 1.185 ms · 0B · 0a | 76.46 µs · 0B · 0a | 15.5× | 100.4 µs · 8B · 1a | 35.32 µs · 0B · 0a | 2.1 ms · 319345B · 28655a | 1.925 ms · 704B · 2a | 2.569 ms · 4680B · 39a | 11.03 ms · 10158183B · 109494a | 928.6 µs · 51B · 0a | 25.08 ms · 13061318B · 394061a |
| iterative-fib | 1.1 µs · 0B · 0a | 73.43 ns · 0B · 0a | 15× | 86.2 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.23 µs · 90592B · 61a | 1.155 µs · 160B · 0a | 4.251 µs · 368B · 20a | 6.117 µs · 2448B · 88a | 853.9 ns · 0B · 0a | 4.438 µs · 784B · 50a |
| mandelbrot | 289.9 µs · 0B · 0a | 11.11 µs · 0B · 0a | 26.1× | — | 6.321 µs · 0B · 0a | 959.9 µs · 292345B · 25008a | 476.1 µs · 169408B · 661a | 7.978 ms · 5558652B · 104191a | 1.537 ms · 324978B · 23643a | 366.8 µs · 20B · 0a | 1.273 ms · 324082B · 19107a |
| mat-mul | 367 µs · 6216B · 6a | 8.581 µs · 6216B · 6a | 42.8× | — | 5.405 µs · 6144B · 3a | 1.35 ms · 431714B · 34871a | 664.3 µs · 143168B · 478a | 1.205 ms · 61304B · 537a | 1.474 ms · 90712B · 9350a | 371.8 µs · 21B · 0a | 608.5 µs · 16873B · 1134a |
| n-body | 604.1 µs · 504B · 14a | 18.89 µs · 504B · 14a | 32× | — | 7.348 µs · 0B · 0a | 1.501 ms · 386898B · 35892a | 1.213 ms · 258913B · 2002a | 3.198 ms · 658435B · 81545a | 2.416 ms · 382714B · 34975a | 501.8 µs · 28B · 0a | 1.591 ms · 400499B · 18248a |
| n-queens | 428.2 µs · 120B · 6a | 32.93 µs · 120B · 6a | 13× | — | 6.015 µs · 0B · 0a | 926 µs · 234865B · 17784a | 564.2 µs · 14423B · 63a | 1.104 ms · 7424B · 35a | 1.418 ms · 410346B · 5147a | 285.6 µs · 16B · 0a | 1.764 ms · 749464B · 29235a |
| permutation-flips | 126.4 µs · 7680B · 128a | 17.23 µs · 7680B · 128a | 7.33× | — | 1.877 ns · 0B · 0a | 550.9 µs · 292857B · 9856a | 231.1 µs · 78808B · 451a | 521.5 µs · 122504B · 765a | 491.9 µs · 111976B · 2368a | 81.53 µs · 5B · 0a | 961.3 ns · 528B · 13a |
| recursive-fib-20 | 712.2 µs · 0B · 0a | 60.88 µs · 0B · 0a | 11.7× | 42.75 µs · 8B · 1a | 21.62 µs · 0B · 0a | 1.947 ms · 319346B · 28655a | 2.24 ms · 703B · 2a | 2.973 ms · 4680B · 39a | 11.34 ms · 9807919B · 109494a | 883.4 µs · 50B · 0a | 9.945 ms · 8302690B · 192848a |
| recursive-fib-35 | 971.4 ms · 0B · 0a | 82.87 ms · 0B · 0a | 11.7× | 58.23 ms · 8B · 1a | 29.05 ms · 0B · 0a | 2.56 s · 312797152B · 39088167a | 3.009 s · 971008B · 3793a | 4.044 s · 375360B · 46373a | 15.29 s · 13378018816B · 149349978a | 1.204 s · 19088B · 52a | — |
| sieve | 19.91 µs · 1048B · 2a | 895.3 ns · 1048B · 2a | 22.2× | 1.038 µs · 8B · 1a | 350.8 ns · 0B · 0a | 135.5 µs · 122504B · 1611a | 50.15 µs · 18416B · 44a | 84.28 µs · 1872B · 25a | 77.34 µs · 5704B · 30a | 18.93 µs · 1B · 0a | 40.27 µs · 1256B · 43a |
| sort-stress | 507.3 µs · 5136B · 512a | 12.6 µs · 5136B · 512a | 40.3× | — | 5.797 µs · 1024B · 2a | 1.286 ms · 270945B · 19965a | 840 µs · 36568B · 365a | 1.787 ms · 49104B · 3621a | 1.791 ms · 23448B · 2034a | 512.8 µs · 29B · 0a | 877.7 µs · 13122B · 935a |
| spectral-norm | 530.5 µs · 648B · 6a | 34.74 µs · 648B · 6a | 15.3× | — | 7.041 µs · 576B · 3a | 2.043 ms · 571371B · 54822a | 1.065 ms · 234784B · 925a | 2.481 ms · 275337B · 33729a | 4.648 ms · 2457135B · 52718a | 824 µs · 46B · 0a | 3.819 ms · 2443913B · 69364a |
| string-build | 731.3 µs · 85408B · 4107a | 573.9 µs · 85408B · 4107a | 1.27× | — | 352 µs · 855890B · 5001a | 2.344 ms · 2211271B · 36101a | 2.343 ms · 1078111B · 13859a | 3.055 ms · 1147385B · 26883a | 2.422 ms · 1326242B · 24469a | 551.2 µs · 31B · 0a | 2.92 ms · 2348559B · 38417a |
| struct-tree-walk | 282.7 µs · 768B · 8a | 110.1 µs · 768B · 8a | 2.57× | — | 1.876 ns · 0B · 0a | 635 µs · 458379B · 5114a | 1.338 ms · 818512B · 11253a | 1.069 ms · 558960B · 6149a | 3.475 ms · 2570669B · 34797a | 244.5 µs · 14B · 0a | 1.145 µs · 608B · 17a |
| tail-ping-pong | 29.17 µs · 0B · 0a | 511.8 ns · 0B · 0a | 57× | — | 891.1 ns · 0B · 0a | 105.3 µs · 106112B · 2001a | 34.93 µs · 21856B · 85a | 110.4 µs · 13984B · 1726a | 118.8 µs · 14280B · 1731a | 46.41 µs · 2B · 0a | 550.1 µs · 348835B · 9016a |
| tail-sum | 29.29 µs · 0B · 0a | 647.6 ns · 0B · 0a | 45.2× | — | 302 ns · 0B · 0a | 105.7 µs · 106112B · 2001a | 34.8 µs · 21856B · 85a | 110.1 µs · 13984B · 1726a | 118 µs · 14280B · 1731a | 46.3 µs · 2B · 0a | 30.19 µs · 584B · 16a |
| typed-array-sum | 7.239 µs · 0B · 0a | 236.6 ns · 0B · 0a | 30.6× | 267.1 ns · 8B · 1a | 1.876 ns · 0B · 0a | 43 µs · 94208B · 513a | 8.107 µs · 4000B · 15a | 27.39 µs · 2080B · 238a | 17.32 µs · 2496B · 246a | 6.152 µs · 0B · 0a | — |
| xorshift-i64 | 112.6 µs · 18424B · 2046a | 1.381 µs · 18424B · 2046a | 81.5× | — | 360.9 ns · 0B · 0a | 102.9 µs · 107608B · 2188a | — | 941.5 µs · 447249B · 12735a | 410.3 µs · 199937B · 7772a | 91.81 µs · 5B · 0a | 61.92 µs · 7360B · 547a |

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
