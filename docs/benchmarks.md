# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.176 µs · 0B · 0a | 9.118 µs · 0B · 0a | 5.428 µs · 0B · 0a | 1.69× | — | 2.229 µs · 1024B · 128a | 41.65 µs · 96289B · 388a | 20.85 µs · 14376B · 256a | 72.45 µs · 78016B · 770a | 14.1 µs · 5712B · 266a | — | 30.73 µs · 2308B · 276a |
| binary-trees | 1.973 ms · 768B · 8a | 1.965 ms · 768B · 8a | 594.3 µs · 768B · 8a | 3.32× | — | 286.7 µs · 201936B · 8414a | 3.775 ms · 1468183B · 62877a | 10.32 ms · 6995680B · 92703a | 8.74 ms · 5198699B · 69052a | 27.01 ms · 19457596B · 280714a | — | 28 ms · 15128186B · 446106a |
| branch-tree | 1.392 µs · 0B · 0a | 19.6 ns · 0B · 0a | 100.2 ns · 0B · 0a | 13.9× | 632.2 ns · 16B · 1a | 160.4 ns · 0B · 0a | 48.73 µs · 95384B · 660a | 15.37 µs · 2464B · 9a | 26.5 µs · 1992B · 196a | 23.85 µs · 2168B · 203a | — | 20.81 µs · 1640B · 219a |
| closure-counter | 5.523 µs · 64B · 2a | 5.534 µs · 64B · 2a | 934 ns · 64B · 2a | 5.91× | — | 40.49 ns · 0B · 0a | 42.88 µs · 92272B · 261a | 12.87 µs · 151B · 3a | 19.25 µs · 1264B · 13a | 79.21 µs · 58312B · 659a | — | 88.41 µs · 35628B · 922a |
| coroutine-gen | 79.41 µs · 128B · 2a | 79.32 µs · 128B · 2a | 78.77 µs · 128B · 2a | 1.01× | — | 42.9 µs · 208B · 7a | — | 335.3 µs · 149936B · 649a | 592.2 µs · 330649B · 6719a | 200.9 µs · 91800B · 3646a | — | — |
| fannkuch | 826.6 µs · 34608B · 1442a | 813 µs · 34608B · 1442a | 221.1 µs · 34608B · 1442a | 3.74× | — | 35.32 µs · 17280B · 720a | 1.993 ms · 456578B · 25621a | 1.563 ms · 443136B · 1460a | 19.01 ms · 14005333B · 164946a | 3.312 ms · 1367676B · 16944a | — | 4.723 ms · 2501450B · 88439a |
| fnv1a64 | 122.2 µs · 16400B · 2049a | 121.4 µs · 16400B · 2049a | 1.157 µs · 16400B · 2049a | 106× | — | 902.6 ns · 0B · 0a | 163.6 µs · 120808B · 3838a | — | 1.417 ms · 608256B · 18475a | 383.8 µs · 162921B · 7429a | — | 153.5 µs · 17282B · 2074a |
| host-call | 452.2 µs · 41752B · 3080a | 468.7 µs · 41752B · 3080a | 467.8 µs · 41752B · 3080a | 0.967× | — | 3.312 µs · 0B · 0a | 176.5 µs · 139264B · 3073a | 129.1 µs · 22536B · 1079a | 789.2 µs · 158048B · 14585a | 108.5 µs · 14920B · 1799a | — | 550.7 µs · 340638B · 9231a |
| i64-wide-fib | 27.89 ms · 2913456B · 364178a | 20.45 ms · 1942304B · 242785a | 681.5 µs · 16B · 1a | 40.9× | — | 239.2 µs · 0B · 0a | 23.02 µs · 90520B · 52a | — | 152.1 ms · 64193440B · 1942306a | 340 ms · 253472960B · 4248841a | — | 124.3 ms · 98686602B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 1.189 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.6× | 98.99 µs · 8B · 1a | 34.78 µs · 0B · 0a | 2.051 ms · 319345B · 28655a | 1.946 ms · 704B · 2a | 2.538 ms · 4680B · 39a | 11.33 ms · 10158193B · 109494a | — | 23.66 ms · 13061353B · 394062a |
| iterative-fib | 1.134 µs · 0B · 0a | 1.072 µs · 0B · 0a | 82.1 ns · 0B · 0a | 13.8× | 85.4 ns · 8B · 1a | 12.47 ns · 0B · 0a | 23.84 µs · 90592B · 61a | 1.166 µs · 160B · 0a | 4.264 µs · 368B · 20a | 6.046 µs · 2448B · 88a | — | 6.172 µs · 944B · 81a |
| mandelbrot | 291.4 µs · 0B · 0a | 249.2 µs · 0B · 0a | 11.28 µs · 0B · 0a | 25.8× | — | 6.319 µs · 0B · 0a | 919 µs · 292346B · 25008a | 482 µs · 169408B · 661a | 7.87 ms · 5558651B · 104191a | 1.456 ms · 324978B · 23643a | — | 1.526 ms · 365394B · 22513a |
| map-ops | 70.69 µs · 8816B · 3a | 70.59 µs · 8816B · 3a | 10.8 µs · 8816B · 3a | 6.55× | — | 10.82 µs · 4904B · 3a | — | 279.3 µs · 114328B · 198a | 373.6 µs · 66728B · 4078a | — | — | 1.093 ms · 501518B · 15813a |
| mat-mul | 376.9 µs · 6216B · 6a | 375.4 µs · 6216B · 6a | 8.668 µs · 6216B · 6a | 43.5× | — | 5.508 µs · 6144B · 3a | 1.325 ms · 431714B · 34871a | 675.4 µs · 143168B · 478a | 1.208 ms · 61304B · 537a | 1.437 ms · 90712B · 9350a | — | 924.5 µs · 36615B · 6036a |
| n-body | 616.4 µs · 504B · 14a | 583 µs · 504B · 14a | 18.86 µs · 504B · 14a | 32.7× | — | 7.357 µs · 0B · 0a | 1.449 ms · 386898B · 35892a | 1.206 ms · 258913B · 2002a | 3.218 ms · 658435B · 81545a | 2.374 ms · 382715B · 34975a | — | 1.759 ms · 445466B · 20669a |
| n-queens | 430.4 µs · 120B · 6a | 429.6 µs · 120B · 6a | 31.7 µs · 120B · 6a | 13.6× | — | 6.021 µs · 0B · 0a | 918.4 µs · 234865B · 17784a | 572.7 µs · 14423B · 63a | 1.094 ms · 7424B · 35a | 1.387 ms · 410346B · 5147a | — | 1.994 ms · 766004B · 33371a |
| naive-checksum | 250.2 µs · 0B · 0a | 142.5 µs · 0B · 0a | 8.713 µs · 0B · 0a | 28.7× | — | 2.759 µs · 0B · 0a | 607.4 µs · 235753B · 18206a | 480 µs · 174768B · 682a | 860.3 µs · 155640B · 19402a | 755.3 µs · 163817B · 20409a | — | 310.7 µs · 8977B · 2071a |
| permutation-flips | 128.8 µs · 7680B · 128a | 128.8 µs · 7680B · 128a | 16.7 µs · 7680B · 128a | 7.71× | — | 1.871 ns · 0B · 0a | 551.5 µs · 292858B · 9856a | 234.2 µs · 78808B · 451a | 521.4 µs · 122504B · 765a | 482.8 µs · 111976B · 2368a | — | 1.003 µs · 528B · 13a |
| recursive-fib-20 | 711.8 µs · 0B · 0a | 719.6 µs · 0B · 0a | 47.17 µs · 0B · 0a | 15.1× | 42.75 µs · 8B · 1a | 21.48 µs · 0B · 0a | 1.905 ms · 319345B · 28655a | 2.326 ms · 704B · 2a | 2.945 ms · 4680B · 39a | 11.3 ms · 9807911B · 109494a | — | 10.54 ms · 8302728B · 192848a |
| recursive-fib-35 | 970.9 ms · 0B · 0a | 977.2 ms · 0B · 0a | 64.81 ms · 0B · 0a | 15× | 58.2 ms · 8B · 1a | 29.16 ms · 0B · 0a | 2.553 s · 312797264B · 39088167a | 2.965 s · 971008B · 3793a | 4.018 s · 375360B · 46373a | 15.25 s · 13378017136B · 149349955a | — | — |
| sieve | 20.38 µs · 1048B · 2a | 20.4 µs · 1048B · 2a | 944.6 ns · 1048B · 2a | 21.6× | 1.02 µs · 8B · 1a | 372 ns · 0B · 0a | 136.7 µs · 122504B · 1611a | 50.48 µs · 18416B · 44a | 84.39 µs · 1872B · 25a | 77.15 µs · 5704B · 30a | — | 80.71 µs · 4368B · 795a |
| sort-stress | 504 µs · 5136B · 512a | 506.8 µs · 5136B · 512a | 12.9 µs · 5136B · 512a | 39.1× | — | 5.352 µs · 1024B · 2a | 1.238 ms · 270945B · 19965a | 857.3 µs · 36567B · 365a | 1.791 ms · 49104B · 3621a | 1.785 ms · 23448B · 2034a | — | 920.8 µs · 17473B · 1708a |
| spectral-norm | 533.2 µs · 648B · 6a | 461.3 µs · 648B · 6a | 8.378 µs · 648B · 6a | 63.6× | — | 7.033 µs · 576B · 3a | 1.963 ms · 571371B · 54822a | 1.077 ms · 234784B · 925a | 2.418 ms · 275337B · 33729a | 4.543 ms · 2457137B · 52718a | — | 4.026 ms · 2448023B · 74233a |
| string-build | 719.1 µs · 85408B · 4107a | 712.7 µs · 85408B · 4107a | 605.1 µs · 85408B · 4107a | 1.19× | — | 369.9 µs · 855890B · 5001a | 2.431 ms · 2211274B · 36101a | 2.347 ms · 1078110B · 13859a | 3.11 ms · 1147384B · 26883a | 2.476 ms · 1326242B · 24469a | — | 3.073 ms · 2372782B · 41432a |
| string-compare | 1.085 ms · 21136B · 1284a | 1.087 ms · 21136B · 1284a | 220.5 µs · 21136B · 1284a | 4.92× | — | 63.27 µs · 9632B · 1585a | 1.65 ms · 296281B · 20528a | 1.138 ms · 59712B · 3431a | 2.331 ms · 50744B · 3848a | 1.814 ms · 209521B · 5520a | — | 2.376 ms · 503610B · 31846a |
| struct-tree-walk | 283 µs · 768B · 8a | 281.7 µs · 768B · 8a | 108 µs · 768B · 8a | 2.62× | — | 1.893 ns · 0B · 0a | 645.9 µs · 458378B · 5114a | 1.326 ms · 818512B · 11253a | 1.061 ms · 558960B · 6149a | 3.411 ms · 2570668B · 34797a | — | 1.179 µs · 600B · 17a |
| tail-ping-pong | 29.37 µs · 0B · 0a | 29.4 µs · 0B · 0a | 484.9 ns · 0B · 0a | 60.6× | — | 890.8 ns · 0B · 0a | 107.5 µs · 106112B · 2001a | 35.93 µs · 21856B · 85a | 112.2 µs · 13984B · 1726a | 115.3 µs · 14280B · 1731a | — | 529.1 µs · 348833B · 9016a |
| tail-sum | 29.35 µs · 0B · 0a | 29.39 µs · 0B · 0a | 680.4 ns · 0B · 0a | 43.1× | — | 302.6 ns · 0B · 0a | 109.2 µs · 106112B · 2001a | 35.82 µs · 21856B · 85a | 112 µs · 13984B · 1726a | 115.4 µs · 14280B · 1731a | — | 83.79 µs · 4608B · 1017a |
| typed-array-sum | 7.37 µs · 0B · 0a | 7.37 µs · 0B · 0a | 263.1 ns · 0B · 0a | 28× | 265.7 ns · 8B · 1a | 1.864 ns · 0B · 0a | 45.35 µs · 94208B · 513a | 8.29 µs · 4000B · 15a | 27.36 µs · 2080B · 238a | 17.22 µs · 2496B · 246a | — | — |
| xorshift-i64 | 109.8 µs · 18424B · 2046a | 109.8 µs · 18424B · 2046a | 1.38 µs · 18424B · 2046a | 79.6× | — | 356.9 ns · 0B · 0a | 100.6 µs · 107608B · 2188a | — | 918 µs · 447253B · 12735a | 411.7 µs · 199938B · 7772a | — | 102 µs · 13545B · 1317a |

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
