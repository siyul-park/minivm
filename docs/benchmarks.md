# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.123 µs · 0B · 0a | 5.514 µs · 0B · 0a | 1.65× | — | 2.167 µs · 1024B · 128a | 44.61 µs · 96289B · 388a | 20.66 µs · 14376B · 256a | 77.92 µs · 78016B · 770a | 14.06 µs · 5712B · 266a | 5.814 µs · 0B · 0a | 24.06 µs · 1744B · 148a |
| binary-trees | 1.959 ms · 768B · 8a | 606 µs · 768B · 8a | 3.23× | — | 285.5 µs · 201936B · 8414a | 3.708 ms · 1468186B · 62877a | 10.68 ms · 6995671B · 92703a | 8.838 ms · 5198708B · 69052a | 28.04 ms · 19457622B · 280714a | 2.135 ms · 120B · 0a | 28.07 ms · 15127368B · 446011a |
| branch-tree | 1.299 µs · 0B · 0a | 141 ns · 0B · 0a | 9.21× | 627 ns · 16B · 1a | 160.6 ns · 0B · 0a | 46.93 µs · 95385B · 660a | 15.23 µs · 2464B · 9a | 26.42 µs · 1992B · 196a | 23.92 µs · 2168B · 203a | 9.53 µs · 0B · 0a | 14.99 µs · 1252B · 122a |
| closure-counter | 5.523 µs · 64B · 2a | 941.5 ns · 64B · 2a | 5.87× | — | 39.83 ns · 0B · 0a | 41.57 µs · 92272B · 261a | 12.85 µs · 151B · 3a | 19.23 µs · 1264B · 13a | 78.39 µs · 58312B · 659a | 8.656 µs · 0B · 0a | 83.65 µs · 35048B · 793a |
| fannkuch | 842.9 µs · 34608B · 1442a | 265.4 µs · 34608B · 1442a | 3.18× | — | 35.34 µs · 17280B · 720a | 1.999 ms · 456579B · 25621a | 1.548 ms · 443137B · 1460a | 19.52 ms · 14005339B · 164946a | 3.31 ms · 1367679B · 16944a | 621.1 µs · 35B · 0a | 4.534 ms · 2491461B · 85959a |
| fnv1a64 | 121 µs · 16400B · 2049a | 1.205 µs · 16400B · 2049a | 100× | — | 903.1 ns · 0B · 0a | 162.7 µs · 120808B · 3838a | — | 1.413 ms · 608252B · 18475a | 374.8 µs · 162921B · 7429a | 139.5 µs · 8B · 0a | 101.6 µs · 9105B · 1050a |
| i64-wide-fib | 27.94 ms · 2913456B · 364178a | 859.9 µs · 16B · 1a | 32.5× | — | 239.2 µs · 0B · 0a | 25.33 µs · 90520B · 52a | — | 156.3 ms · 64193192B · 1942305a | 356.6 ms · 253472640B · 4248839a | 34.59 ms · 2388B · 6a | 118.7 ms · 98686570B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 76.24 µs · 0B · 0a | 15.6× | 98.83 µs · 8B · 1a | 34.77 µs · 0B · 0a | 2.076 ms · 319346B · 28655a | 1.934 ms · 704B · 2a | 2.559 ms · 4680B · 39a | 11.78 ms · 10158178B · 109494a | 930.5 µs · 52B · 0a | 24.08 ms · 13061301B · 394061a |
| iterative-fib | 1.105 µs · 0B · 0a | 71.34 ns · 0B · 0a | 15.5× | 85.36 ns · 8B · 1a | 11.91 ns · 0B · 0a | 24.61 µs · 90592B · 61a | 1.148 µs · 160B · 0a | 4.201 µs · 368B · 20a | 5.854 µs · 2448B · 88a | 859.8 ns · 0B · 0a | 4.428 µs · 784B · 50a |
| mandelbrot | 290.7 µs · 0B · 0a | 11.09 µs · 0B · 0a | 26.2× | — | 6.325 µs · 0B · 0a | 940.6 µs · 292345B · 25008a | 481.6 µs · 169407B · 661a | 8.253 ms · 5558648B · 104191a | 1.456 ms · 324978B · 23643a | 366.8 µs · 21B · 0a | 1.268 ms · 324081B · 19107a |
| mat-mul | 367.9 µs · 6216B · 6a | 8.767 µs · 6216B · 6a | 42× | — | 5.559 µs · 6144B · 3a | 1.329 ms · 431714B · 34871a | 663.8 µs · 143168B · 478a | 1.208 ms · 61304B · 537a | 1.458 ms · 90712B · 9350a | 371.5 µs · 21B · 0a | 614.8 µs · 16873B · 1134a |
| n-body | 607.4 µs · 504B · 14a | 18.93 µs · 504B · 14a | 32.1× | — | 7.344 µs · 0B · 0a | 1.463 ms · 386898B · 35892a | 1.215 ms · 258913B · 2002a | 3.212 ms · 658435B · 81545a | 2.402 ms · 382714B · 34975a | 499.8 µs · 29B · 0a | 1.615 ms · 400502B · 18248a |
| n-queens | 429.6 µs · 120B · 6a | 32.92 µs · 120B · 6a | 13.1× | — | 6.015 µs · 0B · 0a | 931.3 µs · 234865B · 17784a | 570.2 µs · 14423B · 63a | 1.099 ms · 7424B · 35a | 1.384 ms · 410345B · 5147a | 284.4 µs · 16B · 0a | 1.759 ms · 749463B · 29235a |
| permutation-flips | 127.7 µs · 7680B · 128a | 17.27 µs · 7680B · 128a | 7.39× | — | 1.889 ns · 0B · 0a | 560.4 µs · 292857B · 9856a | 234.7 µs · 78808B · 451a | 531.4 µs · 122504B · 765a | 485.4 µs · 111976B · 2368a | 81.61 µs · 5B · 0a | 953.4 ns · 528B · 13a |
| recursive-fib-20 | 711 µs · 0B · 0a | 60.91 µs · 0B · 0a | 11.7× | 42.75 µs · 8B · 1a | 21.5 µs · 0B · 0a | 1.926 ms · 319345B · 28655a | 2.166 ms · 704B · 2a | 2.969 ms · 4680B · 39a | 11.58 ms · 9807915B · 109494a | 891.6 µs · 49B · 0a | 10.06 ms · 8302677B · 192847a |
| recursive-fib-35 | 971.3 ms · 0B · 0a | 82.8 ms · 0B · 0a | 11.7× | 58.23 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.578 s · 312797520B · 39088168a | 2.958 s · 971008B · 3793a | 4.034 s · 375360B · 46373a | 15.79 s · 13378018128B · 149349974a | 1.203 s · 20048B · 54a | — |
| sieve | 19.92 µs · 1048B · 2a | 930.6 ns · 1048B · 2a | 21.4× | 1.05 µs · 8B · 1a | 350.6 ns · 0B · 0a | 140.7 µs · 122504B · 1611a | 50.66 µs · 18416B · 44a | 84.56 µs · 1872B · 25a | 77.08 µs · 5704B · 30a | 18.93 µs · 1B · 0a | 40.61 µs · 1256B · 43a |
| sort-stress | 502.6 µs · 5136B · 512a | 12.61 µs · 5136B · 512a | 39.8× | — | 5.805 µs · 1024B · 2a | 1.265 ms · 270945B · 19965a | 842.4 µs · 36567B · 365a | 1.79 ms · 49104B · 3621a | 1.797 ms · 23448B · 2034a | 514.7 µs · 29B · 0a | 876.8 µs · 13122B · 935a |
| spectral-norm | 529.3 µs · 648B · 6a | 34.71 µs · 648B · 6a | 15.3× | — | 7.038 µs · 576B · 3a | 2.04 ms · 571371B · 54822a | 1.074 ms · 234784B · 925a | 2.429 ms · 275337B · 33729a | 4.745 ms · 2457134B · 52718a | 828.9 µs · 46B · 0a | 3.723 ms · 2443919B · 69364a |
| string-build | 718.1 µs · 85408B · 4107a | 573 µs · 85408B · 4107a | 1.25× | — | 372 µs · 855890B · 5001a | 2.447 ms · 2211278B · 36101a | 2.317 ms · 1078112B · 13859a | 3.128 ms · 1147384B · 26883a | 2.466 ms · 1326243B · 24469a | 551.6 µs · 31B · 0a | 2.946 ms · 2348531B · 38417a |
| string-compare | 1.084 ms · 21136B · 1284a | 1.055 ms · 21136B · 1284a | 1.03× | — | 62.08 µs · 9632B · 1585a | 1.68 ms · 296281B · 20528a | 1.127 ms · 59712B · 3431a | 2.329 ms · 50744B · 3848a | 1.796 ms · 209521B · 5520a | 445.7 µs · 25B · 0a | 1.439 ms · 369683B · 15139a |
| struct-tree-walk | 282.7 µs · 768B · 8a | 110.1 µs · 768B · 8a | 2.57× | — | 1.875 ns · 0B · 0a | 650.8 µs · 458379B · 5114a | 1.386 ms · 818512B · 11253a | 1.11 ms · 558961B · 6149a | 3.565 ms · 2570668B · 34797a | 244.8 µs · 14B · 0a | 1.131 µs · 608B · 17a |
| tail-ping-pong | 29.02 µs · 0B · 0a | 516.7 ns · 0B · 0a | 56.2× | — | 891.1 ns · 0B · 0a | 107.6 µs · 106112B · 2001a | 35.62 µs · 21856B · 85a | 110.1 µs · 13984B · 1726a | 116 µs · 14280B · 1731a | 46.36 µs · 2B · 0a | 536.6 µs · 348833B · 9016a |
| tail-sum | 28.94 µs · 0B · 0a | 646.7 ns · 0B · 0a | 44.8× | — | 302.1 ns · 0B · 0a | 108.7 µs · 106112B · 2001a | 35.58 µs · 21856B · 85a | 110.4 µs · 13984B · 1726a | 117.5 µs · 14280B · 1731a | 46.09 µs · 2B · 0a | 30.11 µs · 584B · 16a |
| typed-array-sum | 7.229 µs · 0B · 0a | 236 ns · 0B · 0a | 30.6× | 267.9 ns · 8B · 1a | 1.875 ns · 0B · 0a | 47.89 µs · 94208B · 513a | 8.332 µs · 4000B · 15a | 27.16 µs · 2080B · 238a | 17.16 µs · 2496B · 246a | 6.202 µs · 0B · 0a | — |
| xorshift-i64 | 110 µs · 18424B · 2046a | 1.425 µs · 18424B · 2046a | 77.2× | — | 360.3 ns · 0B · 0a | 104.2 µs · 107608B · 2188a | — | 989.5 µs · 447248B · 12735a | 426.7 µs · 199938B · 7772a | 91.61 µs · 5B · 0a | 62.1 µs · 7361B · 547a |

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
