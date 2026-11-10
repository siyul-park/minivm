# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.009 µs · 0B · 0a | 5.374 µs · 0B · 0a | 1.68× | — | 2.216 µs · 1024B · 128a | 40.47 µs · 96288B · 388a | 20.04 µs · 14376B · 256a | 80.11 µs · 78016B · 770a | 14.25 µs · 5712B · 266a | — | 30.75 µs · 2308B · 276a |
| binary-trees | 1.981 ms · 768B · 8a | 590.3 µs · 768B · 8a | 3.36× | — | 291.3 µs · 201936B · 8414a | 3.902 ms · 1468185B · 62877a | 10.77 ms · 6995680B · 92703a | 9.069 ms · 5198699B · 69052a | 28.16 ms · 19457596B · 280714a | — | 28.68 ms · 15128180B · 446106a |
| branch-tree | 1.3 µs · 0B · 0a | 135.8 ns · 0B · 0a | 9.57× | 624.8 ns · 16B · 1a | 160.5 ns · 0B · 0a | 48.07 µs · 95385B · 660a | 15.29 µs · 2463B · 9a | 26.57 µs · 1992B · 196a | 24.71 µs · 2168B · 203a | — | 20.68 µs · 1640B · 219a |
| closure-counter | 5.45 µs · 64B · 2a | 873.4 ns · 64B · 2a | 6.24× | — | 39.86 ns · 0B · 0a | 42.09 µs · 92272B · 261a | 13.12 µs · 151B · 3a | 18.97 µs · 1264B · 13a | 78.18 µs · 58312B · 659a | — | 90.56 µs · 35628B · 922a |
| fannkuch | 821.9 µs · 34608B · 1442a | 221 µs · 34608B · 1442a | 3.72× | — | 35.29 µs · 17280B · 720a | 2.045 ms · 456579B · 25621a | 1.555 ms · 443136B · 1460a | 19.56 ms · 14005353B · 164946a | 3.379 ms · 1367678B · 16944a | — | 4.698 ms · 2501454B · 88439a |
| fnv1a64 | 118.1 µs · 16400B · 2049a | 1.147 µs · 16400B · 2049a | 103× | — | 903.6 ns · 0B · 0a | 162.6 µs · 120808B · 3838a | — | 1.472 ms · 608252B · 18475a | 398.7 µs · 162921B · 7429a | — | 153.2 µs · 17282B · 2074a |
| i64-wide-fib | 28 ms · 2913456B · 364178a | 681.3 µs · 16B · 1a | 41.1× | — | 239.2 µs · 0B · 0a | 23.62 µs · 90520B · 52a | — | 159.1 ms · 64193448B · 1942307a | 349.8 ms · 253472816B · 4248841a | — | 125.8 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 60.46 µs · 0B · 0a | 19.6× | 99.19 µs · 8B · 1a | 34.76 µs · 0B · 0a | 2.08 ms · 319345B · 28655a | 1.932 ms · 704B · 2a | 2.485 ms · 4680B · 39a | 11.48 ms · 10158190B · 109494a | — | 24.53 ms · 13061340B · 394061a |
| iterative-fib | 1.105 µs · 0B · 0a | 72.07 ns · 0B · 0a | 15.3× | 84.89 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.41 µs · 90592B · 61a | 1.144 µs · 160B · 0a | 4.196 µs · 368B · 20a | 6.073 µs · 2448B · 88a | — | 6.22 µs · 944B · 81a |
| mandelbrot | 289 µs · 0B · 0a | 10.9 µs · 0B · 0a | 26.5× | — | 6.324 µs · 0B · 0a | 962.2 µs · 292345B · 25008a | 477.2 µs · 169408B · 661a | 8.103 ms · 5558658B · 104191a | 1.484 ms · 324978B · 23643a | — | 1.523 ms · 365397B · 22513a |
| mat-mul | 375.4 µs · 6216B · 6a | 8.745 µs · 6216B · 6a | 42.9× | — | 5.62 µs · 6144B · 3a | 1.374 ms · 431714B · 34871a | 681.6 µs · 143168B · 478a | 1.203 ms · 61304B · 537a | 1.456 ms · 90713B · 9350a | — | 919.7 µs · 36614B · 6036a |
| n-body | 605.1 µs · 504B · 14a | 18.8 µs · 504B · 14a | 32.2× | — | 7.344 µs · 0B · 0a | 1.494 ms · 386898B · 35892a | 1.226 ms · 258914B · 2002a | 3.244 ms · 658436B · 81545a | 2.413 ms · 382714B · 34975a | — | 1.746 ms · 445474B · 20669a |
| n-queens | 428.6 µs · 120B · 6a | 31.72 µs · 120B · 6a | 13.5× | — | 6.015 µs · 0B · 0a | 943.5 µs · 234865B · 17784a | 566.2 µs · 14423B · 63a | 1.106 ms · 7424B · 35a | 1.407 ms · 410345B · 5147a | — | 2.001 ms · 766007B · 33371a |
| permutation-flips | 128.9 µs · 7680B · 128a | 17.32 µs · 7680B · 128a | 7.44× | — | 1.888 ns · 0B · 0a | 569.3 µs · 292858B · 9856a | 235.1 µs · 78808B · 451a | 526.8 µs · 122504B · 765a | 490.6 µs · 111976B · 2368a | — | 1.021 µs · 528B · 13a |
| recursive-fib-20 | 714.2 µs · 0B · 0a | 47.16 µs · 0B · 0a | 15.1× | 42.71 µs · 8B · 1a | 21.67 µs · 0B · 0a | 1.978 ms · 319346B · 28655a | 2.173 ms · 704B · 2a | 3.071 ms · 4680B · 39a | 11.83 ms · 9807910B · 109494a | — | 10.6 ms · 8302671B · 192847a |
| recursive-fib-35 | 974.9 ms · 0B · 0a | 64.06 ms · 0B · 0a | 15.2× | 58.22 ms · 8B · 1a | 29.11 ms · 0B · 0a | 2.648 s · 312797488B · 39088168a | 2.962 s · 971008B · 3793a | 4.087 s · 375360B · 46373a | 15.87 s · 13378018304B · 149349968a | — | — |
| sieve | 19.88 µs · 1048B · 2a | 944.4 ns · 1048B · 2a | 21× | 1.033 µs · 8B · 1a | 351.2 ns · 0B · 0a | 139.7 µs · 122504B · 1611a | 50.73 µs · 18416B · 44a | 83.81 µs · 1872B · 25a | 76.51 µs · 5704B · 30a | — | 81.58 µs · 4368B · 795a |
| sort-stress | 500.9 µs · 5136B · 512a | 12.62 µs · 5136B · 512a | 39.7× | — | 5.826 µs · 1024B · 2a | 1.323 ms · 270945B · 19965a | 852.5 µs · 36568B · 365a | 1.795 ms · 49104B · 3621a | 1.777 ms · 23448B · 2034a | — | 925.3 µs · 17474B · 1708a |
| spectral-norm | 531 µs · 648B · 6a | 8.328 µs · 648B · 6a | 63.8× | — | 7.054 µs · 576B · 3a | 2.146 ms · 571371B · 54822a | 1.079 ms · 234784B · 925a | 2.476 ms · 275337B · 33729a | 4.794 ms · 2457134B · 52718a | — | 4.314 ms · 2448024B · 74233a |
| string-build | 731.1 µs · 85408B · 4107a | 613.3 µs · 85408B · 4107a | 1.19× | — | 378.9 µs · 855890B · 5001a | 2.489 ms · 2211272B · 36101a | 2.319 ms · 1078111B · 13859a | 3.111 ms · 1147385B · 26883a | 2.489 ms · 1326243B · 24469a | — | 3.108 ms · 2372781B · 41432a |
| string-compare | 1.103 ms · 21136B · 1284a | 238.3 µs · 21136B · 1284a | 4.63× | — | 63.95 µs · 9632B · 1585a | 1.719 ms · 296282B · 20528a | 1.14 ms · 59712B · 3431a | 2.317 ms · 50744B · 3848a | 1.846 ms · 209521B · 5520a | — | 2.387 ms · 503618B · 31846a |
| struct-tree-walk | 285.6 µs · 768B · 8a | 107.3 µs · 768B · 8a | 2.66× | — | 1.875 ns · 0B · 0a | 655.7 µs · 458378B · 5114a | 1.389 ms · 818512B · 11253a | 1.101 ms · 558960B · 6149a | 3.536 ms · 2570671B · 34797a | — | 1.254 µs · 600B · 17a |
| tail-ping-pong | 28.94 µs · 0B · 0a | 471.1 ns · 0B · 0a | 61.4× | — | 891.1 ns · 0B · 0a | 107.7 µs · 106112B · 2001a | 36.09 µs · 21856B · 85a | 111.3 µs · 13984B · 1726a | 118.7 µs · 14280B · 1731a | — | 564 µs · 348834B · 9016a |
| tail-sum | 28.93 µs · 0B · 0a | 666.3 ns · 0B · 0a | 43.4× | — | 301.6 ns · 0B · 0a | 109.8 µs · 106112B · 2001a | 36 µs · 21856B · 85a | 112.4 µs · 13984B · 1726a | 119.2 µs · 14280B · 1731a | — | 84.78 µs · 4608B · 1017a |
| typed-array-sum | 7.279 µs · 0B · 0a | 263.1 ns · 0B · 0a | 27.7× | 266.7 ns · 8B · 1a | 1.874 ns · 0B · 0a | 45.75 µs · 94208B · 513a | 8.307 µs · 4000B · 15a | 27.09 µs · 2080B · 238a | 17.33 µs · 2496B · 246a | — | — |
| xorshift-i64 | 114.8 µs · 18424B · 2046a | 1.378 µs · 18424B · 2046a | 83.3× | — | 360.2 ns · 0B · 0a | 105.1 µs · 107608B · 2188a | — | 1.003 ms · 447250B · 12735a | 436.3 µs · 199937B · 7772a | — | 101.9 µs · 13545B · 1317a |

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
