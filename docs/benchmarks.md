# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.79 µs · 0B · 0a | 8.774 µs · 0B · 0a | 1× | — | 2.235 µs · 1024B · 128a | 39.28 µs · 96289B · 388a | 17.81 µs · 14375B · 256a | 73.53 µs · 78016B · 770a | 14.15 µs · 5712B · 266a | 9.895 µs · 19824B · 54a | 23.95 µs · 1744B · 148a |
| binary-trees | 1.961 ms · 768B · 8a | 1.23 ms · 768B · 8a | 1.59× | — | 310 µs · 201936B · 8414a | 4.056 ms · 1468187B · 62877a | 10.99 ms · 6995666B · 92703a | 9.326 ms · 5198702B · 69052a | 28.89 ms · 19457604B · 280714a | 2.141 ms · 123B · 0a | 28.8 ms · 15127317B · 446011a |
| branch-tree | 1.307 µs · 0B · 0a | 137.4 ns · 0B · 0a | 9.51× | 627.3 ns · 16B · 1a | 160.7 ns · 0B · 0a | 49.45 µs · 95384B · 660a | 15.65 µs · 2463B · 9a | 26.58 µs · 1992B · 196a | 24.12 µs · 2168B · 203a | 9.412 µs · 0B · 0a | 14.93 µs · 1252B · 122a |
| closure-counter | 5.481 µs · 64B · 2a | 945.5 ns · 64B · 2a | 5.8× | — | 39.85 ns · 0B · 0a | 42.61 µs · 92272B · 261a | 12.95 µs · 151B · 3a | 19.19 µs · 1264B · 13a | 81.87 µs · 58312B · 659a | 8.659 µs · 0B · 0a | 81.9 µs · 35048B · 793a |
| fannkuch | 822.6 µs · 34608B · 1442a | 264.1 µs · 34608B · 1442a | 3.11× | — | 35.51 µs · 17280B · 720a | 2.072 ms · 456578B · 25621a | 1.546 ms · 443136B · 1460a | 19.65 ms · 14005326B · 164946a | 3.392 ms · 1367677B · 16944a | 620.9 µs · 35B · 0a | 4.71 ms · 2491474B · 85959a |
| fnv1a64 | 120.8 µs · 16400B · 2049a | 1.205 µs · 16400B · 2049a | 100× | — | 904.1 ns · 0B · 0a | 163.5 µs · 120808B · 3838a | — | 1.497 ms · 608255B · 18475a | 399.3 µs · 162921B · 7429a | 138 µs · 8B · 0a | 101.6 µs · 9105B · 1050a |
| i64-wide-fib | 27.82 ms · 2913456B · 364178a | 860.5 µs · 16B · 1a | 32.3× | — | 238.6 µs · 0B · 0a | 24.49 µs · 90520B · 52a | — | 160.3 ms · 64193312B · 1942305a | 372.6 ms · 253472944B · 4248842a | 34.64 ms · 2388B · 6a | 123.6 ms · 98686586B · 2138708a |
| indirect-recursive-fib | 1.185 ms · 0B · 0a | 76.39 µs · 0B · 0a | 15.5× | 99.16 µs · 8B · 1a | 35.21 µs · 0B · 0a | 2.077 ms · 319345B · 28655a | 1.923 ms · 703B · 2a | 2.561 ms · 4680B · 39a | 11.6 ms · 10158180B · 109494a | 928.7 µs · 52B · 0a | 24.42 ms · 13061273B · 394061a |
| iterative-fib | 1.102 µs · 0B · 0a | 71 ns · 0B · 0a | 15.5× | 85.45 ns · 8B · 1a | 11.89 ns · 0B · 0a | 25.1 µs · 90592B · 61a | 1.164 µs · 160B · 0a | 4.241 µs · 368B · 20a | 6.139 µs · 2448B · 88a | 856.5 ns · 0B · 0a | 4.469 µs · 784B · 50a |
| mandelbrot | 291.8 µs · 0B · 0a | 11.05 µs · 0B · 0a | 26.4× | — | 6.325 µs · 0B · 0a | 971.4 µs · 292346B · 25008a | 483.2 µs · 169408B · 661a | 8.316 ms · 5558663B · 104191a | 1.506 ms · 324978B · 23643a | 366.5 µs · 20B · 0a | 1.28 ms · 324076B · 19107a |
| mat-mul | 369.5 µs · 6216B · 6a | 8.666 µs · 6216B · 6a | 42.6× | — | 5.535 µs · 6144B · 3a | 1.394 ms · 431714B · 34871a | 668.1 µs · 143168B · 478a | 1.205 ms · 61304B · 537a | 1.449 ms · 90712B · 9350a | 371.6 µs · 21B · 0a | 610.4 µs · 16875B · 1134a |
| n-body | 606.3 µs · 504B · 14a | 18.91 µs · 504B · 14a | 32.1× | — | 7.348 µs · 0B · 0a | 1.538 ms · 386898B · 35892a | 1.222 ms · 258913B · 2002a | 3.294 ms · 658435B · 81545a | 2.428 ms · 382715B · 34975a | 499.2 µs · 29B · 0a | 1.593 ms · 400499B · 18248a |
| n-queens | 427.8 µs · 120B · 6a | 32.98 µs · 120B · 6a | 13× | — | 6.006 µs · 0B · 0a | 963.5 µs · 234865B · 17784a | 563.9 µs · 14423B · 63a | 1.094 ms · 7424B · 35a | 1.428 ms · 410345B · 5147a | 284.1 µs · 16B · 0a | 1.772 ms · 749458B · 29235a |
| permutation-flips | 127.8 µs · 7680B · 128a | 23.12 µs · 7680B · 128a | 5.53× | — | 1.889 ns · 0B · 0a | 573.1 µs · 292858B · 9856a | 235.1 µs · 78808B · 451a | 534.9 µs · 122504B · 765a | 498.2 µs · 111976B · 2368a | 81.79 µs · 5B · 0a | 1.004 µs · 528B · 13a |
| recursive-fib-20 | 710.2 µs · 0B · 0a | 60.92 µs · 0B · 0a | 11.7× | 42.73 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.946 ms · 319345B · 28655a | 2.171 ms · 704B · 2a | 2.963 ms · 4680B · 39a | 11.68 ms · 9807926B · 109494a | 896.9 µs · 51B · 0a | 10.35 ms · 8302677B · 192847a |
| recursive-fib-35 | 964.8 ms · 0B · 0a | 82.78 ms · 0B · 0a | 11.7× | 58.22 ms · 8B · 1a | 29.13 ms · 0B · 0a | 2.613 s · 312797536B · 39088169a | 2.969 s · 971008B · 3793a | 4.029 s · 375360B · 46373a | 15.93 s · 13378017680B · 149349969a | 1.212 s · 19088B · 52a | — |
| sieve | 19.95 µs · 1048B · 2a | 943.3 ns · 1048B · 2a | 21.1× | 1.033 µs · 8B · 1a | 351 ns · 0B · 0a | 139.3 µs · 122504B · 1611a | 50.36 µs · 18416B · 44a | 84.42 µs · 1872B · 25a | 76.69 µs · 5704B · 30a | 18.94 µs · 1B · 0a | 40.16 µs · 1256B · 43a |
| sort-stress | 504.1 µs · 5136B · 512a | 12.58 µs · 5136B · 512a | 40.1× | — | 5.813 µs · 1024B · 2a | 1.29 ms · 270945B · 19965a | 842.9 µs · 36567B · 365a | 1.797 ms · 49104B · 3621a | 1.792 ms · 23448B · 2034a | 513.4 µs · 28B · 0a | 901.8 µs · 13121B · 935a |
| spectral-norm | 532.5 µs · 648B · 6a | 34.71 µs · 648B · 6a | 15.3× | — | 7.08 µs · 576B · 3a | 2.123 ms · 571371B · 54822a | 1.084 ms · 234784B · 925a | 2.434 ms · 275337B · 33729a | 4.888 ms · 2457138B · 52718a | 827.3 µs · 46B · 0a | 3.951 ms · 2443890B · 69364a |
| string-build | 732 µs · 85408B · 4107a | 573.7 µs · 85408B · 4107a | 1.28× | — | 390.1 µs · 855889B · 5001a | 2.485 ms · 2211276B · 36101a | 2.383 ms · 1078112B · 13859a | 3.137 ms · 1147384B · 26883a | 2.538 ms · 1326243B · 24469a | 550.1 µs · 31B · 0a | 3.021 ms · 2348544B · 38417a |
| struct-tree-walk | 280.5 µs · 768B · 8a | 183.4 µs · 768B · 8a | 1.53× | — | 1.889 ns · 0B · 0a | 659.9 µs · 458378B · 5114a | 1.413 ms · 818513B · 11253a | 1.11 ms · 558961B · 6149a | 3.627 ms · 2570666B · 34797a | 244.2 µs · 14B · 0a | 1.173 µs · 608B · 17a |
| tail-ping-pong | 29.45 µs · 0B · 0a | 517.8 ns · 0B · 0a | 56.9× | — | 890.8 ns · 0B · 0a | 109.3 µs · 106112B · 2001a | 35.98 µs · 21856B · 85a | 113.6 µs · 13984B · 1726a | 114.5 µs · 14280B · 1731a | 46.38 µs · 2B · 0a | 549.2 µs · 348835B · 9016a |
| tail-sum | 29.15 µs · 0B · 0a | 650.7 ns · 0B · 0a | 44.8× | — | 301.8 ns · 0B · 0a | 109.3 µs · 106112B · 2001a | 35.81 µs · 21856B · 85a | 113.1 µs · 13984B · 1726a | 114.6 µs · 14280B · 1731a | 46.1 µs · 2B · 0a | 29.88 µs · 584B · 16a |
| typed-array-sum | 7.196 µs · 0B · 0a | 231.8 ns · 0B · 0a | 31× | 267.2 ns · 8B · 1a | 1.898 ns · 0B · 0a | 45.76 µs · 94208B · 513a | 8.358 µs · 4000B · 15a | 27.35 µs · 2080B · 238a | 17.26 µs · 2496B · 246a | 6.169 µs · 0B · 0a | — |
| xorshift-i64 | 113.6 µs · 18424B · 2046a | 1.408 µs · 18424B · 2046a | 80.6× | — | 360.2 ns · 0B · 0a | 104.8 µs · 107608B · 2188a | — | 992.3 µs · 447251B · 12735a | 437.9 µs · 199938B · 7772a | 91.64 µs · 5B · 0a | 62.13 µs · 7361B · 547a |

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
