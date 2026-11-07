# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.857 µs · 0B · 0a | 8.881 µs · 0B · 0a | 0.997× | — | 2.151 µs · 1024B · 128a | 38.48 µs · 96289B · 388a | 17.49 µs · 14376B · 256a | 70.65 µs · 78016B · 770a | 14.13 µs · 5712B · 266a | 9.399 µs · 19136B · 53a | 24.45 µs · 1744B · 148a |
| binary-trees | 1.971 ms · 768B · 8a | 1.217 ms · 768B · 8a | 1.62× | — | 282.8 µs · 201936B · 8414a | 3.889 ms · 1468185B · 62877a | 10.64 ms · 6995685B · 92703a | 8.844 ms · 5198701B · 69052a | 27.39 ms · 19457629B · 280714a | 2.133 ms · 119B · 0a | 30.09 ms · 15127326B · 446011a |
| branch-tree | 1.305 µs · 0B · 0a | 139.5 ns · 0B · 0a | 9.35× | 625.5 ns · 16B · 1a | 160.3 ns · 0B · 0a | 47.51 µs · 95384B · 660a | 15.22 µs · 2464B · 9a | 26.34 µs · 1992B · 196a | 24.23 µs · 2168B · 203a | 9.43 µs · 0B · 0a | 14.95 µs · 1252B · 122a |
| closure-counter | 5.524 µs · 64B · 2a | 934.9 ns · 64B · 2a | 5.91× | — | 39.83 ns · 0B · 0a | 42.66 µs · 92272B · 261a | 12.77 µs · 151B · 3a | 19.11 µs · 1264B · 13a | 77.81 µs · 58312B · 659a | 8.592 µs · 0B · 0a | 83.66 µs · 35048B · 793a |
| fannkuch | 820.1 µs · 34608B · 1442a | 265.9 µs · 34608B · 1442a | 3.08× | — | 35.23 µs · 17280B · 720a | 2.045 ms · 456582B · 25621a | 1.549 ms · 443136B · 1460a | 19.08 ms · 14005338B · 164946a | 3.321 ms · 1367678B · 16944a | 619.3 µs · 35B · 0a | 4.554 ms · 2491472B · 85959a |
| fnv1a64 | 118.7 µs · 16400B · 2049a | 1.193 µs · 16400B · 2049a | 99.5× | — | 902.6 ns · 0B · 0a | 163.1 µs · 120808B · 3838a | — | 1.464 ms · 608257B · 18475a | 388.1 µs · 162921B · 7429a | 137.2 µs · 8B · 0a | 102.4 µs · 9105B · 1050a |
| i64-wide-fib | 27.54 ms · 2913456B · 364178a | 860.3 µs · 16B · 1a | 32× | — | 238.7 µs · 0B · 0a | 24.35 µs · 90520B · 52a | — | 154.7 ms · 64193336B · 1942306a | 346.5 ms · 253473344B · 4248845a | 34.82 ms · 2388B · 6a | 127.2 ms · 98686597B · 2138708a |
| indirect-recursive-fib | 1.182 ms · 0B · 0a | 76.53 µs · 0B · 0a | 15.4× | 99.03 µs · 8B · 1a | 34.78 µs · 0B · 0a | 2.078 ms · 319345B · 28655a | 1.923 ms · 704B · 2a | 2.497 ms · 4680B · 39a | 11.35 ms · 10158179B · 109494a | 920.8 µs · 52B · 0a | 24.28 ms · 13061339B · 394061a |
| iterative-fib | 1.099 µs · 0B · 0a | 69.31 ns · 0B · 0a | 15.9× | 84.59 ns · 8B · 1a | 11.89 ns · 0B · 0a | 24.68 µs · 90592B · 61a | 1.154 µs · 160B · 0a | 4.225 µs · 368B · 20a | 6.075 µs · 2448B · 88a | 852.7 ns · 0B · 0a | 4.476 µs · 784B · 50a |
| mandelbrot | 290.6 µs · 0B · 0a | 11.11 µs · 0B · 0a | 26.2× | — | 6.336 µs · 0B · 0a | 957.2 µs · 292345B · 25008a | 486.2 µs · 169408B · 661a | 8.102 ms · 5558646B · 104191a | 1.476 ms · 324977B · 23643a | 367.9 µs · 20B · 0a | 1.27 ms · 324080B · 19107a |
| mat-mul | 368.2 µs · 6216B · 6a | 8.594 µs · 6216B · 6a | 42.8× | — | 5.439 µs · 6144B · 3a | 1.37 ms · 431714B · 34871a | 672.9 µs · 143168B · 478a | 1.209 ms · 61304B · 537a | 1.455 ms · 90713B · 9350a | 372.3 µs · 21B · 0a | 607.7 µs · 16875B · 1134a |
| n-body | 611 µs · 504B · 14a | 18.95 µs · 504B · 14a | 32.2× | — | 7.344 µs · 0B · 0a | 1.452 ms · 386898B · 35892a | 1.22 ms · 258913B · 2002a | 3.329 ms · 658435B · 81545a | 2.435 ms · 382715B · 34975a | 498.9 µs · 28B · 0a | 1.609 ms · 400501B · 18248a |
| n-queens | 429.8 µs · 120B · 6a | 32.82 µs · 120B · 6a | 13.1× | — | 6.014 µs · 0B · 0a | 963.4 µs · 234865B · 17784a | 569.5 µs · 14423B · 63a | 1.099 ms · 7424B · 35a | 1.437 ms · 410346B · 5147a | 284.1 µs · 16B · 0a | 1.775 ms · 749458B · 29235a |
| permutation-flips | 128 µs · 7680B · 128a | 22.61 µs · 7680B · 128a | 5.66× | — | 1.89 ns · 0B · 0a | 563.9 µs · 292857B · 9856a | 234.3 µs · 78808B · 451a | 526.3 µs · 122504B · 765a | 492.2 µs · 111976B · 2368a | 81.46 µs · 5B · 0a | 1.021 µs · 528B · 13a |
| recursive-fib-20 | 708.5 µs · 0B · 0a | 60.9 µs · 0B · 0a | 11.6× | 42.72 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.947 ms · 319345B · 28655a | 2.154 ms · 704B · 2a | 2.985 ms · 4680B · 39a | 11.47 ms · 9807915B · 109494a | 883.9 µs · 49B · 0a | 10.3 ms · 8302688B · 192847a |
| recursive-fib-35 | 964.8 ms · 0B · 0a | 82.8 ms · 0B · 0a | 11.7× | 58.23 ms · 8B · 1a | 29.07 ms · 0B · 0a | 2.568 s · 312797360B · 39088168a | 2.953 s · 971008B · 3793a | 4.065 s · 375360B · 46373a | 15.63 s · 13378015280B · 149349944a | 1.204 s · 20048B · 54a | — |
| sieve | 19.84 µs · 1048B · 2a | 924.9 ns · 1048B · 2a | 21.5× | 1.036 µs · 8B · 1a | 350.8 ns · 0B · 0a | 137.5 µs · 122504B · 1611a | 50.65 µs · 18416B · 44a | 84.14 µs · 1872B · 25a | 76.38 µs · 5704B · 30a | 18.86 µs · 1B · 0a | 40.49 µs · 1256B · 43a |
| sort-stress | 503.8 µs · 5136B · 512a | 12.43 µs · 5136B · 512a | 40.5× | — | 5.811 µs · 1024B · 2a | 1.294 ms · 270945B · 19965a | 840.5 µs · 36568B · 365a | 1.799 ms · 49104B · 3621a | 1.782 ms · 23448B · 2034a | 511.6 µs · 29B · 0a | 879.2 µs · 13121B · 935a |
| spectral-norm | 533.5 µs · 648B · 6a | 34.67 µs · 648B · 6a | 15.4× | — | 7.041 µs · 576B · 3a | 2.081 ms · 571371B · 54822a | 1.08 ms · 234784B · 925a | 2.437 ms · 275337B · 33729a | 4.653 ms · 2457134B · 52718a | 826.3 µs · 46B · 0a | 3.799 ms · 2443895B · 69364a |
| string-build | 717.7 µs · 85408B · 4107a | 583 µs · 85408B · 4107a | 1.23× | — | 369.3 µs · 855891B · 5001a | 2.42 ms · 2211276B · 36101a | 2.325 ms · 1078112B · 13859a | 3.13 ms · 1147384B · 26883a | 2.435 ms · 1326240B · 24469a | 556.2 µs · 30B · 0a | 2.964 ms · 2348542B · 38417a |
| struct-tree-walk | 281.6 µs · 768B · 8a | 183.1 µs · 768B · 8a | 1.54× | — | 1.89 ns · 0B · 0a | 648.5 µs · 458378B · 5114a | 1.382 ms · 818512B · 11253a | 1.092 ms · 558961B · 6149a | 3.496 ms · 2570668B · 34797a | 246.4 µs · 14B · 0a | 1.14 µs · 608B · 17a |
| tail-ping-pong | 29.2 µs · 0B · 0a | 511.2 ns · 0B · 0a | 57.1× | — | 890.5 ns · 0B · 0a | 107.3 µs · 106112B · 2001a | 35.85 µs · 21856B · 85a | 109.6 µs · 13984B · 1726a | 117.2 µs · 14280B · 1731a | 46.34 µs · 2B · 0a | 533.1 µs · 348833B · 9016a |
| tail-sum | 29.25 µs · 0B · 0a | 642.3 ns · 0B · 0a | 45.5× | — | 301.6 ns · 0B · 0a | 108.5 µs · 106112B · 2001a | 35.57 µs · 21856B · 85a | 109.8 µs · 13984B · 1726a | 117.5 µs · 14280B · 1731a | 46.41 µs · 2B · 0a | 30.21 µs · 584B · 16a |
| typed-array-sum | 7.214 µs · 0B · 0a | 227.4 ns · 0B · 0a | 31.7× | 266.9 ns · 8B · 1a | 1.899 ns · 0B · 0a | 46.15 µs · 94208B · 513a | 8.256 µs · 4000B · 15a | 27.11 µs · 2080B · 238a | 17.01 µs · 2496B · 246a | 6.239 µs · 0B · 0a | — |
| xorshift-i64 | 106.6 µs · 18424B · 2046a | 1.41 µs · 18424B · 2046a | 75.6× | — | 361 ns · 0B · 0a | 103.1 µs · 107608B · 2188a | — | 966.4 µs · 447255B · 12736a | 416.9 µs · 199937B · 7772a | 91.73 µs · 5B · 0a | 62.3 µs · 7360B · 547a |

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
