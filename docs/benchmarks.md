# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.887 µs · 0B · 0a | 8.885 µs · 0B · 0a | 1× | — | 2.22 µs · 1024B · 128a | 37.85 µs · 96289B · 388a | 18.08 µs · 14376B · 256a | 72.61 µs · 78016B · 770a | 14.8 µs · 5712B · 266a | 5.818 µs · 0B · 0a | 24.01 µs · 1744B · 148a |
| binary-trees | 1.965 ms · 768B · 8a | 1.225 ms · 768B · 8a | 1.6× | — | 295.8 µs · 201936B · 8414a | 3.974 ms · 1468187B · 62877a | 10.76 ms · 6995680B · 92703a | 9.16 ms · 5198698B · 69052a | 28.1 ms · 19457629B · 280714a | 2.137 ms · 120B · 0a | 29.94 ms · 15127310B · 446011a |
| branch-tree | 1.313 µs · 0B · 0a | 133.4 ns · 0B · 0a | 9.84× | 624.8 ns · 16B · 1a | 160.5 ns · 0B · 0a | 46.68 µs · 95384B · 660a | 15.3 µs · 2464B · 9a | 26.83 µs · 1992B · 196a | 24.67 µs · 2168B · 203a | 9.434 µs · 0B · 0a | 14.9 µs · 1252B · 122a |
| closure-counter | 5.524 µs · 64B · 2a | 942.8 ns · 64B · 2a | 5.86× | — | 39.85 ns · 0B · 0a | 40.41 µs · 92272B · 261a | 12.76 µs · 151B · 3a | 19.4 µs · 1264B · 13a | 78.47 µs · 58312B · 659a | 8.571 µs · 0B · 0a | 86.03 µs · 35049B · 793a |
| fannkuch | 824.2 µs · 34608B · 1442a | 268.3 µs · 34608B · 1442a | 3.07× | — | 35.52 µs · 17280B · 720a | 2.026 ms · 456580B · 25621a | 1.552 ms · 443136B · 1460a | 19.61 ms · 14005357B · 164946a | 3.383 ms · 1367677B · 16944a | 616.8 µs · 34B · 0a | 4.652 ms · 2491466B · 85959a |
| fnv1a64 | 118.6 µs · 16400B · 2049a | 1.198 µs · 16400B · 2049a | 99× | — | 903.2 ns · 0B · 0a | 161.5 µs · 120808B · 3838a | — | 1.463 ms · 608249B · 18475a | 392.3 µs · 162921B · 7429a | 136.5 µs · 8B · 0a | 102.1 µs · 9105B · 1050a |
| i64-wide-fib | 28 ms · 2913456B · 364178a | 861.3 µs · 16B · 1a | 32.5× | — | 239 µs · 0B · 0a | 23.48 µs · 90520B · 52a | — | 156 ms · 64193328B · 1942306a | 346.1 ms · 253472784B · 4248842a | 34.76 ms · 2510B · 6a | 129.9 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 1.189 ms · 0B · 0a | 76.43 µs · 0B · 0a | 15.6× | 99.05 µs · 8B · 1a | 35.09 µs · 0B · 0a | 2.093 ms · 319345B · 28655a | 1.941 ms · 704B · 2a | 2.547 ms · 4680B · 39a | 11.64 ms · 10158200B · 109494a | 927.9 µs · 52B · 0a | 25.44 ms · 13061316B · 394061a |
| iterative-fib | 1.106 µs · 0B · 0a | 71.28 ns · 0B · 0a | 15.5× | 84.94 ns · 8B · 1a | 11.89 ns · 0B · 0a | 25.43 µs · 90592B · 61a | 1.145 µs · 160B · 0a | 4.246 µs · 368B · 20a | 6.135 µs · 2448B · 88a | 872.5 ns · 0B · 0a | 4.49 µs · 784B · 50a |
| mandelbrot | 292 µs · 0B · 0a | 11.1 µs · 0B · 0a | 26.3× | — | 6.327 µs · 0B · 0a | 945.1 µs · 292345B · 25008a | 483 µs · 169408B · 661a | 8.323 ms · 5558649B · 104191a | 1.52 ms · 324977B · 23643a | 367.7 µs · 21B · 0a | 1.274 ms · 324077B · 19107a |
| mat-mul | 367.4 µs · 6216B · 6a | 8.732 µs · 6216B · 6a | 42.1× | — | 5.563 µs · 6144B · 3a | 1.408 ms · 431714B · 34871a | 673 µs · 143168B · 478a | 1.203 ms · 61304B · 537a | 1.468 ms · 90713B · 9350a | 371.1 µs · 21B · 0a | 614.4 µs · 16875B · 1134a |
| n-body | 610.1 µs · 504B · 14a | 18.96 µs · 504B · 14a | 32.2× | — | 7.349 µs · 0B · 0a | 1.549 ms · 386898B · 35892a | 1.21 ms · 258914B · 2002a | 3.408 ms · 658438B · 81545a | 2.454 ms · 382714B · 34975a | 499.5 µs · 29B · 0a | 1.613 ms · 400502B · 18248a |
| n-queens | 429.5 µs · 120B · 6a | 32.92 µs · 120B · 6a | 13× | — | 6.015 µs · 0B · 0a | 974.8 µs · 234865B · 17784a | 570 µs · 14423B · 63a | 1.097 ms · 7424B · 35a | 1.429 ms · 410345B · 5147a | 283.7 µs · 16B · 0a | 1.778 ms · 749462B · 29235a |
| permutation-flips | 128.3 µs · 7680B · 128a | 22.92 µs · 7680B · 128a | 5.6× | — | 1.891 ns · 0B · 0a | 565.3 µs · 292857B · 9856a | 238 µs · 78808B · 451a | 533.1 µs · 122504B · 765a | 497.9 µs · 111976B · 2368a | 81.49 µs · 5B · 0a | 1.014 µs · 528B · 13a |
| recursive-fib-20 | 710.9 µs · 0B · 0a | 60.89 µs · 0B · 0a | 11.7× | 42.77 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.957 ms · 319346B · 28655a | 2.167 ms · 704B · 2a | 2.972 ms · 4680B · 39a | 11.81 ms · 9807924B · 109494a | 898.8 µs · 49B · 0a | 10.6 ms · 8302703B · 192848a |
| recursive-fib-35 | 976.3 ms · 0B · 0a | 82.78 ms · 0B · 0a | 11.8× | 58.23 ms · 8B · 1a | 29.15 ms · 0B · 0a | 2.561 s · 312797328B · 39088167a | 2.943 s · 971008B · 3793a | 4.039 s · 375360B · 46373a | 15.97 s · 13378019272B · 149349942a | 1.202 s · 19200B · 53a | — |
| sieve | 19.77 µs · 1048B · 2a | 931.1 ns · 1048B · 2a | 21.2× | 1.038 µs · 8B · 1a | 351 ns · 0B · 0a | 137.8 µs · 122504B · 1611a | 50.92 µs · 18416B · 44a | 84.22 µs · 1872B · 25a | 76.77 µs · 5704B · 30a | 18.94 µs · 1B · 0a | 40.46 µs · 1256B · 43a |
| sort-stress | 505.7 µs · 5136B · 512a | 12.47 µs · 5136B · 512a | 40.5× | — | 5.818 µs · 1024B · 2a | 1.267 ms · 270945B · 19965a | 851.9 µs · 36567B · 365a | 1.792 ms · 49104B · 3621a | 1.78 ms · 23448B · 2034a | 514.1 µs · 29B · 0a | 875.8 µs · 13122B · 935a |
| spectral-norm | 533.4 µs · 648B · 6a | 34.77 µs · 648B · 6a | 15.3× | — | 7.036 µs · 576B · 3a | 2.078 ms · 571372B · 54822a | 1.092 ms · 234784B · 925a | 2.444 ms · 275337B · 33729a | 4.66 ms · 2457136B · 52718a | 827.7 µs · 47B · 0a | 3.797 ms · 2443915B · 69364a |
| string-build | 717.7 µs · 85408B · 4107a | 560.5 µs · 85408B · 4107a | 1.28× | — | 378.4 µs · 855891B · 5001a | 2.432 ms · 2211278B · 36101a | 2.33 ms · 1078110B · 13859a | 3.122 ms · 1147383B · 26883a | 2.471 ms · 1326242B · 24469a | 549.6 µs · 31B · 0a | 2.967 ms · 2348530B · 38417a |
| struct-tree-walk | 282.1 µs · 768B · 8a | 184.7 µs · 768B · 8a | 1.53× | — | 1.89 ns · 0B · 0a | 656.8 µs · 458379B · 5114a | 1.366 ms · 818513B · 11253a | 1.087 ms · 558961B · 6149a | 3.545 ms · 2570668B · 34797a | 244.3 µs · 14B · 0a | 1.165 µs · 608B · 17a |
| tail-ping-pong | 29.3 µs · 0B · 0a | 516.6 ns · 0B · 0a | 56.7× | — | 890.9 ns · 0B · 0a | 107.5 µs · 106112B · 2001a | 35.91 µs · 21856B · 85a | 108.8 µs · 13984B · 1726a | 118.6 µs · 14280B · 1731a | 46.38 µs · 2B · 0a | 540.3 µs · 348834B · 9016a |
| tail-sum | 29.44 µs · 0B · 0a | 651.3 ns · 0B · 0a | 45.2× | — | 301.5 ns · 0B · 0a | 107.6 µs · 106112B · 2001a | 36 µs · 21855B · 85a | 109.6 µs · 13984B · 1726a | 118.3 µs · 14280B · 1731a | 46.14 µs · 2B · 0a | 30.64 µs · 584B · 16a |
| typed-array-sum | 7.431 µs · 0B · 0a | 235.7 ns · 0B · 0a | 31.5× | 266.5 ns · 8B · 1a | 1.9 ns · 0B · 0a | 45.4 µs · 94208B · 513a | 8.285 µs · 4000B · 15a | 27.1 µs · 2080B · 238a | 17.31 µs · 2496B · 246a | 6.137 µs · 0B · 0a | — |
| xorshift-i64 | 110.2 µs · 18424B · 2046a | 1.428 µs · 18424B · 2046a | 77.1× | — | 361.3 ns · 0B · 0a | 103.6 µs · 107608B · 2188a | — | 983.4 µs · 447253B · 12735a | 429 µs · 199937B · 7772a | 91.38 µs · 5B · 0a | 62.35 µs · 7361B · 547a |

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
