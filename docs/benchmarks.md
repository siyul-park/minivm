# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.869 µs · 0B · 0a | 8.892 µs · 0B · 0a | 0.997× | — | 2.231 µs · 1024B · 128a | 36.63 µs · 96288B · 388a | 16.84 µs · 14375B · 256a | 69.03 µs · 78016B · 770a | 14.27 µs · 5712B · 266a | 5.818 µs · 0B · 0a | 24.43 µs · 1744B · 148a |
| binary-trees | 1.947 ms · 768B · 8a | 1.217 ms · 768B · 8a | 1.6× | — | 295.4 µs · 201936B · 8414a | 3.918 ms · 1468184B · 62877a | 10.57 ms · 6995680B · 92703a | 8.862 ms · 5198707B · 69052a | 27.7 ms · 19457585B · 280714a | 2.133 ms · 119B · 0a | 30.7 ms · 15127304B · 446011a |
| branch-tree | 1.292 µs · 0B · 0a | 135.6 ns · 0B · 0a | 9.53× | 626.8 ns · 16B · 1a | 160.6 ns · 0B · 0a | 45.7 µs · 95384B · 660a | 15.13 µs · 2463B · 9a | 26.4 µs · 1992B · 196a | 24.14 µs · 2168B · 203a | 9.44 µs · 0B · 0a | 15.14 µs · 1252B · 122a |
| closure-counter | 5.516 µs · 64B · 2a | 947.6 ns · 64B · 2a | 5.82× | — | 39.83 ns · 0B · 0a | 40.89 µs · 92272B · 261a | 13 µs · 151B · 3a | 19.26 µs · 1264B · 13a | 77.61 µs · 58312B · 659a | 8.578 µs · 0B · 0a | 87.78 µs · 35048B · 793a |
| fannkuch | 826.4 µs · 34608B · 1442a | 273.1 µs · 34608B · 1442a | 3.03× | — | 35.27 µs · 17280B · 720a | 2.07 ms · 456579B · 25621a | 1.529 ms · 443135B · 1460a | 19.29 ms · 14005333B · 164946a | 3.348 ms · 1367678B · 16944a | 618.2 µs · 34B · 0a | 4.67 ms · 2491459B · 85959a |
| fnv1a64 | 123.4 µs · 16400B · 2049a | 1.198 µs · 16400B · 2049a | 103× | — | 902.8 ns · 0B · 0a | 160.9 µs · 120808B · 3838a | — | 1.528 ms · 608256B · 18475a | 403 µs · 162921B · 7429a | 137.5 µs · 8B · 0a | 102.5 µs · 9105B · 1050a |
| i64-wide-fib | 28.5 ms · 2913440B · 364178a | 860.8 µs · 16B · 1a | 33.1× | — | 239.2 µs · 0B · 0a | 23.7 µs · 90520B · 52a | — | 163.6 ms · 64193280B · 1942305a | 338.7 ms · 253473264B · 4248845a | 34.6 ms · 2388B · 6a | 124.3 ms · 98686613B · 2138708a |
| indirect-recursive-fib | 1.19 ms · 0B · 0a | 76.56 µs · 0B · 0a | 15.5× | 98.99 µs · 8B · 1a | 34.77 µs · 0B · 0a | 2.141 ms · 319345B · 28655a | 1.921 ms · 703B · 2a | 2.563 ms · 4680B · 39a | 11.46 ms · 10158210B · 109494a | 930.5 µs · 52B · 0a | 25.68 ms · 13061345B · 394061a |
| iterative-fib | 1.098 µs · 0B · 0a | 70.29 ns · 0B · 0a | 15.6× | 86.16 ns · 8B · 1a | 11.89 ns · 0B · 0a | 23.42 µs · 90592B · 61a | 1.151 µs · 159B · 0a | 4.22 µs · 368B · 20a | 6.124 µs · 2448B · 88a | 852.5 ns · 0B · 0a | 4.445 µs · 784B · 50a |
| mandelbrot | 291.7 µs · 0B · 0a | 11.09 µs · 0B · 0a | 26.3× | — | 6.329 µs · 0B · 0a | 971.7 µs · 292345B · 25008a | 484 µs · 169408B · 661a | 7.929 ms · 5558657B · 104191a | 1.517 ms · 324978B · 23643a | 367.7 µs · 20B · 0a | 1.303 ms · 324080B · 19107a |
| mat-mul | 369.8 µs · 6216B · 6a | 8.482 µs · 6216B · 6a | 43.6× | — | 5.381 µs · 6144B · 3a | 1.385 ms · 431715B · 34871a | 662.6 µs · 143168B · 478a | 1.212 ms · 61304B · 537a | 1.459 ms · 90712B · 9350a | 370.8 µs · 21B · 0a | 616.9 µs · 16876B · 1134a |
| n-body | 608.9 µs · 504B · 14a | 19 µs · 504B · 14a | 32× | — | 7.343 µs · 0B · 0a | 1.539 ms · 386898B · 35892a | 1.193 ms · 258913B · 2002a | 3.407 ms · 658435B · 81545a | 2.452 ms · 382714B · 34975a | 498.7 µs · 28B · 0a | 1.611 ms · 400506B · 18248a |
| n-queens | 428.2 µs · 120B · 6a | 32.94 µs · 120B · 6a | 13× | — | 6.006 µs · 0B · 0a | 970.4 µs · 234865B · 17784a | 563.8 µs · 14424B · 63a | 1.102 ms · 7424B · 35a | 1.446 ms · 410345B · 5147a | 284.8 µs · 16B · 0a | 1.782 ms · 749461B · 29235a |
| permutation-flips | 129 µs · 7680B · 128a | 22.76 µs · 7680B · 128a | 5.67× | — | 1.888 ns · 0B · 0a | 558.8 µs · 292858B · 9856a | 231.3 µs · 78808B · 451a | 525.7 µs · 122504B · 765a | 498.9 µs · 111976B · 2368a | 81.26 µs · 4B · 0a | 1.042 µs · 528B · 13a |
| recursive-fib-20 | 708.7 µs · 0B · 0a | 60.91 µs · 0B · 0a | 11.6× | 42.74 µs · 8B · 1a | 21.67 µs · 0B · 0a | 2.007 ms · 319345B · 28655a | 2.17 ms · 704B · 2a | 2.967 ms · 4680B · 39a | 11.32 ms · 9807921B · 109494a | 883.6 µs · 50B · 0a | 10.77 ms · 8302685B · 192847a |
| recursive-fib-35 | 964.4 ms · 0B · 0a | 82.82 ms · 0B · 0a | 11.6× | 58.19 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.682 s · 312797728B · 39088169a | 2.952 s · 971008B · 3793a | 4.034 s · 375360B · 46373a | 15.27 s · 13378018208B · 149349974a | 1.229 s · 19088B · 52a | — |
| sieve | 19.98 µs · 1048B · 2a | 896.7 ns · 1048B · 2a | 22.3× | 1.041 µs · 8B · 1a | 351 ns · 0B · 0a | 136.7 µs · 122504B · 1611a | 49.94 µs · 18416B · 44a | 84.35 µs · 1872B · 25a | 76.65 µs · 5704B · 30a | 18.94 µs · 1B · 0a | 41.07 µs · 1256B · 43a |
| sort-stress | 503 µs · 5136B · 512a | 12.46 µs · 5136B · 512a | 40.4× | — | 5.794 µs · 1024B · 2a | 1.299 ms · 270945B · 19965a | 840.5 µs · 36568B · 365a | 1.791 ms · 49104B · 3621a | 1.803 ms · 23448B · 2034a | 511.9 µs · 28B · 0a | 879.7 µs · 13121B · 935a |
| spectral-norm | 528.4 µs · 648B · 6a | 34.81 µs · 648B · 6a | 15.2× | — | 7.066 µs · 576B · 3a | 2.083 ms · 571372B · 54822a | 1.072 ms · 234784B · 925a | 2.488 ms · 275337B · 33729a | 4.665 ms · 2457135B · 52718a | 827.4 µs · 46B · 0a | 3.892 ms · 2443901B · 69364a |
| string-build | 736.1 µs · 85408B · 4107a | 612.8 µs · 85408B · 4107a | 1.2× | — | 361.1 µs · 855891B · 5001a | 2.365 ms · 2211272B · 36101a | 2.284 ms · 1078112B · 13859a | 3.114 ms · 1147384B · 26883a | 2.434 ms · 1326246B · 24469a | 549.3 µs · 30B · 0a | 2.944 ms · 2348565B · 38417a |
| struct-tree-walk | 282.6 µs · 768B · 8a | 183 µs · 768B · 8a | 1.54× | — | 1.887 ns · 0B · 0a | 632.1 µs · 458379B · 5114a | 1.353 ms · 818513B · 11253a | 1.075 ms · 558961B · 6149a | 3.478 ms · 2570664B · 34797a | 245 µs · 14B · 0a | 1.2 µs · 608B · 17a |
| tail-ping-pong | 29.17 µs · 0B · 0a | 510.3 ns · 0B · 0a | 57.2× | — | 890.7 ns · 0B · 0a | 105.8 µs · 106112B · 2001a | 34.91 µs · 21856B · 85a | 113.8 µs · 13984B · 1726a | 118 µs · 14280B · 1731a | 46.32 µs · 3B · 0a | 554.4 µs · 348833B · 9016a |
| tail-sum | 29.1 µs · 0B · 0a | 651.4 ns · 0B · 0a | 44.7× | — | 302.9 ns · 0B · 0a | 107.1 µs · 106112B · 2001a | 35.73 µs · 21856B · 85a | 112.9 µs · 13984B · 1726a | 117.9 µs · 14280B · 1731a | 46.11 µs · 3B · 0a | 30.11 µs · 584B · 16a |
| typed-array-sum | 7.245 µs · 0B · 0a | 227.9 ns · 0B · 0a | 31.8× | 267.5 ns · 8B · 1a | 1.897 ns · 0B · 0a | 43.62 µs · 94208B · 513a | 8.172 µs · 4000B · 15a | 27.72 µs · 2080B · 238a | 17.51 µs · 2496B · 246a | 6.129 µs · 0B · 0a | — |
| xorshift-i64 | 111.2 µs · 18424B · 2046a | 1.373 µs · 18424B · 2046a | 81× | — | 360.3 ns · 0B · 0a | 102 µs · 107608B · 2188a | — | 988.6 µs · 447251B · 12735a | 427 µs · 199937B · 7772a | 92.13 µs · 5B · 0a | 63.16 µs · 7361B · 547a |

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
