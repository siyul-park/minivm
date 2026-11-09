# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.178 µs · 0B · 0a | 5.353 µs · 0B · 0a | 1.71× | — | 2.195 µs · 1024B · 128a | 39.05 µs · 96288B · 388a | 17.77 µs · 14376B · 256a | 71.96 µs · 78016B · 770a | 14.34 µs · 5712B · 266a | 9.151 µs · 19568B · 53a | 24.52 µs · 1744B · 148a |
| binary-trees | 1.971 ms · 768B · 8a | 610.4 µs · 768B · 8a | 3.23× | — | 294.6 µs · 201936B · 8414a | 3.871 ms · 1468183B · 62877a | 10.99 ms · 6995693B · 92703a | 9.006 ms · 5198694B · 69052a | 27.42 ms · 19457574B · 280714a | 2.139 ms · 120B · 0a | 28.34 ms · 15127309B · 446011a |
| branch-tree | 1.313 µs · 0B · 0a | 138.1 ns · 0B · 0a | 9.51× | 626 ns · 16B · 1a | 160.4 ns · 0B · 0a | 48.12 µs · 95385B · 660a | 15.23 µs · 2463B · 9a | 26.5 µs · 1992B · 196a | 24.22 µs · 2168B · 203a | 9.496 µs · 0B · 0a | 15.13 µs · 1252B · 122a |
| closure-counter | 5.526 µs · 64B · 2a | 948.6 ns · 64B · 2a | 5.83× | — | 39.82 ns · 0B · 0a | 43.67 µs · 92272B · 261a | 12.84 µs · 151B · 3a | 19.09 µs · 1264B · 13a | 76.67 µs · 58312B · 659a | 8.629 µs · 0B · 0a | 81.42 µs · 35049B · 793a |
| fannkuch | 826.6 µs · 34608B · 1442a | 264.9 µs · 34608B · 1442a | 3.12× | — | 35.38 µs · 17280B · 720a | 2.006 ms · 456578B · 25621a | 1.547 ms · 443136B · 1460a | 19.1 ms · 14005355B · 164946a | 3.331 ms · 1367677B · 16944a | 618.2 µs · 34B · 0a | 4.605 ms · 2491470B · 85959a |
| fnv1a64 | 120.9 µs · 16400B · 2049a | 1.201 µs · 16400B · 2049a | 101× | — | 902.9 ns · 0B · 0a | 167.1 µs · 120808B · 3838a | — | 1.486 ms · 608248B · 18475a | 402.6 µs · 162921B · 7429a | 138.5 µs · 8B · 0a | 102.1 µs · 9105B · 1050a |
| i64-wide-fib | 28.2 ms · 2913456B · 364178a | 860.9 µs · 16B · 1a | 32.8× | — | 239.4 µs · 0B · 0a | 23.74 µs · 90520B · 52a | — | 160.8 ms · 64193256B · 1942305a | 353.7 ms · 253472992B · 4248843a | 34.54 ms · 2388B · 6a | 120.6 ms · 98686592B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 76.37 µs · 0B · 0a | 15.5× | 98.57 µs · 8B · 1a | 35.03 µs · 0B · 0a | 2.059 ms · 319345B · 28655a | 1.927 ms · 704B · 2a | 2.566 ms · 4680B · 39a | 11.72 ms · 10158188B · 109494a | 931 µs · 52B · 0a | 23.9 ms · 13061320B · 394061a |
| iterative-fib | 1.102 µs · 0B · 0a | 75.65 ns · 0B · 0a | 14.6× | 84.76 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.5 µs · 90592B · 61a | 1.142 µs · 160B · 0a | 4.218 µs · 368B · 20a | 6.047 µs · 2448B · 88a | 852.2 ns · 0B · 0a | 4.468 µs · 784B · 50a |
| mandelbrot | 289.9 µs · 0B · 0a | 11.09 µs · 0B · 0a | 26.1× | — | 6.329 µs · 0B · 0a | 956.1 µs · 292345B · 25008a | 477.6 µs · 169408B · 661a | 8.159 ms · 5558650B · 104191a | 1.496 ms · 324979B · 23643a | 367.6 µs · 20B · 0a | 1.265 ms · 324082B · 19107a |
| mat-mul | 367.9 µs · 6216B · 6a | 8.69 µs · 6216B · 6a | 42.3× | — | 5.507 µs · 6144B · 3a | 1.349 ms · 431714B · 34871a | 664.8 µs · 143168B · 478a | 1.211 ms · 61304B · 537a | 1.465 ms · 90712B · 9350a | 371.2 µs · 21B · 0a | 614.6 µs · 16875B · 1134a |
| n-body | 607.7 µs · 504B · 14a | 19.1 µs · 504B · 14a | 31.8× | — | 7.346 µs · 0B · 0a | 1.505 ms · 386898B · 35892a | 1.214 ms · 258913B · 2002a | 3.242 ms · 658437B · 81545a | 2.401 ms · 382715B · 34975a | 498.9 µs · 29B · 0a | 1.589 ms · 400505B · 18248a |
| n-queens | 431.1 µs · 120B · 6a | 32.96 µs · 120B · 6a | 13.1× | — | 6.012 µs · 0B · 0a | 948.2 µs · 234865B · 17784a | 565.5 µs · 14423B · 63a | 1.103 ms · 7424B · 35a | 1.441 ms · 410345B · 5147a | 285.2 µs · 17B · 0a | 1.76 ms · 749459B · 29235a |
| permutation-flips | 127.7 µs · 7680B · 128a | 17.54 µs · 7680B · 128a | 7.28× | — | 1.874 ns · 0B · 0a | 564.2 µs · 292857B · 9856a | 234.5 µs · 78808B · 451a | 529.7 µs · 122504B · 765a | 488.1 µs · 111976B · 2368a | 81.5 µs · 5B · 0a | 985.6 ns · 528B · 13a |
| recursive-fib-20 | 711.4 µs · 0B · 0a | 60.91 µs · 0B · 0a | 11.7× | 42.75 µs · 8B · 1a | 21.47 µs · 0B · 0a | 1.929 ms · 319345B · 28655a | 2.208 ms · 704B · 2a | 2.967 ms · 4680B · 39a | 11.76 ms · 9807916B · 109494a | 886.8 µs · 50B · 0a | 10.06 ms · 8302669B · 192847a |
| recursive-fib-35 | 969.9 ms · 0B · 0a | 82.81 ms · 0B · 0a | 11.7× | 58.22 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.565 s · 312797424B · 39088168a | 3.014 s · 971008B · 3793a | 4.024 s · 375360B · 46373a | 15.71 s · 13378018528B · 149349973a | 1.209 s · 19088B · 52a | — |
| sieve | 19.92 µs · 1048B · 2a | 930 ns · 1048B · 2a | 21.4× | 1.036 µs · 8B · 1a | 350.7 ns · 0B · 0a | 137.9 µs · 122504B · 1611a | 50.54 µs · 18416B · 44a | 84.57 µs · 1872B · 25a | 77.12 µs · 5704B · 30a | 18.86 µs · 1B · 0a | 40.34 µs · 1256B · 43a |
| sort-stress | 504.6 µs · 5136B · 512a | 12.57 µs · 5136B · 512a | 40.1× | — | 5.804 µs · 1024B · 2a | 1.276 ms · 270945B · 19965a | 841.6 µs · 36567B · 365a | 1.79 ms · 49104B · 3621a | 1.799 ms · 23448B · 2034a | 512.8 µs · 29B · 0a | 873.8 µs · 13121B · 935a |
| spectral-norm | 533.1 µs · 648B · 6a | 34.84 µs · 648B · 6a | 15.3× | — | 7.038 µs · 576B · 3a | 2.044 ms · 571372B · 54822a | 1.073 ms · 234784B · 925a | 2.416 ms · 275337B · 33729a | 4.749 ms · 2457139B · 52718a | 824.7 µs · 46B · 0a | 3.736 ms · 2443915B · 69364a |
| string-build | 717.6 µs · 85408B · 4107a | 577.7 µs · 85408B · 4107a | 1.24× | — | 376.4 µs · 855894B · 5001a | 2.449 ms · 2211279B · 36101a | 2.363 ms · 1078109B · 13859a | 3.101 ms · 1147384B · 26883a | 2.459 ms · 1326241B · 24469a | 552.9 µs · 31B · 0a | 2.941 ms · 2348527B · 38417a |
| string-compare | 1.08 ms · 21136B · 1284a | 272.9 µs · 21136B · 1284a | 3.96× | — | 62.9 µs · 9632B · 1585a | 1.684 ms · 296282B · 20528a | 1.131 ms · 59712B · 3431a | 2.333 ms · 50744B · 3848a | 1.776 ms · 209521B · 5520a | 446.4 µs · 25B · 0a | 1.439 ms · 369679B · 15139a |
| struct-tree-walk | 284.2 µs · 768B · 8a | 110.5 µs · 768B · 8a | 2.57× | — | 1.876 ns · 0B · 0a | 656 µs · 458379B · 5114a | 1.37 ms · 818514B · 11253a | 1.099 ms · 558961B · 6149a | 3.566 ms · 2570669B · 34797a | 244.8 µs · 14B · 0a | 1.111 µs · 608B · 17a |
| tail-ping-pong | 29.08 µs · 0B · 0a | 519.2 ns · 0B · 0a | 56× | — | 890.8 ns · 0B · 0a | 108.4 µs · 106112B · 2001a | 35.34 µs · 21855B · 85a | 108.2 µs · 13984B · 1726a | 117.4 µs · 14280B · 1731a | 46.42 µs · 2B · 0a | 528.7 µs · 348833B · 9016a |
| tail-sum | 29.16 µs · 0B · 0a | 644.7 ns · 0B · 0a | 45.2× | — | 301.6 ns · 0B · 0a | 108.8 µs · 106112B · 2001a | 35.63 µs · 21855B · 85a | 107.7 µs · 13984B · 1726a | 117.4 µs · 14280B · 1731a | 46.4 µs · 2B · 0a | 29.94 µs · 584B · 16a |
| typed-array-sum | 7.168 µs · 0B · 0a | 236.6 ns · 0B · 0a | 30.3× | 266.4 ns · 8B · 1a | 1.887 ns · 0B · 0a | 47.05 µs · 94208B · 513a | 8.271 µs · 4000B · 15a | 27.14 µs · 2080B · 238a | 17.22 µs · 2496B · 246a | 6.221 µs · 0B · 0a | — |
| xorshift-i64 | 110.1 µs · 18424B · 2046a | 1.44 µs · 18424B · 2046a | 76.5× | — | 360.7 ns · 0B · 0a | 103.1 µs · 107608B · 2188a | — | 988.8 µs · 447255B · 12736a | 421.8 µs · 199938B · 7772a | 92.23 µs · 5B · 0a | 62.03 µs · 7360B · 547a |

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
