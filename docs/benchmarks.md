# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.874 µs · 0B · 0a | 8.901 µs · 0B · 0a | 0.997× | — | 2.241 µs · 1024B · 128a | 40.07 µs · 96288B · 388a | 17.46 µs · 14376B · 256a | 70.95 µs · 78016B · 770a | 14.44 µs · 5712B · 266a | 5.824 µs · 0B · 0a | 24 µs · 1744B · 148a |
| binary-trees | 1.945 ms · 768B · 8a | 1.211 ms · 768B · 8a | 1.61× | — | 286.2 µs · 201936B · 8414a | 3.859 ms · 1468183B · 62877a | 10.42 ms · 6995685B · 92703a | 8.82 ms · 5198825B · 69052a | 29.06 ms · 19457675B · 280714a | 2.137 ms · 119B · 0a | 29.82 ms · 15127308B · 446011a |
| branch-tree | 1.311 µs · 0B · 0a | 137.4 ns · 0B · 0a | 9.54× | 625.6 ns · 16B · 1a | 162.5 ns · 0B · 0a | 46.07 µs · 95384B · 660a | 15.04 µs · 2463B · 9a | 26.3 µs · 1992B · 196a | 23.9 µs · 2168B · 203a | 9.435 µs · 0B · 0a | 14.94 µs · 1252B · 122a |
| closure-counter | 5.53 µs · 64B · 2a | 941.5 ns · 64B · 2a | 5.87× | — | 39.83 ns · 0B · 0a | 40.99 µs · 92272B · 261a | 12.85 µs · 151B · 3a | 18.89 µs · 1264B · 13a | 77.62 µs · 58312B · 659a | 8.657 µs · 0B · 0a | 86.62 µs · 35049B · 793a |
| fannkuch | 820.8 µs · 34608B · 1442a | 267.6 µs · 34608B · 1442a | 3.07× | — | 35.01 µs · 17280B · 720a | 2.03 ms · 456578B · 25621a | 1.522 ms · 443136B · 1460a | 18.67 ms · 14005344B · 164946a | 3.316 ms · 1367678B · 16944a | 617.8 µs · 34B · 0a | 4.698 ms · 2491474B · 85959a |
| fnv1a64 | 123.3 µs · 16400B · 2049a | 1.194 µs · 16400B · 2049a | 103× | — | 902.4 ns · 0B · 0a | 160.9 µs · 120808B · 3838a | — | 1.444 ms · 608254B · 18475a | 392.4 µs · 162921B · 7429a | 136.9 µs · 8B · 0a | 102.3 µs · 9105B · 1050a |
| i64-wide-fib | 27.85 ms · 2913456B · 364178a | 860.3 µs · 16B · 1a | 32.4× | — | 239.4 µs · 0B · 0a | 23.33 µs · 90520B · 52a | — | 157.4 ms · 64193248B · 1942305a | 340.3 ms · 253473024B · 4248842a | 34.66 ms · 2388B · 6a | 124.5 ms · 98686560B · 2138708a |
| indirect-recursive-fib | 1.189 ms · 0B · 0a | 76.54 µs · 0B · 0a | 15.5× | 100.3 µs · 8B · 1a | 34.85 µs · 0B · 0a | 2.1 ms · 319346B · 28655a | 1.92 ms · 703B · 2a | 2.461 ms · 4680B · 39a | 11.01 ms · 10158195B · 109494a | 925.5 µs · 51B · 0a | 25.99 ms · 13061331B · 394061a |
| iterative-fib | 1.106 µs · 0B · 0a | 73.27 ns · 0B · 0a | 15.1× | 86.72 ns · 8B · 1a | 11.9 ns · 0B · 0a | 23.7 µs · 90592B · 61a | 1.143 µs · 160B · 0a | 4.284 µs · 368B · 20a | 6.09 µs · 2448B · 88a | 851.1 ns · 0B · 0a | 4.52 µs · 784B · 50a |
| mandelbrot | 299.7 µs · 0B · 0a | 11.09 µs · 0B · 0a | 27× | — | 6.33 µs · 0B · 0a | 961.3 µs · 292345B · 25008a | 477.4 µs · 169408B · 661a | 7.956 ms · 5558652B · 104191a | 1.503 ms · 324978B · 23643a | 366.3 µs · 20B · 0a | 1.315 ms · 324083B · 19107a |
| mat-mul | 367.6 µs · 6216B · 6a | 8.697 µs · 6216B · 6a | 42.3× | — | 5.581 µs · 6144B · 3a | 1.375 ms · 431714B · 34871a | 660.1 µs · 143168B · 478a | 1.202 ms · 61304B · 537a | 1.474 ms · 90712B · 9350a | 374.6 µs · 21B · 0a | 609.7 µs · 16874B · 1134a |
| n-body | 609.1 µs · 504B · 14a | 19.27 µs · 504B · 14a | 31.6× | — | 7.347 µs · 0B · 0a | 1.505 ms · 386898B · 35892a | 1.207 ms · 258913B · 2002a | 3.404 ms · 658435B · 81545a | 2.458 ms · 382714B · 34975a | 499.9 µs · 29B · 0a | 1.645 ms · 400496B · 18248a |
| n-queens | 429.7 µs · 120B · 6a | 32.9 µs · 120B · 6a | 13.1× | — | 6.027 µs · 0B · 0a | 967.6 µs · 234865B · 17784a | 566.7 µs · 14423B · 63a | 1.095 ms · 7424B · 35a | 1.439 ms · 410345B · 5147a | 285.6 µs · 16B · 0a | 1.786 ms · 749456B · 29235a |
| permutation-flips | 129 µs · 7680B · 128a | 22.72 µs · 7680B · 128a | 5.68× | — | 1.874 ns · 0B · 0a | 556.5 µs · 292858B · 9856a | 232.2 µs · 78808B · 451a | 523.3 µs · 122504B · 765a | 491.4 µs · 111976B · 2368a | 81.54 µs · 5B · 0a | 1.029 µs · 528B · 13a |
| recursive-fib-20 | 708 µs · 0B · 0a | 60.95 µs · 0B · 0a | 11.6× | 42.73 µs · 8B · 1a | 21.53 µs · 0B · 0a | 1.967 ms · 319345B · 28655a | 2.163 ms · 704B · 2a | 2.964 ms · 4680B · 39a | 11.22 ms · 9807913B · 109494a | 890.2 µs · 49B · 0a | 11.01 ms · 8302711B · 192848a |
| recursive-fib-35 | 965.6 ms · 0B · 0a | 82.79 ms · 0B · 0a | 11.7× | 58.2 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.639 s · 312797392B · 39088168a | 2.945 s · 971008B · 3793a | 4.028 s · 375360B · 46373a | 15.4 s · 13378016272B · 149349970a | 1.203 s · 19088B · 52a | — |
| sieve | 19.82 µs · 1048B · 2a | 936.3 ns · 1048B · 2a | 21.2× | 1.041 µs · 8B · 1a | 350.7 ns · 0B · 0a | 137.9 µs · 122504B · 1611a | 50.43 µs · 18416B · 44a | 83.87 µs · 1872B · 25a | 77.52 µs · 5704B · 30a | 18.9 µs · 1B · 0a | 40.57 µs · 1256B · 43a |
| sort-stress | 505.4 µs · 5136B · 512a | 12.48 µs · 5136B · 512a | 40.5× | — | 5.865 µs · 1024B · 2a | 1.296 ms · 270945B · 19965a | 842.4 µs · 36567B · 365a | 1.789 ms · 49104B · 3621a | 1.787 ms · 23448B · 2034a | 512.9 µs · 28B · 0a | 882.9 µs · 13121B · 935a |
| spectral-norm | 532.1 µs · 648B · 6a | 34.85 µs · 648B · 6a | 15.3× | — | 7.097 µs · 576B · 3a | 2.075 ms · 571370B · 54822a | 1.078 ms · 234784B · 925a | 2.482 ms · 275338B · 33729a | 4.625 ms · 2457132B · 52718a | 825.3 µs · 46B · 0a | 3.844 ms · 2443893B · 69364a |
| string-build | 736.6 µs · 85408B · 4107a | 562.7 µs · 85408B · 4107a | 1.31× | — | 382 µs · 855889B · 5001a | 2.365 ms · 2211274B · 36101a | 2.344 ms · 1078111B · 13859a | 3.13 ms · 1147383B · 26883a | 2.419 ms · 1326241B · 24469a | 553.8 µs · 30B · 0a | 2.96 ms · 2348557B · 38417a |
| struct-tree-walk | 281.6 µs · 768B · 8a | 182.2 µs · 768B · 8a | 1.55× | — | 1.876 ns · 0B · 0a | 644.1 µs · 458378B · 5114a | 1.345 ms · 818511B · 11253a | 1.066 ms · 558960B · 6149a | 3.416 ms · 2570666B · 34797a | 244.7 µs · 14B · 0a | 1.208 µs · 608B · 17a |
| tail-ping-pong | 29.32 µs · 0B · 0a | 511.4 ns · 0B · 0a | 57.3× | — | 890.9 ns · 0B · 0a | 106.5 µs · 106112B · 2001a | 35.15 µs · 21856B · 85a | 113.4 µs · 13984B · 1726a | 118.2 µs · 14280B · 1731a | 46.43 µs · 3B · 0a | 550.9 µs · 348832B · 9016a |
| tail-sum | 29.26 µs · 0B · 0a | 645.5 ns · 0B · 0a | 45.3× | — | 302.4 ns · 0B · 0a | 107.6 µs · 106112B · 2001a | 35.69 µs · 21856B · 85a | 113.2 µs · 13984B · 1726a | 119.1 µs · 14280B · 1731a | 46.35 µs · 3B · 0a | 30.25 µs · 584B · 16a |
| typed-array-sum | 7.252 µs · 0B · 0a | 228 ns · 0B · 0a | 31.8× | 266.9 ns · 8B · 1a | 1.875 ns · 0B · 0a | 43.96 µs · 94208B · 513a | 8.191 µs · 4000B · 15a | 27.68 µs · 2080B · 238a | 17.32 µs · 2496B · 246a | 6.13 µs · 0B · 0a | — |
| xorshift-i64 | 111.8 µs · 18424B · 2046a | 1.442 µs · 18424B · 2046a | 77.5× | — | 360.4 ns · 0B · 0a | 102.1 µs · 107608B · 2188a | — | 982.3 µs · 447248B · 12735a | 430.8 µs · 199938B · 7772a | 91.72 µs · 5B · 0a | 62.75 µs · 7360B · 547a |

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
