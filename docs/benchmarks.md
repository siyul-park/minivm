# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.439 µs · 0B · 0a | 5.327 µs · 0B · 0a | 1.77× | — | 2.206 µs · 1024B · 128a | 38.37 µs · 96289B · 388a | 17.1 µs · 14376B · 256a | 72.15 µs · 78016B · 770a | 14.33 µs · 5712B · 266a | — | 30.73 µs · 2308B · 276a |
| binary-trees | 1.977 ms · 768B · 8a | 589.5 µs · 768B · 8a | 3.35× | — | 293.7 µs · 201936B · 8414a | 3.858 ms · 1468183B · 62877a | 10.71 ms · 6995680B · 92703a | 8.923 ms · 5198697B · 69052a | 28.41 ms · 19457625B · 280714a | — | 29 ms · 15128180B · 446106a |
| branch-tree | 1.328 µs · 0B · 0a | 153.5 ns · 0B · 0a | 8.65× | 627.8 ns · 16B · 1a | 161.4 ns · 0B · 0a | 47.15 µs · 95384B · 660a | 15.2 µs · 2463B · 9a | 26.71 µs · 1992B · 196a | 23.85 µs · 2168B · 203a | — | 20.6 µs · 1640B · 219a |
| closure-counter | 5.559 µs · 64B · 2a | 923 ns · 64B · 2a | 6.02× | — | 40.42 ns · 0B · 0a | 49.61 µs · 92272B · 261a | 12.99 µs · 151B · 3a | 19.67 µs · 1264B · 13a | 80.76 µs · 58312B · 659a | — | 89.47 µs · 35628B · 922a |
| coroutine-gen | 78.01 µs · 128B · 2a | 78.42 µs · 128B · 2a | 0.995× | — | 43.36 µs · 208B · 7a | — | 337 µs · 149936B · 649a | 616.6 µs · 330649B · 6719a | 200.4 µs · 91800B · 3646a | — | — |
| fannkuch | 822.1 µs · 34608B · 1442a | 222.9 µs · 34608B · 1442a | 3.69× | — | 35.28 µs · 17280B · 720a | 1.993 ms · 456579B · 25621a | 1.544 ms · 443135B · 1460a | 19.34 ms · 14005367B · 164946a | 3.353 ms · 1367677B · 16944a | — | 4.726 ms · 2501446B · 88439a |
| fnv1a64 | 123.3 µs · 16400B · 2049a | 1.155 µs · 16400B · 2049a | 107× | — | 902.5 ns · 0B · 0a | 168.7 µs · 120808B · 3838a | — | 1.501 ms · 608261B · 18475a | 397.9 µs · 162921B · 7429a | — | 153.5 µs · 17282B · 2074a |
| host-call | 721.6 µs · 196592B · 14073a | 701 µs · 196592B · 14073a | 1.03× | — | 3.313 µs · 0B · 0a | 185.1 µs · 139264B · 3073a | 135.6 µs · 22536B · 1079a | 786.6 µs · 158049B · 14585a | 107.8 µs · 14920B · 1799a | — | 575.6 µs · 340638B · 9231a |
| i64-wide-fib | 28.08 ms · 2913440B · 364178a | 679.9 µs · 16B · 1a | 41.3× | — | 239.3 µs · 0B · 0a | 23.54 µs · 90520B · 52a | — | 156.8 ms · 64193480B · 1942307a | 349.6 ms · 253473760B · 4248848a | — | 123.1 ms · 98686650B · 2138708a |
| indirect-recursive-fib | 1.195 ms · 0B · 0a | 60.53 µs · 0B · 0a | 19.7× | 98.97 µs · 8B · 1a | 35.18 µs · 0B · 0a | 2.056 ms · 319345B · 28655a | 1.943 ms · 704B · 2a | 2.544 ms · 4680B · 39a | 11.44 ms · 10158188B · 109494a | — | 24.43 ms · 13061329B · 394061a |
| iterative-fib | 1.118 µs · 0B · 0a | 80.04 ns · 0B · 0a | 14× | 85.62 ns · 8B · 1a | 12.45 ns · 0B · 0a | 24.69 µs · 90592B · 61a | 1.163 µs · 160B · 0a | 4.264 µs · 368B · 20a | 6.142 µs · 2448B · 88a | — | 6.195 µs · 944B · 81a |
| mandelbrot | 290.1 µs · 0B · 0a | 10.9 µs · 0B · 0a | 26.6× | — | 6.323 µs · 0B · 0a | 936.5 µs · 292345B · 25008a | 487.3 µs · 169408B · 661a | 8.483 ms · 5558663B · 104191a | 1.49 ms · 324977B · 23643a | — | 1.526 ms · 365400B · 22513a |
| map-ops | 73.26 µs · 9584B · 5a | 73.79 µs · 9584B · 5a | 0.993× | — | 10.85 µs · 4904B · 3a | — | 281 µs · 114328B · 198a | 379.8 µs · 66728B · 4078a | — | — | 1.123 ms · 501519B · 15813a |
| mat-mul | 373.5 µs · 6216B · 6a | 8.692 µs · 6216B · 6a | 43× | — | 5.497 µs · 6144B · 3a | 1.341 ms · 431715B · 34871a | 677 µs · 143169B · 478a | 1.206 ms · 61304B · 537a | 1.442 ms · 90713B · 9350a | — | 923.5 µs · 36615B · 6036a |
| n-body | 610.9 µs · 504B · 14a | 18.84 µs · 504B · 14a | 32.4× | — | 7.36 µs · 0B · 0a | 1.495 ms · 386898B · 35892a | 1.218 ms · 258913B · 2002a | 3.237 ms · 658436B · 81545a | 2.405 ms · 382715B · 34975a | — | 1.752 ms · 445462B · 20669a |
| n-queens | 428.4 µs · 120B · 6a | 31.56 µs · 120B · 6a | 13.6× | — | 6.004 µs · 0B · 0a | 933 µs · 234865B · 17784a | 571.4 µs · 14424B · 63a | 1.096 ms · 7424B · 35a | 1.427 ms · 410346B · 5147a | — | 2.006 ms · 766004B · 33371a |
| permutation-flips | 128.6 µs · 7680B · 128a | 17.29 µs · 7680B · 128a | 7.44× | — | 1.877 ns · 0B · 0a | 554.2 µs · 292857B · 9856a | 237.1 µs · 78808B · 451a | 524.9 µs · 122504B · 765a | 486.9 µs · 111976B · 2368a | — | 992.5 ns · 528B · 13a |
| recursive-fib-20 | 712.7 µs · 0B · 0a | 47.24 µs · 0B · 0a | 15.1× | 42.74 µs · 8B · 1a | 21.57 µs · 0B · 0a | 1.938 ms · 319345B · 28655a | 2.175 ms · 704B · 2a | 2.954 ms · 4680B · 39a | 11.48 ms · 9807919B · 109494a | — | 10.68 ms · 8302692B · 192848a |
| recursive-fib-35 | 973.8 ms · 0B · 0a | 63.96 ms · 0B · 0a | 15.2× | 58.22 ms · 8B · 1a | 29.31 ms · 0B · 0a | 2.569 s · 312797200B · 39088167a | 2.958 s · 971008B · 3793a | 4.02 s · 375360B · 46373a | 15.65 s · 13378015936B · 149349954a | — | — |
| sieve | 20.14 µs · 1048B · 2a | 939.3 ns · 1048B · 2a | 21.4× | 1.034 µs · 8B · 1a | 372 ns · 0B · 0a | 139.8 µs · 122504B · 1611a | 50.9 µs · 18416B · 44a | 84.46 µs · 1872B · 25a | 76.78 µs · 5704B · 30a | — | 80.82 µs · 4368B · 795a |
| sort-stress | 505.3 µs · 5136B · 512a | 12.62 µs · 5136B · 512a | 40× | — | 5.325 µs · 1024B · 2a | 1.272 ms · 270945B · 19965a | 847.7 µs · 36568B · 365a | 1.786 ms · 49104B · 3621a | 1.779 ms · 23448B · 2034a | — | 923.3 µs · 17475B · 1708a |
| spectral-norm | 537.8 µs · 648B · 6a | 8.326 µs · 648B · 6a | 64.6× | — | 7.027 µs · 576B · 3a | 2.015 ms · 571371B · 54822a | 1.093 ms · 234784B · 925a | 2.437 ms · 275337B · 33729a | 4.625 ms · 2457133B · 52718a | — | 4.14 ms · 2448013B · 74233a |
| string-build | 720.6 µs · 85408B · 4107a | 598.1 µs · 85408B · 4107a | 1.2× | — | 373.9 µs · 855889B · 5001a | 2.446 ms · 2211272B · 36101a | 2.379 ms · 1078111B · 13859a | 3.284 ms · 1147384B · 26883a | 2.483 ms · 1326242B · 24469a | — | 3.111 ms · 2372780B · 41432a |
| string-compare | 1.087 ms · 21136B · 1284a | 235.3 µs · 21136B · 1284a | 4.62× | — | 62.46 µs · 9632B · 1585a | 1.657 ms · 296281B · 20528a | 1.13 ms · 59712B · 3431a | 2.329 ms · 50744B · 3848a | 1.828 ms · 209521B · 5520a | — | 2.348 ms · 503617B · 31846a |
| struct-tree-walk | 285 µs · 768B · 8a | 107.2 µs · 768B · 8a | 2.66× | — | 1.877 ns · 0B · 0a | 654.8 µs · 458378B · 5114a | 1.381 ms · 818512B · 11253a | 1.084 ms · 558960B · 6149a | 3.533 ms · 2570661B · 34797a | — | 1.158 µs · 600B · 17a |
| tail-ping-pong | 29.15 µs · 0B · 0a | 479.7 ns · 0B · 0a | 60.8× | — | 891.1 ns · 0B · 0a | 109 µs · 106112B · 2001a | 36.23 µs · 21856B · 85a | 112.3 µs · 13984B · 1726a | 114.8 µs · 14280B · 1731a | — | 545.9 µs · 348833B · 9016a |
| tail-sum | 28.87 µs · 0B · 0a | 676.6 ns · 0B · 0a | 42.7× | — | 302.6 ns · 0B · 0a | 109.2 µs · 106112B · 2001a | 35.91 µs · 21856B · 85a | 112.3 µs · 13984B · 1726a | 114.8 µs · 14280B · 1731a | — | 84.33 µs · 4608B · 1017a |
| typed-array-sum | 7.233 µs · 0B · 0a | 263.8 ns · 0B · 0a | 27.4× | 265.2 ns · 8B · 1a | 1.875 ns · 0B · 0a | 45.35 µs · 94208B · 513a | 8.356 µs · 4000B · 15a | 27.33 µs · 2080B · 238a | 17.21 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.4 µs · 18424B · 2046a | 1.388 µs · 18424B · 2046a | 81× | — | 356.1 ns · 0B · 0a | 104.4 µs · 107608B · 2188a | — | 972.3 µs · 447253B · 12735a | 430.2 µs · 199938B · 7772a | — | 101.8 µs · 13546B · 1317a |

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
