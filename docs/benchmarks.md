# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.139 µs · 0B · 0a | 9.135 µs · 0B · 0a | 5.42 µs · 0B · 0a | 1.69× | — | 2.227 µs · 1024B · 128a | 37.13 µs · 96288B · 388a | 17.67 µs · 14376B · 256a | 71.46 µs · 78016B · 770a | 14.76 µs · 5712B · 266a | — | 30.68 µs · 2308B · 276a |
| binary-trees | 1.966 ms · 768B · 8a | 1.966 ms · 768B · 8a | 599.8 µs · 768B · 8a | 3.28× | — | 327.6 µs · 201937B · 8414a | 4.275 ms · 1468193B · 62877a | 10.59 ms · 6995676B · 92703a | 9.074 ms · 5198708B · 69052a | 28.08 ms · 19457593B · 280714a | — | 30.18 ms · 15128165B · 446106a |
| branch-tree | 1.349 µs · 0B · 0a | 18.73 ns · 0B · 0a | 88.38 ns · 0B · 0a | 15.3× | 626.2 ns · 16B · 1a | 160.7 ns · 0B · 0a | 46.47 µs · 95385B · 660a | 15.18 µs · 2463B · 9a | 26.83 µs · 1992B · 196a | 24.56 µs · 2168B · 203a | — | 20.74 µs · 1640B · 219a |
| closure-counter | 5.511 µs · 64B · 2a | 5.527 µs · 64B · 2a | 933.8 ns · 64B · 2a | 5.9× | — | 40.46 ns · 0B · 0a | 41.31 µs · 92272B · 261a | 13.21 µs · 151B · 3a | 19.26 µs · 1264B · 13a | 81.14 µs · 58312B · 659a | — | 94.02 µs · 35628B · 922a |
| coroutine-gen | 78.92 µs · 128B · 2a | 79.32 µs · 128B · 2a | 78.76 µs · 128B · 2a | 1× | — | 42.21 µs · 208B · 7a | — | 330.5 µs · 149936B · 649a | 601.9 µs · 330649B · 6719a | 199.8 µs · 91800B · 3646a | — | — |
| fannkuch | 830.2 µs · 34608B · 1442a | 812.3 µs · 34608B · 1442a | 228.9 µs · 34608B · 1442a | 3.63× | — | 35.81 µs · 17280B · 720a | 2.023 ms · 456578B · 25621a | 1.542 ms · 443136B · 1460a | 19.48 ms · 14005343B · 164946a | 3.435 ms · 1367679B · 16944a | — | 4.829 ms · 2501428B · 88439a |
| fnv1a64 | 124.5 µs · 16400B · 2049a | 124.9 µs · 16400B · 2049a | 1.127 µs · 16400B · 2049a | 110× | — | 904.3 ns · 0B · 0a | 164.2 µs · 120808B · 3838a | — | 1.516 ms · 608261B · 18475a | 401.2 µs · 162921B · 7429a | — | 155 µs · 17282B · 2074a |
| host-call | 458.1 µs · 41752B · 3080a | 480.8 µs · 41752B · 3080a | 469.9 µs · 41752B · 3080a | 0.975× | — | 3.312 µs · 0B · 0a | 181.5 µs · 139264B · 3073a | 134.6 µs · 22536B · 1079a | 813.2 µs · 158048B · 14585a | 105.7 µs · 14920B · 1799a | — | 595.3 µs · 340636B · 9231a |
| i64-wide-fib | 27.99 ms · 2913456B · 364178a | 20.77 ms · 1942304B · 242785a | 682.1 µs · 16B · 1a | 41× | — | 239.6 µs · 0B · 0a | 24.13 µs · 90520B · 52a | — | 160.3 ms · 64193376B · 1942306a | 351.5 ms · 253472944B · 4248842a | — | 136.3 ms · 98686613B · 2138708a |
| indirect-recursive-fib | 1.191 ms · 0B · 0a | 1.187 ms · 0B · 0a | 60.49 µs · 0B · 0a | 19.7× | 99.22 µs · 8B · 1a | 35.47 µs · 0B · 0a | 2.119 ms · 319345B · 28655a | 1.95 ms · 703B · 2a | 2.576 ms · 4680B · 39a | 11.59 ms · 10158196B · 109494a | — | 25.49 ms · 13061284B · 394061a |
| iterative-fib | 1.135 µs · 0B · 0a | 1.073 µs · 0B · 0a | 80.4 ns · 0B · 0a | 14.1× | 84.92 ns · 8B · 1a | 12.41 ns · 0B · 0a | 24.11 µs · 90592B · 61a | 1.156 µs · 160B · 0a | 4.24 µs · 368B · 20a | 6.244 µs · 2448B · 88a | — | 6.199 µs · 944B · 81a |
| mandelbrot | 291.3 µs · 0B · 0a | 248.9 µs · 0B · 0a | 11.27 µs · 0B · 0a | 25.8× | — | 6.32 µs · 0B · 0a | 974 µs · 292345B · 25008a | 483.5 µs · 169407B · 661a | 8.217 ms · 5558662B · 104191a | 1.494 ms · 324978B · 23643a | — | 1.551 ms · 365401B · 22513a |
| map-ops | 70.62 µs · 8816B · 3a | 70.64 µs · 8816B · 3a | 10.88 µs · 8816B · 3a | 6.49× | — | 10.87 µs · 4904B · 3a | — | 280.1 µs · 114328B · 198a | 388.2 µs · 66728B · 4078a | — | — | 1.14 ms · 501521B · 15813a |
| mat-mul | 376 µs · 6216B · 6a | 375.3 µs · 6216B · 6a | 8.577 µs · 6216B · 6a | 43.8× | — | 5.511 µs · 6144B · 3a | 1.395 ms · 431714B · 34871a | 659.7 µs · 143168B · 478a | 1.209 ms · 61304B · 537a | 1.464 ms · 90713B · 9350a | — | 931.4 µs · 36614B · 6036a |
| n-body | 611.1 µs · 504B · 14a | 582.7 µs · 504B · 14a | 19.17 µs · 504B · 14a | 31.9× | — | 7.361 µs · 0B · 0a | 1.495 ms · 386898B · 35892a | 1.21 ms · 258913B · 2002a | 3.419 ms · 658435B · 81545a | 2.457 ms · 382714B · 34975a | — | 1.768 ms · 445465B · 20669a |
| n-queens | 429.3 µs · 120B · 6a | 429.5 µs · 120B · 6a | 31.61 µs · 120B · 6a | 13.6× | — | 6.012 µs · 0B · 0a | 965.7 µs · 234865B · 17784a | 571.7 µs · 14423B · 63a | 1.102 ms · 7424B · 35a | 1.456 ms · 410346B · 5147a | — | 2.036 ms · 766009B · 33371a |
| naive-checksum | 250.5 µs · 0B · 0a | 141.9 µs · 0B · 0a | 8.678 µs · 0B · 0a | 28.9× | — | 2.76 µs · 0B · 0a | 622 µs · 235753B · 18206a | 483 µs · 174768B · 682a | 885.4 µs · 155640B · 19402a | 799.1 µs · 163817B · 20409a | — | 313.7 µs · 8977B · 2071a |
| permutation-flips | 128.3 µs · 7680B · 128a | 128.4 µs · 7680B · 128a | 17.26 µs · 7680B · 128a | 7.43× | — | 1.877 ns · 0B · 0a | 554.3 µs · 292857B · 9856a | 235.4 µs · 78808B · 451a | 523.7 µs · 122504B · 765a | 505.5 µs · 111976B · 2368a | — | 1.043 µs · 528B · 13a |
| recursive-fib-20 | 711.5 µs · 0B · 0a | 719.7 µs · 0B · 0a | 47.4 µs · 0B · 0a | 15× | 42.75 µs · 8B · 1a | 21.47 µs · 0B · 0a | 1.935 ms · 319345B · 28655a | 2.162 ms · 704B · 2a | 2.972 ms · 4680B · 39a | 11.77 ms · 9807915B · 109494a | — | 10.96 ms · 8302697B · 192848a |
| recursive-fib-35 | 969.7 ms · 0B · 0a | 976.7 ms · 0B · 0a | 63.94 ms · 0B · 0a | 15.2× | 58.19 ms · 8B · 1a | 29.28 ms · 0B · 0a | 2.598 s · 312797280B · 39088167a | 2.951 s · 971008B · 3793a | 4.049 s · 375360B · 46373a | 16.02 s · 13378014928B · 149349943a | — | — |
| sieve | 20.32 µs · 1048B · 2a | 20.37 µs · 1048B · 2a | 948.8 ns · 1048B · 2a | 21.4× | 1.023 µs · 8B · 1a | 372 ns · 0B · 0a | 138.6 µs · 122504B · 1611a | 50.65 µs · 18416B · 44a | 83.94 µs · 1872B · 25a | 76.76 µs · 5704B · 30a | — | 82.3 µs · 4368B · 795a |
| sort-stress | 504.7 µs · 5136B · 512a | 508.3 µs · 5136B · 512a | 12.86 µs · 5136B · 512a | 39.2× | — | 5.366 µs · 1024B · 2a | 1.291 ms · 270945B · 19965a | 852.5 µs · 36568B · 365a | 1.801 ms · 49104B · 3621a | 1.777 ms · 23448B · 2034a | — | 919.8 µs · 17474B · 1708a |
| spectral-norm | 534.2 µs · 648B · 6a | 461.2 µs · 648B · 6a | 8.952 µs · 648B · 6a | 59.7× | — | 7.04 µs · 576B · 3a | 2.089 ms · 571370B · 54822a | 1.077 ms · 234784B · 925a | 2.474 ms · 275337B · 33729a | 4.7 ms · 2457136B · 52718a | — | 4.253 ms · 2448005B · 74233a |
| string-build | 735.6 µs · 85408B · 4107a | 729.3 µs · 85408B · 4107a | 619.6 µs · 85408B · 4107a | 1.19× | — | 375.2 µs · 855889B · 5001a | 2.438 ms · 2211274B · 36101a | 2.322 ms · 1078111B · 13859a | 3.182 ms · 1147384B · 26883a | 2.438 ms · 1326244B · 24469a | — | 3.155 ms · 2372781B · 41432a |
| string-compare | 1.085 ms · 21136B · 1284a | 1.084 ms · 21136B · 1284a | 222.2 µs · 21136B · 1284a | 4.88× | — | 63.66 µs · 9632B · 1585a | 1.673 ms · 296281B · 20528a | 1.134 ms · 59712B · 3431a | 2.334 ms · 50744B · 3848a | 1.793 ms · 209521B · 5520a | — | 2.367 ms · 503618B · 31846a |
| struct-tree-walk | 282.5 µs · 768B · 8a | 282 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.62× | — | 1.876 ns · 0B · 0a | 661 µs · 458379B · 5114a | 1.39 ms · 818513B · 11253a | 1.107 ms · 558960B · 6149a | 3.549 ms · 2570665B · 34797a | — | 1.21 µs · 600B · 17a |
| tail-ping-pong | 29.3 µs · 0B · 0a | 29.54 µs · 0B · 0a | 488.2 ns · 0B · 0a | 60× | — | 891 ns · 0B · 0a | 109.3 µs · 106112B · 2001a | 35.85 µs · 21855B · 85a | 111.5 µs · 13984B · 1726a | 116.2 µs · 14280B · 1731a | — | 555.2 µs · 348832B · 9016a |
| tail-sum | 29.43 µs · 0B · 0a | 29.21 µs · 0B · 0a | 686.8 ns · 0B · 0a | 42.9× | — | 302.6 ns · 0B · 0a | 109.1 µs · 106112B · 2001a | 35.6 µs · 21856B · 85a | 111.1 µs · 13984B · 1726a | 116.2 µs · 14280B · 1731a | — | 84.96 µs · 4608B · 1017a |
| typed-array-sum | 7.357 µs · 0B · 0a | 7.394 µs · 0B · 0a | 269 ns · 0B · 0a | 27.3× | 265.4 ns · 8B · 1a | 1.879 ns · 0B · 0a | 45.88 µs · 94208B · 513a | 8.312 µs · 4000B · 15a | 27.12 µs · 2080B · 238a | 17.1 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.4 µs · 18424B · 2046a | 112.3 µs · 18424B · 2046a | 1.34 µs · 18424B · 2046a | 83.9× | — | 356 ns · 0B · 0a | 104 µs · 107608B · 2188a | — | 1.008 ms · 447252B · 12735a | 437.7 µs · 199937B · 7772a | — | 101.7 µs · 13545B · 1317a |

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
