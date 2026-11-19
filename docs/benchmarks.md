# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.857 µs · 0B · 0a | 8.855 µs · 0B · 0a | 5.345 µs · 0B · 0a | 1.66× | — | 2.234 µs · 1024B · 128a | 37.58 µs · 96288B · 388a | 17.63 µs · 14376B · 256a | 80.94 µs · 78016B · 770a | 15.14 µs · 5712B · 266a | — | 30.98 µs · 2308B · 276a |
| binary-trees | 1.953 ms · 768B · 8a | 1.955 ms · 768B · 8a | 594.4 µs · 768B · 8a | 3.29× | — | 296.1 µs · 201936B · 8414a | 3.924 ms · 1468183B · 62877a | 10.59 ms · 6995679B · 92703a | 8.913 ms · 5198695B · 69052a | 27.73 ms · 19457574B · 280714a | — | 28.8 ms · 15128181B · 446106a |
| branch-tree | 1.347 µs · 0B · 0a | 20.11 ns · 0B · 0a | 83.59 ns · 0B · 0a | 16.1× | 627 ns · 16B · 1a | 160.5 ns · 0B · 0a | 45.84 µs · 95385B · 660a | 15.26 µs · 2464B · 9a | 26.41 µs · 1992B · 196a | 23.71 µs · 2168B · 203a | — | 20.66 µs · 1640B · 219a |
| closure-counter | 5.559 µs · 64B · 2a | 5.5 µs · 64B · 2a | 903.9 ns · 64B · 2a | 6.15× | — | 40.43 ns · 0B · 0a | 42.88 µs · 92272B · 261a | 12.88 µs · 152B · 3a | 19.12 µs · 1264B · 13a | 80.57 µs · 58312B · 659a | — | 90.44 µs · 35629B · 922a |
| coroutine-gen | 78.9 µs · 128B · 2a | 79.21 µs · 128B · 2a | 79.29 µs · 128B · 2a | 0.995× | — | 42.4 µs · 208B · 7a | — | 329.1 µs · 149936B · 649a | 606.2 µs · 330649B · 6719a | 201.8 µs · 91800B · 3646a | — | — |
| fannkuch | 828.7 µs · 34608B · 1442a | 812.2 µs · 34608B · 1442a | 218.5 µs · 34608B · 1442a | 3.79× | — | 35.61 µs · 17280B · 720a | 2.017 ms · 456578B · 25621a | 1.552 ms · 443136B · 1460a | 19.38 ms · 14005341B · 164946a | 3.382 ms · 1367679B · 16944a | — | 4.793 ms · 2501453B · 88439a |
| fnv1a64 | 120.8 µs · 16400B · 2049a | 120 µs · 16400B · 2049a | 1.156 µs · 16400B · 2049a | 104× | — | 902.5 ns · 0B · 0a | 164.6 µs · 120808B · 3838a | — | 1.456 ms · 608256B · 18475a | 395.3 µs · 162921B · 7429a | — | 153.6 µs · 17282B · 2074a |
| host-call | 469 µs · 41752B · 3080a | 468 µs · 41752B · 3080a | 470.4 µs · 41752B · 3080a | 0.997× | — | 3.314 µs · 0B · 0a | 176.8 µs · 139264B · 3073a | 129.6 µs · 22536B · 1079a | 774.6 µs · 158049B · 14585a | 106.7 µs · 14920B · 1799a | — | 581.6 µs · 340641B · 9231a |
| i64-wide-fib | 27.79 ms · 2913440B · 364178a | 20.72 ms · 1942304B · 242785a | 681.1 µs · 16B · 1a | 40.8× | — | 238.7 µs · 0B · 0a | 23.62 µs · 90520B · 52a | — | 158.7 ms · 64193456B · 1942306a | 352.7 ms · 253472720B · 4248841a | — | 129.5 ms · 98686672B · 2138708a |
| indirect-recursive-fib | 1.189 ms · 0B · 0a | 1.183 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.7× | 99.22 µs · 8B · 1a | 34.97 µs · 0B · 0a | 2.14 ms · 319345B · 28655a | 1.941 ms · 704B · 2a | 2.558 ms · 4680B · 39a | 11.52 ms · 10158191B · 109494a | — | 24.41 ms · 13061349B · 394061a |
| iterative-fib | 1.133 µs · 0B · 0a | 1.072 µs · 0B · 0a | 78.85 ns · 0B · 0a | 14.4× | 85.37 ns · 8B · 1a | 12.47 ns · 0B · 0a | 24.54 µs · 90592B · 61a | 1.163 µs · 160B · 0a | 4.258 µs · 368B · 20a | 6.175 µs · 2448B · 88a | — | 6.152 µs · 944B · 81a |
| mandelbrot | 289.7 µs · 0B · 0a | 248.8 µs · 0B · 0a | 11.25 µs · 0B · 0a | 25.8× | — | 6.32 µs · 0B · 0a | 964.2 µs · 292345B · 25008a | 488.4 µs · 169408B · 661a | 8.078 ms · 5558650B · 104191a | 1.474 ms · 324978B · 23643a | — | 1.525 ms · 365398B · 22513a |
| map-ops | 70.5 µs · 8816B · 3a | 70.24 µs · 8816B · 3a | 10.77 µs · 8816B · 3a | 6.54× | — | 10.86 µs · 4904B · 3a | — | 277.6 µs · 114328B · 198a | 380 µs · 66728B · 4078a | — | — | 1.103 ms · 501514B · 15813a |
| mat-mul | 375.8 µs · 6216B · 6a | 374.4 µs · 6216B · 6a | 8.597 µs · 6216B · 6a | 43.7× | — | 5.467 µs · 6144B · 3a | 1.381 ms · 431714B · 34871a | 695.8 µs · 143169B · 478a | 1.212 ms · 61304B · 537a | 1.457 ms · 90713B · 9350a | — | 924.3 µs · 36618B · 6036a |
| n-body | 612.3 µs · 504B · 14a | 583.9 µs · 504B · 14a | 19.02 µs · 504B · 14a | 32.2× | — | 7.357 µs · 0B · 0a | 1.492 ms · 386897B · 35892a | 1.209 ms · 258913B · 2002a | 3.255 ms · 658436B · 81545a | 2.382 ms · 382714B · 34975a | — | 1.744 ms · 445460B · 20669a |
| n-queens | 430 µs · 120B · 6a | 429.6 µs · 120B · 6a | 31.65 µs · 120B · 6a | 13.6× | — | 6.018 µs · 0B · 0a | 945.1 µs · 234865B · 17784a | 571.5 µs · 14423B · 63a | 1.095 ms · 7424B · 35a | 1.431 ms · 410346B · 5147a | — | 2.01 ms · 766010B · 33371a |
| naive-checksum | 250.6 µs · 0B · 0a | 141.7 µs · 0B · 0a | 8.727 µs · 0B · 0a | 28.7× | — | 2.758 µs · 0B · 0a | 616.3 µs · 235753B · 18206a | 483 µs · 174767B · 682a | 869.2 µs · 155640B · 19402a | 778.3 µs · 163816B · 20409a | — | 310.7 µs · 8977B · 2071a |
| permutation-flips | 129.1 µs · 7680B · 128a | 128.6 µs · 7680B · 128a | 17.02 µs · 7680B · 128a | 7.58× | — | 1.863 ns · 0B · 0a | 560.8 µs · 292857B · 9856a | 236.7 µs · 78808B · 451a | 518.1 µs · 122504B · 765a | 489.9 µs · 111976B · 2368a | — | 1.002 µs · 528B · 13a |
| recursive-fib-20 | 719.4 µs · 0B · 0a | 724.9 µs · 0B · 0a | 47.32 µs · 0B · 0a | 15.2× | 42.73 µs · 8B · 1a | 21.46 µs · 0B · 0a | 1.946 ms · 319345B · 28655a | 2.167 ms · 704B · 2a | 2.956 ms · 4680B · 39a | 11.27 ms · 9807914B · 109494a | — | 10.91 ms · 8302697B · 192848a |
| recursive-fib-35 | 969.3 ms · 0B · 0a | 978.5 ms · 0B · 0a | 63.85 ms · 0B · 0a | 15.2× | 58.23 ms · 8B · 1a | 29.23 ms · 0B · 0a | 2.587 s · 312797424B · 39088168a | 2.956 s · 971008B · 3793a | 4.055 s · 375360B · 46373a | 15.59 s · 13378014672B · 149349948a | — | — |
| sieve | 20.37 µs · 1048B · 2a | 20.32 µs · 1048B · 2a | 940.8 ns · 1048B · 2a | 21.6× | 1.02 µs · 8B · 1a | 371.9 ns · 0B · 0a | 140.2 µs · 122504B · 1611a | 50.71 µs · 18416B · 44a | 84.38 µs · 1872B · 25a | 76.87 µs · 5704B · 30a | — | 80.81 µs · 4368B · 795a |
| sort-stress | 504.6 µs · 5136B · 512a | 509 µs · 5136B · 512a | 12.62 µs · 5136B · 512a | 40× | — | 5.335 µs · 1024B · 2a | 1.303 ms · 270945B · 19965a | 845.1 µs · 36567B · 365a | 1.79 ms · 49104B · 3621a | 1.772 ms · 23448B · 2034a | — | 923 µs · 17475B · 1708a |
| spectral-norm | 536.6 µs · 648B · 6a | 464.4 µs · 648B · 6a | 8.366 µs · 648B · 6a | 64.1× | — | 7.043 µs · 576B · 3a | 2.022 ms · 571371B · 54822a | 1.086 ms · 234784B · 925a | 2.466 ms · 275337B · 33729a | 4.708 ms · 2457136B · 52718a | — | 4.108 ms · 2448016B · 74233a |
| string-build | 720.1 µs · 85408B · 4107a | 712.8 µs · 85408B · 4107a | 600.9 µs · 85408B · 4107a | 1.2× | — | 369.6 µs · 855890B · 5001a | 2.432 ms · 2211274B · 36101a | 2.31 ms · 1078109B · 13859a | 3.139 ms · 1147384B · 26883a | 2.443 ms · 1326242B · 24469a | — | 3.108 ms · 2372778B · 41432a |
| string-compare | 1.086 ms · 21136B · 1284a | 1.083 ms · 21136B · 1284a | 221.5 µs · 21136B · 1284a | 4.9× | — | 63.17 µs · 9632B · 1585a | 1.654 ms · 296281B · 20528a | 1.128 ms · 59712B · 3431a | 2.333 ms · 50744B · 3848a | 1.818 ms · 209521B · 5520a | — | 2.356 ms · 503612B · 31846a |
| struct-tree-walk | 281.5 µs · 768B · 8a | 282.6 µs · 768B · 8a | 108.2 µs · 768B · 8a | 2.6× | — | 1.864 ns · 0B · 0a | 650.8 µs · 458378B · 5114a | 1.383 ms · 818512B · 11253a | 1.089 ms · 558960B · 6149a | 3.47 ms · 2570668B · 34797a | — | 1.2 µs · 600B · 17a |
| tail-ping-pong | 29.44 µs · 0B · 0a | 29.35 µs · 0B · 0a | 474.9 ns · 0B · 0a | 62× | — | 891 ns · 0B · 0a | 108.5 µs · 106112B · 2001a | 35.81 µs · 21855B · 85a | 111.7 µs · 13984B · 1726a | 115.2 µs · 14280B · 1731a | — | 562.5 µs · 348831B · 9016a |
| tail-sum | 29.44 µs · 0B · 0a | 29.38 µs · 0B · 0a | 671.6 ns · 0B · 0a | 43.8× | — | 302.5 ns · 0B · 0a | 109.8 µs · 106112B · 2001a | 36.03 µs · 21856B · 85a | 112.2 µs · 13984B · 1726a | 114.8 µs · 14280B · 1731a | — | 83.85 µs · 4608B · 1017a |
| typed-array-sum | 7.343 µs · 0B · 0a | 7.347 µs · 0B · 0a | 256.2 ns · 0B · 0a | 28.7× | 264.9 ns · 8B · 1a | 1.87 ns · 0B · 0a | 46.32 µs · 94208B · 513a | 8.417 µs · 3999B · 15a | 27.39 µs · 2080B · 238a | 17.23 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111 µs · 18424B · 2046a | 111.7 µs · 18424B · 2046a | 1.324 µs · 18424B · 2046a | 83.8× | — | 356.2 ns · 0B · 0a | 102.7 µs · 107608B · 2188a | — | 983.5 µs · 447249B · 12735a | 432.3 µs · 199937B · 7772a | — | 102 µs · 13546B · 1317a |

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
