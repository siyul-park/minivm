# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.913 µs · 0B · 0a | 8.905 µs · 0B · 0a | 5.32 µs · 0B · 0a | 1.68× | — | 2.264 µs · 1024B · 128a | 38.64 µs · 96289B · 388a | 17.74 µs · 14376B · 256a | 71.53 µs · 78016B · 770a | 14.46 µs · 5712B · 266a | — | 30.93 µs · 2308B · 276a |
| binary-trees | 1.959 ms · 768B · 8a | 1.957 ms · 768B · 8a | 595 µs · 768B · 8a | 3.29× | — | 337.2 µs · 201937B · 8414a | 4.198 ms · 1468187B · 62877a | 10.77 ms · 6995686B · 92703a | 9.005 ms · 5198706B · 69052a | 28.27 ms · 19457628B · 280714a | — | 29.5 ms · 15128181B · 446106a |
| branch-tree | 1.358 µs · 0B · 0a | 18.92 ns · 0B · 0a | 88.99 ns · 0B · 0a | 15.3× | 625.4 ns · 16B · 1a | 160.3 ns · 0B · 0a | 46.5 µs · 95384B · 660a | 15.3 µs · 2463B · 9a | 26.35 µs · 1992B · 196a | 24.16 µs · 2168B · 203a | — | 20.59 µs · 1640B · 219a |
| closure-counter | 5.574 µs · 64B · 2a | 5.556 µs · 64B · 2a | 920 ns · 64B · 2a | 6.06× | — | 40.16 ns · 0B · 0a | 42.26 µs · 92272B · 261a | 12.84 µs · 151B · 3a | 19.27 µs · 1264B · 13a | 79.03 µs · 58312B · 659a | — | 90.72 µs · 35629B · 922a |
| coroutine-gen | 79.28 µs · 128B · 2a | 79.02 µs · 128B · 2a | 79.05 µs · 128B · 2a | 1× | — | 41.93 µs · 208B · 7a | — | 332.4 µs · 149936B · 649a | 612.5 µs · 330649B · 6719a | 206.5 µs · 91800B · 3646a | — | — |
| fannkuch | 828 µs · 34608B · 1442a | 815.6 µs · 34608B · 1442a | 214 µs · 34608B · 1442a | 3.87× | — | 35.91 µs · 17280B · 720a | 2.052 ms · 456578B · 25621a | 1.544 ms · 443135B · 1460a | 19.55 ms · 14005352B · 164946a | 3.384 ms · 1367678B · 16944a | — | 4.794 ms · 2501441B · 88439a |
| fnv1a64 | 122.7 µs · 16400B · 2049a | 124.1 µs · 16400B · 2049a | 1.156 µs · 16400B · 2049a | 106× | — | 902.9 ns · 0B · 0a | 166.4 µs · 120808B · 3838a | — | 1.482 ms · 608258B · 18475a | 404.9 µs · 162921B · 7429a | — | 153.2 µs · 17282B · 2074a |
| host-call | 454.6 µs · 41752B · 3080a | 470.6 µs · 41752B · 3080a | 465.7 µs · 41752B · 3080a | 0.976× | — | 3.312 µs · 0B · 0a | 185.3 µs · 139264B · 3073a | 136.6 µs · 22536B · 1079a | 804.4 µs · 158049B · 14585a | 106.7 µs · 14920B · 1799a | — | 596.9 µs · 340638B · 9231a |
| i64-wide-fib | 28.26 ms · 2913456B · 364178a | 20.56 ms · 1942304B · 242785a | 680 µs · 16B · 1a | 41.6× | — | 239.4 µs · 0B · 0a | 23.75 µs · 90520B · 52a | — | 156.7 ms · 64193264B · 1942305a | 357.2 ms · 253472592B · 4248840a | — | 132 ms · 98686608B · 2138708a |
| indirect-recursive-fib | 1.197 ms · 0B · 0a | 1.197 ms · 0B · 0a | 60.51 µs · 0B · 0a | 19.8× | 98.95 µs · 8B · 1a | 34.78 µs · 0B · 0a | 2.066 ms · 319346B · 28655a | 1.946 ms · 704B · 2a | 2.528 ms · 4680B · 39a | 11.8 ms · 10158189B · 109494a | — | 24.24 ms · 13061340B · 394061a |
| iterative-fib | 1.133 µs · 0B · 0a | 1.075 µs · 0B · 0a | 80.37 ns · 0B · 0a | 14.1× | 84.57 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.49 µs · 90592B · 61a | 1.162 µs · 160B · 0a | 4.215 µs · 368B · 20a | 6.25 µs · 2448B · 88a | — | 6.218 µs · 944B · 81a |
| mandelbrot | 289.4 µs · 0B · 0a | 248.5 µs · 0B · 0a | 11.24 µs · 0B · 0a | 25.7× | — | 6.323 µs · 0B · 0a | 950.2 µs · 292345B · 25008a | 494.1 µs · 169408B · 661a | 8.193 ms · 5558651B · 104191a | 1.498 ms · 324979B · 23643a | — | 1.525 ms · 365404B · 22513a |
| map-ops | 70.58 µs · 8816B · 3a | 70.42 µs · 8816B · 3a | 10.79 µs · 8816B · 3a | 6.54× | — | 10.89 µs · 4904B · 3a | — | 279 µs · 114328B · 198a | 385.4 µs · 66728B · 4078a | — | — | 1.137 ms · 501521B · 15813a |
| mat-mul | 375.5 µs · 6216B · 6a | 373.7 µs · 6216B · 6a | 8.666 µs · 6216B · 6a | 43.3× | — | 5.509 µs · 6144B · 3a | 1.392 ms · 431714B · 34871a | 674.9 µs · 143168B · 478a | 1.204 ms · 61305B · 537a | 1.465 ms · 90712B · 9350a | — | 915.9 µs · 36613B · 6036a |
| n-body | 611 µs · 504B · 14a | 577.1 µs · 504B · 14a | 19.04 µs · 504B · 14a | 32.1× | — | 7.344 µs · 0B · 0a | 1.524 ms · 386898B · 35892a | 1.215 ms · 258913B · 2002a | 3.363 ms · 658435B · 81545a | 2.421 ms · 382714B · 34975a | — | 1.758 ms · 445463B · 20669a |
| n-queens | 431.5 µs · 120B · 6a | 431.9 µs · 120B · 6a | 31.58 µs · 120B · 6a | 13.7× | — | 6.016 µs · 0B · 0a | 971 µs · 234865B · 17784a | 570.2 µs · 14423B · 63a | 1.093 ms · 7424B · 35a | 1.458 ms · 410345B · 5147a | — | 2.02 ms · 766009B · 33371a |
| naive-checksum | 250.8 µs · 0B · 0a | 142.4 µs · 0B · 0a | 8.727 µs · 0B · 0a | 28.7× | — | 2.759 µs · 0B · 0a | 626 µs · 235753B · 18206a | 486 µs · 174768B · 682a | 882.8 µs · 155641B · 19402a | 793.1 µs · 163816B · 20409a | — | 310.3 µs · 8977B · 2071a |
| permutation-flips | 128.4 µs · 7680B · 128a | 127.6 µs · 7680B · 128a | 17.28 µs · 7680B · 128a | 7.43× | — | 1.902 ns · 0B · 0a | 567.6 µs · 292857B · 9856a | 235 µs · 78807B · 451a | 527.9 µs · 122504B · 765a | 496.9 µs · 111976B · 2368a | — | 1.041 µs · 528B · 13a |
| recursive-fib-20 | 712 µs · 0B · 0a | 717.1 µs · 0B · 0a | 47.22 µs · 0B · 0a | 15.1× | 42.72 µs · 8B · 1a | 21.4 µs · 0B · 0a | 1.957 ms · 319346B · 28655a | 2.158 ms · 704B · 2a | 2.967 ms · 4680B · 39a | 11.77 ms · 9807918B · 109494a | — | 10.77 ms · 8302688B · 192847a |
| recursive-fib-35 | 970.6 ms · 0B · 0a | 975.6 ms · 0B · 0a | 63.96 ms · 0B · 0a | 15.2× | 58.22 ms · 8B · 1a | 29.13 ms · 0B · 0a | 2.578 s · 312797264B · 39088168a | 2.941 s · 971008B · 3793a | 4.036 s · 375360B · 46373a | 16.11 s · 13378016720B · 149349964a | — | — |
| sieve | 20.55 µs · 1048B · 2a | 20.47 µs · 1048B · 2a | 946.6 ns · 1048B · 2a | 21.7× | 1.028 µs · 8B · 1a | 350.9 ns · 0B · 0a | 142.4 µs · 122504B · 1611a | 50.79 µs · 18416B · 44a | 84.28 µs · 1872B · 25a | 77.44 µs · 5704B · 30a | — | 81.14 µs · 4368B · 795a |
| sort-stress | 501.3 µs · 5136B · 512a | 510.1 µs · 5136B · 512a | 12.85 µs · 5136B · 512a | 39× | — | 5.832 µs · 1024B · 2a | 1.261 ms · 270945B · 19965a | 852.7 µs · 36568B · 365a | 1.785 ms · 49104B · 3621a | 1.774 ms · 23448B · 2034a | — | 923.2 µs · 17473B · 1708a |
| spectral-norm | 532.6 µs · 648B · 6a | 458.7 µs · 648B · 6a | 8.381 µs · 648B · 6a | 63.5× | — | 7.048 µs · 576B · 3a | 2.052 ms · 571371B · 54822a | 1.084 ms · 234784B · 925a | 2.424 ms · 275337B · 33729a | 4.729 ms · 2457136B · 52718a | — | 4.112 ms · 2448004B · 74233a |
| string-build | 720.6 µs · 85408B · 4107a | 714.7 µs · 85408B · 4107a | 596.1 µs · 85408B · 4107a | 1.21× | — | 381.3 µs · 855889B · 5001a | 2.498 ms · 2211276B · 36101a | 2.376 ms · 1078110B · 13859a | 3.177 ms · 1147385B · 26883a | 2.477 ms · 1326241B · 24469a | — | 3.126 ms · 2372779B · 41432a |
| string-compare | 1.091 ms · 21136B · 1284a | 1.084 ms · 21136B · 1284a | 216.7 µs · 21136B · 1284a | 5.03× | — | 63.01 µs · 9632B · 1585a | 1.664 ms · 296281B · 20528a | 1.125 ms · 59712B · 3431a | 2.324 ms · 50744B · 3848a | 1.806 ms · 209521B · 5520a | — | 2.365 ms · 503620B · 31846a |
| struct-tree-walk | 281.3 µs · 768B · 8a | 280.6 µs · 768B · 8a | 108.6 µs · 768B · 8a | 2.59× | — | 1.89 ns · 0B · 0a | 657.6 µs · 458379B · 5114a | 1.387 ms · 818515B · 11253a | 1.108 ms · 558961B · 6149a | 3.549 ms · 2570661B · 34797a | — | 1.189 µs · 600B · 17a |
| tail-ping-pong | 29.44 µs · 0B · 0a | 29.43 µs · 0B · 0a | 477.9 ns · 0B · 0a | 61.6× | — | 890.9 ns · 0B · 0a | 110.2 µs · 106112B · 2001a | 36.18 µs · 21855B · 85a | 109.5 µs · 13984B · 1726a | 117.8 µs · 14280B · 1731a | — | 563.5 µs · 348833B · 9016a |
| tail-sum | 29.3 µs · 0B · 0a | 29.46 µs · 0B · 0a | 680.3 ns · 0B · 0a | 43.1× | — | 300.9 ns · 0B · 0a | 109.3 µs · 106112B · 2001a | 35.96 µs · 21856B · 85a | 109.8 µs · 13984B · 1726a | 117.4 µs · 14280B · 1731a | — | 84.41 µs · 4608B · 1017a |
| typed-array-sum | 7.357 µs · 0B · 0a | 7.365 µs · 0B · 0a | 263.9 ns · 0B · 0a | 27.9× | 260.9 ns · 8B · 1a | 1.898 ns · 0B · 0a | 46.24 µs · 94208B · 513a | 8.438 µs · 4000B · 15a | 27.24 µs · 2080B · 238a | 17.1 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.3 µs · 18424B · 2046a | 110.6 µs · 18424B · 2046a | 1.342 µs · 18424B · 2046a | 82.9× | — | 360.9 ns · 0B · 0a | 103.3 µs · 107608B · 2188a | — | 985 µs · 447248B · 12735a | 429.2 µs · 199937B · 7772a | — | 101.4 µs · 13545B · 1317a |

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
