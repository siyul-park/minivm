# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.855 µs · 0B · 0a | 8.858 µs · 0B · 0a | 1× | — | 2.111 µs · 1024B · 128a | 40.31 µs · 96289B · 388a | 17.7 µs · 14376B · 256a | 72.23 µs · 78016B · 770a | 13.6 µs · 5712B · 266a | 10.42 µs · 20048B · 54a | 23.89 µs · 1744B · 148a |
| binary-trees | 1.957 ms · 768B · 8a | 650.3 µs · 768B · 8a | 3.01× | — | 281.5 µs · 201936B · 8414a | 3.744 ms · 1468183B · 62877a | 10.48 ms · 6995689B · 92703a | 8.801 ms · 5198708B · 69052a | 27.34 ms · 19457619B · 280714a | 2.136 ms · 123B · 0a | 28.11 ms · 15127321B · 446011a |
| branch-tree | 1.318 µs · 0B · 0a | 139 ns · 0B · 0a | 9.48× | 624.8 ns · 16B · 1a | 160.7 ns · 0B · 0a | 47.53 µs · 95384B · 660a | 15.16 µs · 2464B · 9a | 26.76 µs · 1992B · 196a | 24.81 µs · 2168B · 203a | 9.45 µs · 0B · 0a | 14.83 µs · 1252B · 122a |
| closure-counter | 5.555 µs · 64B · 2a | 943.7 ns · 64B · 2a | 5.89× | — | 39.83 ns · 0B · 0a | 42.86 µs · 92272B · 261a | 12.42 µs · 151B · 3a | 19.36 µs · 1264B · 13a | 80.64 µs · 58312B · 659a | 8.629 µs · 0B · 0a | 80.81 µs · 35048B · 793a |
| fannkuch | 838 µs · 34608B · 1442a | 260.8 µs · 34608B · 1442a | 3.21× | — | 35.09 µs · 17280B · 720a | 2.005 ms · 456579B · 25621a | 1.549 ms · 443136B · 1460a | 19.02 ms · 14005343B · 164946a | 3.293 ms · 1367677B · 16944a | 617.8 µs · 37B · 0a | 4.516 ms · 2491472B · 85959a |
| fnv1a64 | 118.4 µs · 16400B · 2049a | 1.201 µs · 16400B · 2049a | 98.6× | — | 902.5 ns · 0B · 0a | 162.5 µs · 120808B · 3838a | — | 1.383 ms · 608257B · 18475a | 368.5 µs · 162921B · 7429a | 138.5 µs · 8B · 0a | 102 µs · 9105B · 1050a |
| i64-wide-fib | 27.47 ms · 2913456B · 364178a | 860.4 µs · 16B · 1a | 31.9× | — | 239.1 µs · 0B · 0a | 24.1 µs · 90520B · 52a | — | 151.7 ms · 64193464B · 1942307a | 345.7 ms · 253473040B · 4248844a | 35.07 ms · 2508B · 6a | 116.3 ms · 98686661B · 2138708a |
| indirect-recursive-fib | 1.199 ms · 0B · 0a | 76.25 µs · 0B · 0a | 15.7× | 98.8 µs · 8B · 1a | 35.04 µs · 0B · 0a | 2.07 ms · 319345B · 28655a | 1.929 ms · 704B · 2a | 2.558 ms · 4680B · 39a | 11.78 ms · 10158191B · 109494a | 924.5 µs · 52B · 0a | 24.16 ms · 13061308B · 394061a |
| iterative-fib | 1.105 µs · 0B · 0a | 75.97 ns · 0B · 0a | 14.5× | 84.39 ns · 8B · 1a | 11.9 ns · 0B · 0a | 25.41 µs · 90592B · 61a | 1.169 µs · 160B · 0a | 4.244 µs · 368B · 20a | 5.911 µs · 2448B · 88a | 871 ns · 0B · 0a | 4.442 µs · 784B · 50a |
| mandelbrot | 291.7 µs · 0B · 0a | 11.09 µs · 0B · 0a | 26.3× | — | 6.324 µs · 0B · 0a | 924.8 µs · 292345B · 25008a | 484.8 µs · 169408B · 661a | 8.032 ms · 5558648B · 104191a | 1.455 ms · 324978B · 23643a | 366.7 µs · 21B · 0a | 1.256 ms · 324081B · 19107a |
| mat-mul | 367.6 µs · 6216B · 6a | 8.77 µs · 6216B · 6a | 41.9× | — | 5.48 µs · 6144B · 3a | 1.332 ms · 431716B · 34871a | 673.3 µs · 143168B · 478a | 1.218 ms · 61304B · 537a | 1.454 ms · 90712B · 9350a | 371.7 µs · 21B · 0a | 621.5 µs · 16875B · 1134a |
| n-body | 609.7 µs · 504B · 14a | 19.05 µs · 504B · 14a | 32× | — | 7.345 µs · 0B · 0a | 1.49 ms · 386898B · 35892a | 1.213 ms · 258913B · 2002a | 3.252 ms · 658436B · 81545a | 2.382 ms · 382715B · 34975a | 498.9 µs · 28B · 0a | 1.592 ms · 400498B · 18248a |
| n-queens | 429.3 µs · 120B · 6a | 32.88 µs · 120B · 6a | 13.1× | — | 6.023 µs · 0B · 0a | 950.3 µs · 234865B · 17784a | 569.2 µs · 14423B · 63a | 1.1 ms · 7424B · 35a | 1.392 ms · 410345B · 5147a | 284.5 µs · 16B · 0a | 1.761 ms · 749458B · 29235a |
| permutation-flips | 128.3 µs · 7680B · 128a | 22.11 µs · 7680B · 128a | 5.81× | — | 1.877 ns · 0B · 0a | 561.6 µs · 292858B · 9856a | 231.2 µs · 78808B · 451a | 521 µs · 122504B · 765a | 484.6 µs · 111976B · 2368a | 81.64 µs · 5B · 0a | 946.4 ns · 528B · 13a |
| recursive-fib-20 | 709.2 µs · 0B · 0a | 60.91 µs · 0B · 0a | 11.6× | 42.73 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.923 ms · 319345B · 28655a | 2.254 ms · 703B · 2a | 2.979 ms · 4680B · 39a | 11.73 ms · 9807922B · 109494a | 884.6 µs · 52B · 0a | 10.09 ms · 8302673B · 192847a |
| recursive-fib-35 | 965.1 ms · 0B · 0a | 82.86 ms · 0B · 0a | 11.6× | 58.2 ms · 8B · 1a | 29.06 ms · 0B · 0a | 2.563 s · 312797184B · 39088167a | 3.031 s · 971008B · 3793a | 4.043 s · 375360B · 46373a | 15.8 s · 13378020720B · 149349992a | 1.207 s · 19568B · 53a | — |
| sieve | 19.84 µs · 1048B · 2a | 930 ns · 1048B · 2a | 21.3× | 1.043 µs · 8B · 1a | 350.8 ns · 0B · 0a | 138.4 µs · 122504B · 1611a | 50.72 µs · 18416B · 44a | 84.35 µs · 1872B · 25a | 76.49 µs · 5704B · 30a | 18.86 µs · 1B · 0a | 40.08 µs · 1256B · 43a |
| sort-stress | 505 µs · 5136B · 512a | 12.52 µs · 5136B · 512a | 40.3× | — | 5.84 µs · 1024B · 2a | 1.278 ms · 270946B · 19965a | 842.7 µs · 36567B · 365a | 1.791 ms · 49104B · 3621a | 1.781 ms · 23448B · 2034a | 511.2 µs · 29B · 0a | 872.8 µs · 13121B · 935a |
| spectral-norm | 534.2 µs · 648B · 6a | 34.79 µs · 648B · 6a | 15.4× | — | 7.064 µs · 576B · 3a | 2.004 ms · 571371B · 54822a | 1.078 ms · 234784B · 925a | 2.475 ms · 275337B · 33729a | 4.679 ms · 2457138B · 52718a | 826.8 µs · 49B · 0a | 3.702 ms · 2443899B · 69364a |
| string-build | 717.8 µs · 85408B · 4107a | 556.7 µs · 85408B · 4107a | 1.29× | — | 377.1 µs · 855890B · 5001a | 2.52 ms · 2211276B · 36101a | 2.351 ms · 1078111B · 13859a | 3.141 ms · 1147384B · 26883a | 2.499 ms · 1326242B · 24469a | 552.7 µs · 31B · 0a | 2.922 ms · 2348545B · 38417a |
| struct-tree-walk | 281.8 µs · 768B · 8a | 179 µs · 768B · 8a | 1.57× | — | 1.877 ns · 0B · 0a | 669.6 µs · 458379B · 5114a | 1.346 ms · 818512B · 11253a | 1.096 ms · 558961B · 6149a | 3.552 ms · 2570661B · 34797a | 247.3 µs · 14B · 0a | 1.102 µs · 608B · 17a |
| tail-ping-pong | 29.56 µs · 0B · 0a | 516.8 ns · 0B · 0a | 57.2× | — | 890.7 ns · 0B · 0a | 109.2 µs · 106112B · 2001a | 36.19 µs · 21855B · 85a | 110 µs · 13984B · 1726a | 115.4 µs · 14280B · 1731a | 46.39 µs · 2B · 0a | 516.9 µs · 348831B · 9016a |
| tail-sum | 29.49 µs · 0B · 0a | 648.5 ns · 0B · 0a | 45.5× | — | 301.4 ns · 0B · 0a | 108.8 µs · 106112B · 2001a | 36.38 µs · 21856B · 85a | 110 µs · 13984B · 1726a | 115.4 µs · 14280B · 1731a | 46.39 µs · 2B · 0a | 30.04 µs · 584B · 16a |
| typed-array-sum | 7.229 µs · 0B · 0a | 236.3 ns · 0B · 0a | 30.6× | 266.4 ns · 8B · 1a | 1.876 ns · 0B · 0a | 46.17 µs · 94208B · 513a | 8.409 µs · 4000B · 15a | 27.01 µs · 2080B · 238a | 17.08 µs · 2496B · 246a | 6.208 µs · 0B · 0a | — |
| xorshift-i64 | 107.1 µs · 18424B · 2046a | 1.441 µs · 18424B · 2046a | 74.3× | — | 360.2 ns · 0B · 0a | 104.6 µs · 107608B · 2188a | — | 926.6 µs · 447250B · 12735a | 416.2 µs · 199938B · 7772a | 91.98 µs · 5B · 0a | 62.47 µs · 7361B · 547a |

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
