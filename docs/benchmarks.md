# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.883 µs · 0B · 0a | 8.841 µs · 0B · 0a | 5.31 µs · 0B · 0a | 1.67× | — | 2.194 µs · 1024B · 128a | 37.41 µs · 96289B · 388a | 17.12 µs · 14376B · 256a | 69.27 µs · 78016B · 770a | 14.41 µs · 5712B · 266a | — | 30.95 µs · 2308B · 276a |
| binary-trees | 1.969 ms · 768B · 8a | 1.969 ms · 768B · 8a | 591.6 µs · 768B · 8a | 3.33× | — | 290.6 µs · 201936B · 8414a | 3.931 ms · 1468185B · 62877a | 10.63 ms · 6995691B · 92703a | 8.945 ms · 5198700B · 69052a | 27.58 ms · 19457612B · 280714a | — | 28.91 ms · 15128182B · 446106a |
| branch-tree | 1.321 µs · 0B · 0a | 18.61 ns · 0B · 0a | 93.46 ns · 0B · 0a | 14.1× | 624 ns · 16B · 1a | 160.8 ns · 0B · 0a | 47.98 µs · 95384B · 660a | 15.85 µs · 2463B · 9a | 27.32 µs · 1992B · 196a | 25.11 µs · 2168B · 203a | — | 20.74 µs · 1640B · 219a |
| closure-counter | 5.516 µs · 64B · 2a | 5.492 µs · 64B · 2a | 907.9 ns · 64B · 2a | 6.08× | — | 40.52 ns · 0B · 0a | 39.23 µs · 92272B · 261a | 13.06 µs · 151B · 3a | 18.93 µs · 1264B · 13a | 78.02 µs · 58312B · 659a | — | 90.41 µs · 35628B · 922a |
| coroutine-gen | 79.17 µs · 128B · 2a | 79.35 µs · 128B · 2a | 79.23 µs · 128B · 2a | 0.999× | — | 42.16 µs · 208B · 7a | — | 327.5 µs · 149936B · 649a | 597.6 µs · 330649B · 6719a | 200.5 µs · 91800B · 3646a | — | — |
| fannkuch | 827.3 µs · 34608B · 1442a | 813.9 µs · 34608B · 1442a | 217.5 µs · 34608B · 1442a | 3.8× | — | 35.38 µs · 17280B · 720a | 2.007 ms · 456578B · 25621a | 1.531 ms · 443136B · 1460a | 18.89 ms · 14005350B · 164946a | 3.356 ms · 1367677B · 16944a | — | 4.798 ms · 2501445B · 88439a |
| fnv1a64 | 125.1 µs · 16400B · 2049a | 123.5 µs · 16400B · 2049a | 1.137 µs · 16400B · 2049a | 110× | — | 902.6 ns · 0B · 0a | 161 µs · 120808B · 3838a | — | 1.453 ms · 608259B · 18475a | 388.6 µs · 162921B · 7429a | — | 152.9 µs · 17282B · 2074a |
| host-call | 462.5 µs · 41752B · 3080a | 472.9 µs · 41752B · 3080a | 474.3 µs · 41752B · 3080a | 0.975× | — | 3.312 µs · 0B · 0a | 178.6 µs · 139264B · 3073a | 127.8 µs · 22536B · 1079a | 800.8 µs · 158049B · 14585a | 104.5 µs · 14920B · 1799a | — | 565.2 µs · 340639B · 9231a |
| i64-wide-fib | 28.08 ms · 2913456B · 364178a | 20.71 ms · 1942304B · 242785a | 680.3 µs · 16B · 1a | 41.3× | — | 239.2 µs · 0B · 0a | 23.19 µs · 90520B · 52a | — | 152.6 ms · 64193288B · 1942305a | 347.9 ms · 253472624B · 4248839a | — | 129.4 ms · 98686602B · 2138708a |
| indirect-recursive-fib | 1.191 ms · 0B · 0a | 1.187 ms · 0B · 0a | 60.51 µs · 0B · 0a | 19.7× | 100.4 µs · 8B · 1a | 34.8 µs · 0B · 0a | 2.072 ms · 319345B · 28655a | 1.947 ms · 703B · 2a | 2.529 ms · 4680B · 39a | 11.23 ms · 10158181B · 109494a | — | 25.09 ms · 13061326B · 394061a |
| iterative-fib | 1.123 µs · 0B · 0a | 1.066 µs · 0B · 0a | 82.27 ns · 0B · 0a | 13.7× | 84.38 ns · 8B · 1a | 12.43 ns · 0B · 0a | 23.13 µs · 90592B · 61a | 1.151 µs · 160B · 0a | 4.245 µs · 368B · 20a | 6.099 µs · 2448B · 88a | — | 6.178 µs · 944B · 81a |
| mandelbrot | 290.7 µs · 0B · 0a | 248.9 µs · 0B · 0a | 11.26 µs · 0B · 0a | 25.8× | — | 6.342 µs · 0B · 0a | 970.5 µs · 292346B · 25008a | 475.7 µs · 169407B · 661a | 7.947 ms · 5558644B · 104191a | 1.494 ms · 324978B · 23643a | — | 1.532 ms · 365401B · 22513a |
| map-ops | 70.16 µs · 8816B · 3a | 70.07 µs · 8816B · 3a | 10.59 µs · 8816B · 3a | 6.63× | — | 10.78 µs · 4904B · 3a | — | 277.2 µs · 114328B · 198a | 381.8 µs · 66728B · 4078a | — | — | 1.128 ms · 501519B · 15813a |
| mat-mul | 376.3 µs · 6216B · 6a | 376.4 µs · 6216B · 6a | 8.494 µs · 6216B · 6a | 44.3× | — | 5.365 µs · 6144B · 3a | 1.36 ms · 431714B · 34871a | 663.3 µs · 143168B · 478a | 1.207 ms · 61304B · 537a | 1.45 ms · 90713B · 9350a | — | 926.2 µs · 36616B · 6036a |
| n-body | 613.5 µs · 504B · 14a | 583.9 µs · 504B · 14a | 18.77 µs · 504B · 14a | 32.7× | — | 7.363 µs · 0B · 0a | 1.494 ms · 386898B · 35892a | 1.214 ms · 258913B · 2002a | 3.374 ms · 658435B · 81545a | 2.404 ms · 382715B · 34975a | — | 1.739 ms · 445461B · 20669a |
| n-queens | 432.3 µs · 120B · 6a | 430.1 µs · 120B · 6a | 31.59 µs · 120B · 6a | 13.7× | — | 6.004 µs · 0B · 0a | 938.3 µs · 234865B · 17784a | 569.6 µs · 14423B · 63a | 1.102 ms · 7424B · 35a | 1.415 ms · 410346B · 5147a | — | 1.989 ms · 766004B · 33371a |
| naive-checksum | 249.8 µs · 0B · 0a | 142.3 µs · 0B · 0a | 8.742 µs · 0B · 0a | 28.6× | — | 2.759 µs · 0B · 0a | 618.9 µs · 235753B · 18206a | 475 µs · 174768B · 682a | 890.1 µs · 155640B · 19402a | 789.3 µs · 163817B · 20409a | — | 316.5 µs · 8978B · 2071a |
| permutation-flips | 128.8 µs · 7680B · 128a | 128.9 µs · 7680B · 128a | 16.68 µs · 7680B · 128a | 7.72× | — | 1.88 ns · 0B · 0a | 564.2 µs · 292858B · 9856a | 236.5 µs · 78808B · 451a | 522.7 µs · 122504B · 765a | 485 µs · 111976B · 2368a | — | 989.2 ns · 528B · 13a |
| recursive-fib-20 | 710.7 µs · 0B · 0a | 722 µs · 0B · 0a | 47.49 µs · 0B · 0a | 15× | 42.73 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.923 ms · 319345B · 28655a | 2.215 ms · 704B · 2a | 2.981 ms · 4680B · 39a | 11.33 ms · 9807921B · 109494a | — | 10.58 ms · 8302675B · 192847a |
| recursive-fib-35 | 968 ms · 0B · 0a | 977.3 ms · 0B · 0a | 64.03 ms · 0B · 0a | 15.1× | 58.22 ms · 8B · 1a | 29.22 ms · 0B · 0a | 2.639 s · 312797504B · 39088169a | 2.962 s · 971008B · 3793a | 4.07 s · 375360B · 46373a | 15.23 s · 13378014976B · 149349947a | — | — |
| sieve | 20.36 µs · 1048B · 2a | 20.31 µs · 1048B · 2a | 900.7 ns · 1048B · 2a | 22.6× | 1.008 µs · 8B · 1a | 371.9 ns · 0B · 0a | 134.6 µs · 122504B · 1611a | 49.66 µs · 18416B · 44a | 83.85 µs · 1872B · 25a | 76.64 µs · 5704B · 30a | — | 81.72 µs · 4368B · 795a |
| sort-stress | 505.5 µs · 5136B · 512a | 509.3 µs · 5136B · 512a | 12.85 µs · 5136B · 512a | 39.4× | — | 5.328 µs · 1024B · 2a | 1.297 ms · 270945B · 19965a | 855.7 µs · 36567B · 365a | 1.801 ms · 49104B · 3621a | 1.767 ms · 23448B · 2034a | — | 924.3 µs · 17473B · 1708a |
| spectral-norm | 532.4 µs · 648B · 6a | 459.9 µs · 648B · 6a | 8.4 µs · 648B · 6a | 63.4× | — | 7.054 µs · 576B · 3a | 2.086 ms · 571371B · 54822a | 1.074 ms · 234784B · 925a | 2.519 ms · 275337B · 33729a | 4.673 ms · 2457133B · 52718a | — | 4.247 ms · 2448019B · 74233a |
| string-build | 741.4 µs · 85408B · 4107a | 732 µs · 85408B · 4107a | 605.8 µs · 85408B · 4107a | 1.22× | — | 358.1 µs · 855890B · 5001a | 2.379 ms · 2211273B · 36101a | 2.331 ms · 1078109B · 13859a | 3.112 ms · 1147386B · 26883a | 2.44 ms · 1326242B · 24469a | — | 3.103 ms · 2372802B · 41432a |
| string-compare | 1.098 ms · 21136B · 1284a | 1.09 ms · 21136B · 1284a | 222 µs · 21136B · 1284a | 4.95× | — | 64.98 µs · 9632B · 1585a | 1.681 ms · 296281B · 20528a | 1.147 ms · 59712B · 3431a | 2.322 ms · 50744B · 3848a | 1.847 ms · 209520B · 5520a | — | 2.416 ms · 503612B · 31846a |
| struct-tree-walk | 282.1 µs · 768B · 8a | 282.1 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.62× | — | 1.899 ns · 0B · 0a | 634.2 µs · 458379B · 5114a | 1.336 ms · 818511B · 11253a | 1.079 ms · 558960B · 6149a | 3.396 ms · 2570665B · 34797a | — | 1.22 µs · 600B · 17a |
| tail-ping-pong | 29.42 µs · 0B · 0a | 29.36 µs · 0B · 0a | 475.7 ns · 0B · 0a | 61.9× | — | 890.6 ns · 0B · 0a | 105.7 µs · 106112B · 2001a | 35.22 µs · 21856B · 85a | 114.6 µs · 13984B · 1726a | 119.2 µs · 14280B · 1731a | — | 555.9 µs · 348834B · 9016a |
| tail-sum | 29.35 µs · 0B · 0a | 29.41 µs · 0B · 0a | 679.6 ns · 0B · 0a | 43.2× | — | 302.5 ns · 0B · 0a | 107.5 µs · 106112B · 2001a | 35.56 µs · 21856B · 85a | 114.3 µs · 13984B · 1726a | 119.4 µs · 14280B · 1731a | — | 84.94 µs · 4608B · 1017a |
| typed-array-sum | 7.36 µs · 0B · 0a | 7.371 µs · 0B · 0a | 263.1 ns · 0B · 0a | 28× | 261 ns · 8B · 1a | 1.874 ns · 0B · 0a | 43.81 µs · 94208B · 513a | 8.217 µs · 4000B · 15a | 27.88 µs · 2080B · 238a | 17.5 µs · 2496B · 246a | — | — |
| xorshift-i64 | 114.1 µs · 18424B · 2046a | 115.5 µs · 18424B · 2046a | 1.296 µs · 18424B · 2046a | 88.1× | — | 356.2 ns · 0B · 0a | 100.3 µs · 107608B · 2188a | — | 983.2 µs · 447249B · 12735a | 430.4 µs · 199938B · 7772a | — | 102.6 µs · 13545B · 1317a |

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
