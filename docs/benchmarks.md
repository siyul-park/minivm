# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.775 µs · 0B · 0a | 5.351 µs · 0B · 0a | 1.64× | — | 2.223 µs · 1024B · 128a | 38.32 µs · 96288B · 388a | 19.23 µs · 14376B · 256a | 79.84 µs · 78016B · 770a | 14.74 µs · 5712B · 266a | — | 23.98 µs · 1744B · 148a |
| binary-trees | 1.964 ms · 768B · 8a | 605.4 µs · 768B · 8a | 3.24× | — | 284.6 µs · 201936B · 8414a | 3.909 ms · 1468186B · 62877a | 10.55 ms · 6995683B · 92703a | 8.985 ms · 5198711B · 69052a | 27.89 ms · 19457668B · 280714a | — | 28.69 ms · 15127296B · 446011a |
| branch-tree | 1.268 µs · 0B · 0a | 139.4 ns · 0B · 0a | 9.1× | 626.9 ns · 16B · 1a | 160.8 ns · 0B · 0a | 47.9 µs · 95384B · 660a | 15.16 µs · 2464B · 9a | 26.35 µs · 1992B · 196a | 23.94 µs · 2168B · 203a | — | 14.96 µs · 1252B · 122a |
| closure-counter | 5.486 µs · 64B · 2a | 947.1 ns · 64B · 2a | 5.79× | — | 39.82 ns · 0B · 0a | 40.29 µs · 92272B · 261a | 12.56 µs · 151B · 3a | 19.09 µs · 1264B · 13a | 77.25 µs · 58312B · 659a | — | 82.58 µs · 35048B · 793a |
| fannkuch | 823.6 µs · 34608B · 1442a | 267.1 µs · 34608B · 1442a | 3.08× | — | 35.06 µs · 17280B · 720a | 2.034 ms · 456578B · 25621a | 1.529 ms · 443136B · 1460a | 18.97 ms · 14005341B · 164946a | 3.334 ms · 1367676B · 16944a | — | 4.543 ms · 2491464B · 85959a |
| fnv1a64 | 122 µs · 16400B · 2049a | 1.206 µs · 16400B · 2049a | 101× | — | 902.9 ns · 0B · 0a | 161.1 µs · 120808B · 3838a | — | 1.443 ms · 608257B · 18475a | 392 µs · 162921B · 7429a | — | 102 µs · 9105B · 1050a |
| i64-wide-fib | 28.26 ms · 2913456B · 364178a | 825.7 µs · 16B · 1a | 34.2× | — | 239.1 µs · 0B · 0a | 23.2 µs · 90520B · 52a | — | 156.4 ms · 64193296B · 1942306a | 342.6 ms · 253473072B · 4248843a | — | 117.9 ms · 98686608B · 2138708a |
| indirect-recursive-fib | 1.184 ms · 0B · 0a | 71.28 µs · 0B · 0a | 16.6× | 98.85 µs · 8B · 1a | 34.77 µs · 0B · 0a | 2.061 ms · 319345B · 28655a | 1.922 ms · 704B · 2a | 2.519 ms · 4680B · 39a | 11.15 ms · 10158191B · 109494a | — | 24.01 ms · 13061316B · 394061a |
| iterative-fib | 1.101 µs · 0B · 0a | 71.65 ns · 0B · 0a | 15.4× | 85.06 ns · 8B · 1a | 11.89 ns · 0B · 0a | 24.8 µs · 90592B · 61a | 1.152 µs · 160B · 0a | 4.238 µs · 368B · 20a | 5.996 µs · 2448B · 88a | — | 4.437 µs · 784B · 50a |
| mandelbrot | 290.2 µs · 0B · 0a | 11.1 µs · 0B · 0a | 26.1× | — | 6.329 µs · 0B · 0a | 930.2 µs · 292345B · 25008a | 479.5 µs · 169408B · 661a | 8.016 ms · 5558647B · 104191a | 1.462 ms · 324977B · 23643a | — | 1.263 ms · 324081B · 19107a |
| mat-mul | 366.7 µs · 6216B · 6a | 8.581 µs · 6216B · 6a | 42.7× | — | 5.519 µs · 6144B · 3a | 1.346 ms · 431714B · 34871a | 661.2 µs · 143168B · 478a | 1.203 ms · 61304B · 537a | 1.455 ms · 90712B · 9350a | — | 610.8 µs · 16874B · 1134a |
| n-body | 609.6 µs · 504B · 14a | 18.83 µs · 504B · 14a | 32.4× | — | 7.345 µs · 0B · 0a | 1.457 ms · 386898B · 35892a | 1.215 ms · 258913B · 2002a | 3.215 ms · 658437B · 81545a | 2.414 ms · 382714B · 34975a | — | 1.59 ms · 400499B · 18248a |
| n-queens | 428.8 µs · 120B · 6a | 32.25 µs · 120B · 6a | 13.3× | — | 6.025 µs · 0B · 0a | 961.7 µs · 234865B · 17784a | 564.8 µs · 14423B · 63a | 1.098 ms · 7424B · 35a | 1.422 ms · 410347B · 5147a | — | 1.75 ms · 749461B · 29235a |
| permutation-flips | 127.7 µs · 7680B · 128a | 17.56 µs · 7680B · 128a | 7.27× | — | 1.874 ns · 0B · 0a | 554 µs · 292858B · 9856a | 230.5 µs · 78808B · 451a | 524.7 µs · 122504B · 765a | 483.8 µs · 111976B · 2368a | — | 950.6 ns · 528B · 13a |
| recursive-fib-20 | 711.9 µs · 0B · 0a | 58.42 µs · 0B · 0a | 12.2× | 42.74 µs · 8B · 1a | 21.59 µs · 0B · 0a | 1.944 ms · 319345B · 28655a | 2.152 ms · 704B · 2a | 2.997 ms · 4680B · 39a | 11.24 ms · 9807927B · 109494a | — | 9.985 ms · 8302650B · 192847a |
| recursive-fib-35 | 970.2 ms · 0B · 0a | 79.47 ms · 0B · 0a | 12.2× | 58.18 ms · 8B · 1a | 29.07 ms · 0B · 0a | 2.569 s · 312797312B · 39088169a | 2.939 s · 971008B · 3793a | 4.076 s · 375360B · 46373a | 15.3 s · 13378018816B · 149349980a | — | — |
| sieve | 19.78 µs · 1048B · 2a | 919.6 ns · 1048B · 2a | 21.5× | 1.038 µs · 8B · 1a | 350.5 ns · 0B · 0a | 137.4 µs · 122504B · 1611a | 50.29 µs · 18416B · 44a | 83.46 µs · 1872B · 25a | 77.16 µs · 5704B · 30a | — | 40.56 µs · 1256B · 43a |
| sort-stress | 505.7 µs · 5136B · 512a | 12.63 µs · 5136B · 512a | 40× | — | 5.817 µs · 1024B · 2a | 1.298 ms · 270945B · 19965a | 839.7 µs · 36568B · 365a | 1.784 ms · 49104B · 3621a | 1.782 ms · 23448B · 2034a | — | 879.7 µs · 13121B · 935a |
| spectral-norm | 529 µs · 648B · 6a | 34.78 µs · 648B · 6a | 15.2× | — | 7.039 µs · 576B · 3a | 2.003 ms · 571371B · 54822a | 1.089 ms · 234783B · 925a | 2.427 ms · 275337B · 33729a | 4.613 ms · 2457135B · 52718a | — | 3.688 ms · 2443905B · 69364a |
| string-build | 717.2 µs · 85408B · 4107a | 613 µs · 85408B · 4107a | 1.17× | — | 363.2 µs · 855890B · 5001a | 2.401 ms · 2211273B · 36101a | 2.298 ms · 1078114B · 13859a | 3.144 ms · 1147384B · 26883a | 2.453 ms · 1326242B · 24469a | — | 2.947 ms · 2348547B · 38417a |
| string-compare | 1.08 ms · 21136B · 1284a | 268.6 µs · 21136B · 1284a | 4.02× | — | 63 µs · 9632B · 1585a | 1.665 ms · 296281B · 20528a | 1.119 ms · 59712B · 3431a | 2.31 ms · 50744B · 3848a | 1.829 ms · 209521B · 5520a | — | 1.438 ms · 369682B · 15139a |
| struct-tree-walk | 284 µs · 768B · 8a | 110.1 µs · 768B · 8a | 2.58× | — | 1.875 ns · 0B · 0a | 646.9 µs · 458379B · 5114a | 1.366 ms · 818512B · 11253a | 1.09 ms · 558962B · 6149a | 3.489 ms · 2570664B · 34797a | — | 1.106 µs · 608B · 17a |
| tail-ping-pong | 29.24 µs · 0B · 0a | 520.4 ns · 0B · 0a | 56.2× | — | 891.3 ns · 0B · 0a | 108.3 µs · 106112B · 2001a | 35.55 µs · 21856B · 85a | 112.2 µs · 13984B · 1726a | 117 µs · 14280B · 1731a | — | 529.7 µs · 348834B · 9016a |
| tail-sum | 29.09 µs · 0B · 0a | 653.5 ns · 0B · 0a | 44.5× | — | 301.9 ns · 0B · 0a | 108.4 µs · 106112B · 2001a | 35.84 µs · 21856B · 85a | 112.1 µs · 13984B · 1726a | 116.1 µs · 14280B · 1731a | — | 30.05 µs · 584B · 16a |
| typed-array-sum | 7.215 µs · 0B · 0a | 241.6 ns · 0B · 0a | 29.9× | 268.3 ns · 8B · 1a | 1.888 ns · 0B · 0a | 44.9 µs · 94208B · 513a | 8.2 µs · 3999B · 15a | 27.4 µs · 2080B · 238a | 17.02 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.2 µs · 18424B · 2046a | 1.392 µs · 18424B · 2046a | 79.2× | — | 360.3 ns · 0B · 0a | 104.1 µs · 107608B · 2188a | — | 955.9 µs · 447250B · 12735a | 417.4 µs · 199937B · 7772a | — | 62.25 µs · 7360B · 547a |

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
