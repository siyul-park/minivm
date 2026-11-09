# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.398 µs · 0B · 0a | 5.342 µs · 0B · 0a | 1.76× | — | 2.244 µs · 1024B · 128a | 38.5 µs · 96288B · 388a | 17.32 µs · 14376B · 256a | 72.12 µs · 78016B · 770a | 14.52 µs · 5712B · 266a | 5.835 µs · 0B · 0a | 24.54 µs · 1744B · 148a |
| binary-trees | 1.982 ms · 768B · 8a | 611.8 µs · 768B · 8a | 3.24× | — | 295.4 µs · 201936B · 8414a | 3.919 ms · 1468184B · 62877a | 10.75 ms · 6995680B · 92703a | 9.104 ms · 5198698B · 69052a | 28.4 ms · 19457612B · 280714a | 2.131 ms · 37B · 0a | 29.4 ms · 15127304B · 446011a |
| branch-tree | 1.294 µs · 0B · 0a | 129.6 ns · 0B · 0a | 9.98× | 624.8 ns · 16B · 1a | 160.7 ns · 0B · 0a | 48.22 µs · 95384B · 660a | 15.19 µs · 2464B · 9a | 26.46 µs · 1992B · 196a | 24 µs · 2168B · 203a | 9.379 µs · 0B · 0a | 14.83 µs · 1252B · 122a |
| closure-counter | 5.53 µs · 64B · 2a | 869.6 ns · 64B · 2a | 6.36× | — | 39.81 ns · 0B · 0a | 42.71 µs · 92272B · 261a | 12.85 µs · 151B · 3a | 18.95 µs · 1264B · 13a | 80.05 µs · 58312B · 659a | 8.577 µs · 0B · 0a | 82.67 µs · 35048B · 793a |
| fannkuch | 824.3 µs · 34608B · 1442a | 265.5 µs · 34608B · 1442a | 3.1× | — | 35.95 µs · 17280B · 720a | 2.011 ms · 456579B · 25621a | 1.556 ms · 443136B · 1460a | 19.86 ms · 14005360B · 164946a | 3.385 ms · 1367677B · 16944a | 617.7 µs · 10B · 0a | 4.614 ms · 2491459B · 85959a |
| fnv1a64 | 122.1 µs · 16400B · 2049a | 1.198 µs · 16400B · 2049a | 102× | — | 902.5 ns · 0B · 0a | 165.5 µs · 120808B · 3838a | — | 1.499 ms · 608251B · 18475a | 403.6 µs · 162921B · 7429a | 138.3 µs · 2B · 0a | 101.8 µs · 9105B · 1050a |
| i64-wide-fib | 28.37 ms · 2913456B · 364178a | 860.5 µs · 16B · 1a | 33× | — | 239.3 µs · 0B · 0a | 25.01 µs · 90520B · 52a | — | 161.4 ms · 64193266B · 1942305a | 354.7 ms · 253472869B · 4248841a | 34.59 ms · 616B · 1a | 129.5 ms · 98686628B · 2138708a |
| indirect-recursive-fib | 1.187 ms · 0B · 0a | 76.3 µs · 0B · 0a | 15.6× | 98.78 µs · 8B · 1a | 35.05 µs · 0B · 0a | 2.08 ms · 319345B · 28655a | 1.933 ms · 704B · 2a | 2.565 ms · 4680B · 39a | 11.72 ms · 10158185B · 109494a | 930.1 µs · 17B · 0a | 24.54 ms · 13061326B · 394061a |
| iterative-fib | 1.1 µs · 0B · 0a | 70.58 ns · 0B · 0a | 15.6× | 85.89 ns · 8B · 1a | 11.89 ns · 0B · 0a | 25.79 µs · 90592B · 61a | 1.152 µs · 160B · 0a | 4.246 µs · 368B · 20a | 6.253 µs · 2448B · 88a | 870.7 ns · 0B · 0a | 4.405 µs · 784B · 50a |
| mandelbrot | 292.4 µs · 0B · 0a | 11.04 µs · 0B · 0a | 26.5× | — | 6.32 µs · 0B · 0a | 968.8 µs · 292345B · 25008a | 485.4 µs · 169408B · 661a | 8.282 ms · 5558652B · 104191a | 1.508 ms · 324978B · 23643a | 367.3 µs · 5B · 0a | 1.271 ms · 324083B · 19107a |
| mat-mul | 367.3 µs · 6216B · 6a | 8.885 µs · 6216B · 6a | 41.3× | — | 5.652 µs · 6144B · 3a | 1.394 ms · 431714B · 34871a | 674.1 µs · 143168B · 478a | 1.212 ms · 61304B · 537a | 1.468 ms · 90712B · 9350a | 371.7 µs · 5B · 0a | 612.6 µs · 16874B · 1134a |
| n-body | 607.7 µs · 504B · 14a | 16.67 µs · 504B · 14a | 36.5× | — | 7.346 µs · 0B · 0a | 1.53 ms · 386898B · 35892a | 1.245 ms · 258913B · 2002a | 3.388 ms · 658436B · 81545a | 2.461 ms · 382715B · 34975a | 499.1 µs · 8B · 0a | 1.631 ms · 400500B · 18248a |
| n-queens | 431.6 µs · 120B · 6a | 32.79 µs · 120B · 6a | 13.2× | — | 6.02 µs · 0B · 0a | 964.2 µs · 234865B · 17784a | 578.8 µs · 14423B · 63a | 1.101 ms · 7424B · 35a | 1.441 ms · 410345B · 5147a | 284.5 µs · 4B · 0a | 1.781 ms · 749455B · 29235a |
| permutation-flips | 128 µs · 7680B · 128a | 18.11 µs · 7680B · 128a | 7.07× | — | 1.875 ns · 0B · 0a | 569.4 µs · 292858B · 9856a | 236.6 µs · 78808B · 451a | 533.2 µs · 122504B · 765a | 498.7 µs · 111976B · 2368a | 81.6 µs · 1B · 0a | 1.001 µs · 528B · 13a |
| recursive-fib-20 | 711.1 µs · 0B · 0a | 60.86 µs · 0B · 0a | 11.7× | 42.73 µs · 8B · 1a | 21.5 µs · 0B · 0a | 1.949 ms · 319345B · 28655a | 2.154 ms · 704B · 2a | 2.961 ms · 4680B · 39a | 11.71 ms · 9807920B · 109494a | 889 µs · 17B · 0a | 10.81 ms · 8302699B · 192848a |
| recursive-fib-35 | 971 ms · 0B · 0a | 82.8 ms · 0B · 0a | 11.7× | 58.22 ms · 8B · 1a | 29.1 ms · 0B · 0a | 2.603 s · 312797344B · 39088168a | 3.012 s · 971008B · 3793a | 4.025 s · 375360B · 46373a | 16.11 s · 13378015952B · 149349976a | 1.204 s · 19088B · 52a | — |
| sieve | 19.89 µs · 1048B · 2a | 961.9 ns · 1048B · 2a | 20.7× | 1.027 µs · 8B · 1a | 350.6 ns · 0B · 0a | 142.3 µs · 122504B · 1611a | 50.87 µs · 18416B · 44a | 84.52 µs · 1872B · 25a | 77.48 µs · 5704B · 30a | 18.9 µs · 0B · 0a | 40.18 µs · 1256B · 43a |
| sort-stress | 504.6 µs · 5136B · 512a | 11.9 µs · 5136B · 512a | 42.4× | — | 5.843 µs · 1024B · 2a | 1.311 ms · 270945B · 19965a | 843.3 µs · 36568B · 365a | 1.789 ms · 49104B · 3621a | 1.798 ms · 23448B · 2034a | 513 µs · 8B · 0a | 872.3 µs · 13122B · 935a |
| spectral-norm | 531.5 µs · 648B · 6a | 34.63 µs · 648B · 6a | 15.3× | — | 7.062 µs · 576B · 3a | 2.096 ms · 571371B · 54822a | 1.089 ms · 234784B · 925a | 2.452 ms · 275337B · 33729a | 4.818 ms · 2457134B · 52718a | 827.2 µs · 15B · 0a | 3.879 ms · 2443906B · 69364a |
| string-build | 722.3 µs · 85408B · 4107a | 584.6 µs · 85408B · 4107a | 1.24× | — | 411.9 µs · 855890B · 5001a | 2.537 ms · 2211273B · 36101a | 2.351 ms · 1078111B · 13859a | 3.153 ms · 1147382B · 26883a | 2.528 ms · 1326241B · 24469a | 551 µs · 8B · 0a | 3.06 ms · 2348550B · 38417a |
| string-compare | 1.079 ms · 21136B · 1284a | 272.9 µs · 21136B · 1284a | 3.95× | — | 64.02 µs · 9632B · 1585a | 1.682 ms · 296282B · 20528a | 1.126 ms · 59712B · 3431a | 2.332 ms · 50744B · 3848a | 1.793 ms · 209521B · 5520a | 449.7 µs · 7B · 0a | 1.451 ms · 369684B · 15139a |
| struct-tree-walk | 283.9 µs · 768B · 8a | 110.3 µs · 768B · 8a | 2.57× | — | 1.874 ns · 0B · 0a | 663.9 µs · 458379B · 5114a | 1.403 ms · 818512B · 11253a | 1.116 ms · 558960B · 6149a | 3.597 ms · 2570662B · 34797a | 245.6 µs · 4B · 0a | 1.144 µs · 608B · 17a |
| tail-ping-pong | 29.14 µs · 0B · 0a | 480.8 ns · 0B · 0a | 60.6× | — | 890.7 ns · 0B · 0a | 112.3 µs · 106112B · 2001a | 36.16 µs · 21856B · 85a | 109.7 µs · 13984B · 1726a | 118.5 µs · 14280B · 1731a | 46.11 µs · 0B · 0a | 563.2 µs · 348835B · 9016a |
| tail-sum | 29.11 µs · 0B · 0a | 621.3 ns · 0B · 0a | 46.9× | — | 301.5 ns · 0B · 0a | 111.8 µs · 106112B · 2001a | 36.12 µs · 21856B · 85a | 109.8 µs · 13984B · 1726a | 119.2 µs · 14280B · 1731a | 46.36 µs · 0B · 0a | 30.01 µs · 584B · 16a |
| typed-array-sum | 7.207 µs · 0B · 0a | 229.8 ns · 0B · 0a | 31.4× | 266.5 ns · 8B · 1a | 1.888 ns · 0B · 0a | 48.37 µs · 94208B · 513a | 8.382 µs · 3999B · 15a | 27.23 µs · 2080B · 238a | 17.41 µs · 2496B · 246a | 6.148 µs · 0B · 0a | — |
| xorshift-i64 | 112.9 µs · 18424B · 2046a | 1.491 µs · 18424B · 2046a | 75.7× | — | 360.1 ns · 0B · 0a | 106.8 µs · 107608B · 2188a | — | 1.013 ms · 447253B · 12735a | 435.1 µs · 199937B · 7772a | 92.02 µs · 1B · 0a | 62.02 µs · 7360B · 547a |

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
