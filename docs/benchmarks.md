# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.982 µs · 0B · 0a | 9.004 µs · 0B · 0a | 5.37 µs · 0B · 0a | 1.67× | — | 2.187 µs · 1024B · 128a | 39.2 µs · 96288B · 388a | 17.15 µs · 14376B · 256a | 71.41 µs · 78016B · 770a | 14.36 µs · 5712B · 266a | — | 30.97 µs · 2308B · 276a |
| binary-trees | 1.978 ms · 768B · 8a | 1.979 ms · 768B · 8a | 599.9 µs · 768B · 8a | 3.3× | — | 295.4 µs · 201936B · 8414a | 3.771 ms · 1468185B · 62877a | 10.66 ms · 6995672B · 92703a | 9.033 ms · 5198698B · 69052a | 27.89 ms · 19457578B · 280714a | — | 28.67 ms · 15128177B · 446106a |
| branch-tree | 1.309 µs · 0B · 0a | 19.76 ns · 0B · 0a | 88.82 ns · 0B · 0a | 14.7× | 625.1 ns · 16B · 1a | 160.4 ns · 0B · 0a | 47.72 µs · 95384B · 660a | 15.31 µs · 2463B · 9a | 26.76 µs · 1992B · 196a | 25.57 µs · 2168B · 203a | — | 20.74 µs · 1640B · 219a |
| closure-counter | 5.562 µs · 64B · 2a | 5.545 µs · 64B · 2a | 900.3 ns · 64B · 2a | 6.18× | — | 40.52 ns · 0B · 0a | 44.06 µs · 92272B · 261a | 13.19 µs · 151B · 3a | 19.28 µs · 1264B · 13a | 80.78 µs · 58312B · 659a | — | 88.18 µs · 35629B · 922a |
| coroutine-gen | 79.28 µs · 128B · 2a | 79.08 µs · 128B · 2a | 79.34 µs · 128B · 2a | 0.999× | — | 43.73 µs · 208B · 7a | — | 330.9 µs · 149936B · 649a | 608.2 µs · 330650B · 6719a | 198.2 µs · 91800B · 3646a | — | — |
| fannkuch | 829.7 µs · 34608B · 1442a | 815.4 µs · 34608B · 1442a | 216.6 µs · 34608B · 1442a | 3.83× | — | 35.97 µs · 17280B · 720a | 1.994 ms · 456579B · 25621a | 1.55 ms · 443136B · 1460a | 19.5 ms · 14005353B · 164946a | 3.385 ms · 1367679B · 16944a | — | 4.743 ms · 2501443B · 88439a |
| fnv1a64 | 125.2 µs · 16400B · 2049a | 123.7 µs · 16400B · 2049a | 1.158 µs · 16400B · 2049a | 108× | — | 903.1 ns · 0B · 0a | 164.3 µs · 120808B · 3838a | — | 1.391 ms · 608252B · 18475a | 379 µs · 162921B · 7429a | — | 153.8 µs · 17282B · 2074a |
| host-call | 464.8 µs · 41752B · 3080a | 476.6 µs · 41752B · 3080a | 477.4 µs · 41752B · 3080a | 0.974× | — | 3.312 µs · 0B · 0a | 182.5 µs · 139264B · 3073a | 128.7 µs · 22536B · 1079a | 774 µs · 158048B · 14585a | 102.9 µs · 14920B · 1799a | — | 554 µs · 340640B · 9231a |
| i64-wide-fib | 28.75 ms · 2913456B · 364178a | 21.02 ms · 1942304B · 242785a | 680 µs · 16B · 1a | 42.3× | — | 239.4 µs · 0B · 0a | 23.8 µs · 90520B · 52a | — | 151.8 ms · 64193304B · 1942305a | 352.3 ms · 253473040B · 4248843a | — | 123.2 ms · 98686672B · 2138708a |
| indirect-recursive-fib | 1.19 ms · 0B · 0a | 1.188 ms · 0B · 0a | 60.51 µs · 0B · 0a | 19.7× | 99.12 µs · 8B · 1a | 35.05 µs · 0B · 0a | 2.075 ms · 319345B · 28655a | 1.946 ms · 704B · 2a | 2.564 ms · 4680B · 39a | 11.86 ms · 10158190B · 109494a | — | 24.06 ms · 13061274B · 394061a |
| iterative-fib | 1.137 µs · 0B · 0a | 1.072 µs · 0B · 0a | 84.42 ns · 0B · 0a | 13.5× | 84.36 ns · 8B · 1a | 12.42 ns · 0B · 0a | 24.42 µs · 90592B · 61a | 1.159 µs · 160B · 0a | 4.219 µs · 368B · 20a | 6.032 µs · 2448B · 88a | — | 6.139 µs · 944B · 81a |
| mandelbrot | 293.3 µs · 0B · 0a | 248 µs · 0B · 0a | 11.23 µs · 0B · 0a | 26.1× | — | 6.321 µs · 0B · 0a | 925.1 µs · 292345B · 25008a | 486.9 µs · 169408B · 661a | 8.252 ms · 5558661B · 104191a | 1.448 ms · 324978B · 23643a | — | 1.525 ms · 365399B · 22513a |
| map-ops | 70.61 µs · 8816B · 3a | 70.53 µs · 8816B · 3a | 11.07 µs · 8816B · 3a | 6.38× | — | 10.94 µs · 4904B · 3a | — | 280 µs · 114327B · 198a | 376.4 µs · 66728B · 4078a | — | — | 1.1 ms · 501519B · 15813a |
| mat-mul | 376.7 µs · 6216B · 6a | 372.6 µs · 6216B · 6a | 8.788 µs · 6216B · 6a | 42.9× | — | 5.59 µs · 6144B · 3a | 1.359 ms · 431715B · 34871a | 665.8 µs · 143168B · 478a | 1.212 ms · 61304B · 537a | 1.459 ms · 90712B · 9350a | — | 921.1 µs · 36613B · 6036a |
| n-body | 614.2 µs · 504B · 14a | 583.2 µs · 504B · 14a | 18.59 µs · 504B · 14a | 33× | — | 7.358 µs · 0B · 0a | 1.462 ms · 386897B · 35892a | 1.21 ms · 258913B · 2002a | 3.233 ms · 658435B · 81545a | 2.405 ms · 382714B · 34975a | — | 1.754 ms · 445459B · 20669a |
| n-queens | 432.4 µs · 120B · 6a | 431.1 µs · 120B · 6a | 31.53 µs · 120B · 6a | 13.7× | — | 6.018 µs · 0B · 0a | 926.1 µs · 234866B · 17784a | 568.9 µs · 14423B · 63a | 1.099 ms · 7424B · 35a | 1.399 ms · 410346B · 5147a | — | 2.006 ms · 766004B · 33371a |
| naive-checksum | 252.7 µs · 0B · 0a | 141.1 µs · 0B · 0a | 8.737 µs · 0B · 0a | 28.9× | — | 2.759 µs · 0B · 0a | 611.1 µs · 235753B · 18206a | 479 µs · 174768B · 682a | 862.5 µs · 155640B · 19402a | 771.4 µs · 163816B · 20409a | — | 312.7 µs · 8977B · 2071a |
| permutation-flips | 129 µs · 7680B · 128a | 128.6 µs · 7680B · 128a | 16.9 µs · 7680B · 128a | 7.63× | — | 1.877 ns · 0B · 0a | 571.2 µs · 292857B · 9856a | 234 µs · 78808B · 451a | 527.2 µs · 122504B · 765a | 491.1 µs · 111976B · 2368a | — | 984.7 ns · 528B · 13a |
| recursive-fib-20 | 712.4 µs · 0B · 0a | 721.4 µs · 0B · 0a | 47.3 µs · 0B · 0a | 15.1× | 42.74 µs · 8B · 1a | 21.45 µs · 0B · 0a | 1.967 ms · 319345B · 28655a | 2.244 ms · 703B · 2a | 2.966 ms · 4680B · 39a | 11.65 ms · 9807915B · 109494a | — | 10.8 ms · 8302721B · 192848a |
| recursive-fib-35 | 966.9 ms · 0B · 0a | 979.8 ms · 0B · 0a | 63.91 ms · 0B · 0a | 15.1× | 58.23 ms · 8B · 1a | 29.14 ms · 0B · 0a | 2.57 s · 312797456B · 39088168a | 2.993 s · 971008B · 3793a | 4.046 s · 375360B · 46373a | 16.05 s · 13378018704B · 149349975a | — | — |
| sieve | 20.46 µs · 1048B · 2a | 20.38 µs · 1048B · 2a | 941.3 ns · 1048B · 2a | 21.7× | 1.037 µs · 8B · 1a | 371.9 ns · 0B · 0a | 141.8 µs · 122504B · 1611a | 50.98 µs · 18416B · 44a | 84.34 µs · 1872B · 25a | 76.91 µs · 5704B · 30a | — | 81.39 µs · 4368B · 795a |
| sort-stress | 505.6 µs · 5136B · 512a | 511.1 µs · 5136B · 512a | 12.69 µs · 5136B · 512a | 39.9× | — | 5.33 µs · 1024B · 2a | 1.258 ms · 270946B · 19965a | 842.2 µs · 36567B · 365a | 1.784 ms · 49104B · 3621a | 1.776 ms · 23448B · 2034a | — | 923.9 µs · 17473B · 1708a |
| spectral-norm | 543.6 µs · 648B · 6a | 460.6 µs · 648B · 6a | 8.389 µs · 648B · 6a | 64.8× | — | 7.027 µs · 576B · 3a | 2.014 ms · 571372B · 54822a | 1.08 ms · 234784B · 925a | 2.432 ms · 275338B · 33729a | 4.657 ms · 2457135B · 52718a | — | 4.075 ms · 2448018B · 74233a |
| string-build | 718 µs · 85408B · 4107a | 714.2 µs · 85408B · 4107a | 493.9 µs · 85408B · 4107a | 1.45× | — | 370.6 µs · 855890B · 5001a | 2.461 ms · 2211274B · 36101a | 2.328 ms · 1078111B · 13859a | 3.146 ms · 1147385B · 26883a | 2.44 ms · 1326242B · 24469a | — | 3.089 ms · 2372790B · 41432a |
| string-compare | 1.087 ms · 21136B · 1284a | 1.084 ms · 21136B · 1284a | 221.5 µs · 21136B · 1284a | 4.91× | — | 63.51 µs · 9632B · 1585a | 1.656 ms · 296281B · 20528a | 1.125 ms · 59712B · 3431a | 2.318 ms · 50744B · 3848a | 1.781 ms · 209521B · 5520a | — | 2.354 ms · 503613B · 31846a |
| struct-tree-walk | 286.6 µs · 768B · 8a | 284.8 µs · 768B · 8a | 107.6 µs · 768B · 8a | 2.66× | — | 1.875 ns · 0B · 0a | 666.4 µs · 458378B · 5114a | 1.38 ms · 818512B · 11253a | 1.101 ms · 558960B · 6149a | 3.527 ms · 2570668B · 34797a | — | 1.173 µs · 600B · 17a |
| tail-ping-pong | 29.5 µs · 0B · 0a | 29.63 µs · 0B · 0a | 488.6 ns · 0B · 0a | 60.4× | — | 890.7 ns · 0B · 0a | 109.5 µs · 106112B · 2001a | 35.59 µs · 21856B · 85a | 111.1 µs · 13984B · 1726a | 114.7 µs · 14280B · 1731a | — | 546.4 µs · 348834B · 9016a |
| tail-sum | 29.34 µs · 0B · 0a | 29.54 µs · 0B · 0a | 676.9 ns · 0B · 0a | 43.3× | — | 302.6 ns · 0B · 0a | 109.8 µs · 106112B · 2001a | 35.7 µs · 21856B · 85a | 110.7 µs · 13984B · 1726a | 114.7 µs · 14280B · 1731a | — | 84.47 µs · 4608B · 1017a |
| typed-array-sum | 7.344 µs · 0B · 0a | 7.338 µs · 0B · 0a | 265.9 ns · 0B · 0a | 27.6× | 265.3 ns · 8B · 1a | 1.879 ns · 0B · 0a | 46.26 µs · 94208B · 513a | 8.299 µs · 4000B · 15a | 26.94 µs · 2080B · 238a | 17.1 µs · 2496B · 246a | — | — |
| xorshift-i64 | 114.5 µs · 18424B · 2046a | 114.4 µs · 18424B · 2046a | 1.326 µs · 18424B · 2046a | 86.3× | — | 356.1 ns · 0B · 0a | 103.5 µs · 107608B · 2188a | — | 964.3 µs · 447256B · 12736a | 425.8 µs · 199937B · 7772a | — | 101.2 µs · 13546B · 1317a |

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
