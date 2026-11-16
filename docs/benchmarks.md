# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.994 µs · 0B · 0a | 8.975 µs · 0B · 0a | 5.486 µs · 0B · 0a | 1.64× | — | 2.22 µs · 1024B · 128a | 38.9 µs · 96288B · 388a | 17.6 µs · 14376B · 256a | 70.79 µs · 78016B · 770a | 15.13 µs · 5712B · 266a | — | 30.99 µs · 2308B · 276a |
| binary-trees | 1.973 ms · 768B · 8a | 1.972 ms · 768B · 8a | 593.7 µs · 768B · 8a | 3.32× | — | 287.2 µs · 201936B · 8414a | 3.84 ms · 1468185B · 62877a | 10.47 ms · 6995680B · 92703a | 8.99 ms · 5198695B · 69052a | 27.14 ms · 19457612B · 280714a | — | 29.62 ms · 15128188B · 446106a |
| branch-tree | 1.358 µs · 0B · 0a | 18.7 ns · 0B · 0a | 83.9 ns · 0B · 0a | 16.2× | 625 ns · 16B · 1a | 160.6 ns · 0B · 0a | 45.85 µs · 95384B · 660a | 15.41 µs · 2464B · 9a | 26.42 µs · 1992B · 196a | 24.3 µs · 2168B · 203a | — | 20.62 µs · 1640B · 219a |
| closure-counter | 5.549 µs · 64B · 2a | 5.521 µs · 64B · 2a | 911.9 ns · 64B · 2a | 6.09× | — | 40.51 ns · 0B · 0a | 41.99 µs · 92272B · 261a | 13.11 µs · 151B · 3a | 19.12 µs · 1264B · 13a | 79.28 µs · 58312B · 659a | — | 89.99 µs · 35629B · 922a |
| coroutine-gen | 79.21 µs · 128B · 2a | 78.84 µs · 128B · 2a | 78.9 µs · 128B · 2a | 1× | — | 42.74 µs · 208B · 7a | — | 334.5 µs · 149936B · 649a | 591.2 µs · 330649B · 6719a | 200.5 µs · 91800B · 3646a | — | — |
| fannkuch | 827 µs · 34608B · 1442a | 814.4 µs · 34608B · 1442a | 220 µs · 34608B · 1442a | 3.76× | — | 35.43 µs · 17280B · 720a | 2.044 ms · 456578B · 25621a | 1.524 ms · 443136B · 1460a | 19.1 ms · 14005329B · 164946a | 3.374 ms · 1367676B · 16944a | — | 4.739 ms · 2501441B · 88439a |
| fnv1a64 | 123.7 µs · 16400B · 2049a | 122.9 µs · 16400B · 2049a | 1.15 µs · 16400B · 2049a | 108× | — | 902.6 ns · 0B · 0a | 161.5 µs · 120808B · 3838a | — | 1.489 ms · 608254B · 18475a | 397.4 µs · 162921B · 7429a | — | 154.8 µs · 17282B · 2074a |
| host-call | 455.9 µs · 41752B · 3080a | 471.9 µs · 41752B · 3080a | 466.7 µs · 41752B · 3080a | 0.977× | — | 3.312 µs · 0B · 0a | 180.3 µs · 139264B · 3073a | 130.1 µs · 22536B · 1079a | 788.6 µs · 158048B · 14585a | 108.3 µs · 14920B · 1799a | — | 577 µs · 340637B · 9231a |
| i64-wide-fib | 28.32 ms · 2913456B · 364178a | 20.96 ms · 1942304B · 242785a | 681.7 µs · 16B · 1a | 41.5× | — | 239.9 µs · 0B · 0a | 23.02 µs · 90520B · 52a | — | 161 ms · 64193320B · 1942305a | 345.7 ms · 253472672B · 4248841a | — | 129.2 ms · 98686629B · 2138708a |
| indirect-recursive-fib | 1.191 ms · 0B · 0a | 1.189 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.7× | 99.08 µs · 8B · 1a | 35.08 µs · 0B · 0a | 2.084 ms · 319346B · 28655a | 1.95 ms · 703B · 2a | 2.55 ms · 4680B · 39a | 11.57 ms · 10158197B · 109494a | — | 24.1 ms · 13061316B · 394061a |
| iterative-fib | 1.133 µs · 0B · 0a | 1.084 µs · 0B · 0a | 79.17 ns · 0B · 0a | 14.3× | 84.1 ns · 8B · 1a | 12.41 ns · 0B · 0a | 23.88 µs · 90592B · 61a | 1.152 µs · 160B · 0a | 4.25 µs · 368B · 20a | 6.168 µs · 2448B · 88a | — | 6.145 µs · 944B · 81a |
| mandelbrot | 295.7 µs · 0B · 0a | 249.2 µs · 0B · 0a | 11.27 µs · 0B · 0a | 26.2× | — | 6.323 µs · 0B · 0a | 968.3 µs · 292346B · 25008a | 482.1 µs · 169407B · 661a | 8.189 ms · 5558653B · 104191a | 1.522 ms · 324979B · 23643a | — | 1.531 ms · 365400B · 22513a |
| map-ops | 70.56 µs · 8816B · 3a | 70.7 µs · 8816B · 3a | 10.83 µs · 8816B · 3a | 6.51× | — | 10.82 µs · 4904B · 3a | — | 277.7 µs · 114328B · 198a | 386.5 µs · 66728B · 4078a | — | — | 1.132 ms · 501519B · 15813a |
| mat-mul | 375.6 µs · 6216B · 6a | 374.5 µs · 6216B · 6a | 8.769 µs · 6216B · 6a | 42.8× | — | 5.598 µs · 6144B · 3a | 1.352 ms · 431715B · 34871a | 664.2 µs · 143168B · 478a | 1.197 ms · 61304B · 537a | 1.468 ms · 90712B · 9350a | — | 920.3 µs · 36616B · 6036a |
| n-body | 612.4 µs · 504B · 14a | 581.5 µs · 504B · 14a | 19.12 µs · 504B · 14a | 32× | — | 7.356 µs · 0B · 0a | 1.525 ms · 386897B · 35892a | 1.221 ms · 258913B · 2002a | 3.365 ms · 658437B · 81545a | 2.413 ms · 382714B · 34975a | — | 1.732 ms · 445468B · 20669a |
| n-queens | 429.6 µs · 120B · 6a | 430.5 µs · 120B · 6a | 31.54 µs · 120B · 6a | 13.6× | — | 6.022 µs · 0B · 0a | 957.8 µs · 234865B · 17784a | 567.6 µs · 14423B · 63a | 1.097 ms · 7424B · 35a | 1.429 ms · 410346B · 5147a | — | 2.004 ms · 766006B · 33371a |
| naive-checksum | 255.1 µs · 0B · 0a | 142.5 µs · 0B · 0a | 8.723 µs · 0B · 0a | 29.2× | — | 2.759 µs · 0B · 0a | 614.3 µs · 235753B · 18206a | 476.9 µs · 174768B · 682a | 863.8 µs · 155640B · 19402a | 768.3 µs · 163816B · 20409a | — | 311.6 µs · 8977B · 2071a |
| permutation-flips | 127.7 µs · 7680B · 128a | 127.9 µs · 7680B · 128a | 17.3 µs · 7680B · 128a | 7.38× | — | 1.88 ns · 0B · 0a | 560.3 µs · 292857B · 9856a | 234.7 µs · 78808B · 451a | 526.9 µs · 122504B · 765a | 500.3 µs · 111976B · 2368a | — | 1.015 µs · 528B · 13a |
| recursive-fib-20 | 709.8 µs · 0B · 0a | 718.7 µs · 0B · 0a | 47.16 µs · 0B · 0a | 15× | 42.73 µs · 8B · 1a | 21.48 µs · 0B · 0a | 1.933 ms · 319345B · 28655a | 2.162 ms · 704B · 2a | 2.969 ms · 4680B · 39a | 11.49 ms · 9807918B · 109494a | — | 11.08 ms · 8302685B · 192848a |
| recursive-fib-35 | 971.7 ms · 0B · 0a | 980.9 ms · 0B · 0a | 63.96 ms · 0B · 0a | 15.2× | 58.23 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.554 s · 312797360B · 39088167a | 2.952 s · 971008B · 3793a | 4.049 s · 375376B · 46373a | 15.61 s · 13378015856B · 149349955a | — | — |
| sieve | 20.51 µs · 1048B · 2a | 20.4 µs · 1048B · 2a | 922.1 ns · 1048B · 2a | 22.2× | 1.041 µs · 8B · 1a | 371.9 ns · 0B · 0a | 138.2 µs · 122504B · 1611a | 50.77 µs · 18416B · 44a | 83.51 µs · 1872B · 25a | 76.85 µs · 5704B · 30a | — | 81.44 µs · 4368B · 795a |
| sort-stress | 505.3 µs · 5136B · 512a | 507.1 µs · 5136B · 512a | 12.68 µs · 5136B · 512a | 39.8× | — | 5.321 µs · 1024B · 2a | 1.265 ms · 270945B · 19965a | 844.7 µs · 36568B · 365a | 1.794 ms · 49104B · 3621a | 1.77 ms · 23448B · 2034a | — | 923.3 µs · 17474B · 1708a |
| spectral-norm | 540.4 µs · 648B · 6a | 461.1 µs · 648B · 6a | 8.378 µs · 648B · 6a | 64.5× | — | 7.042 µs · 576B · 3a | 2.021 ms · 571370B · 54822a | 1.077 ms · 234784B · 925a | 2.462 ms · 275337B · 33729a | 4.646 ms · 2457136B · 52718a | — | 4.078 ms · 2448004B · 74233a |
| string-build | 722.9 µs · 85408B · 4107a | 714.2 µs · 85408B · 4107a | 494.3 µs · 85408B · 4107a | 1.46× | — | 369.9 µs · 855889B · 5001a | 2.403 ms · 2211274B · 36101a | 2.357 ms · 1078112B · 13859a | 3.141 ms · 1147384B · 26883a | 2.445 ms · 1326243B · 24469a | — | 3.066 ms · 2372770B · 41432a |
| string-compare | 1.087 ms · 21136B · 1284a | 1.087 ms · 21136B · 1284a | 219 µs · 21136B · 1284a | 4.96× | — | 62.9 µs · 9632B · 1585a | 1.651 ms · 296282B · 20528a | 1.124 ms · 59712B · 3431a | 2.333 ms · 50744B · 3848a | 1.832 ms · 209521B · 5520a | — | 2.356 ms · 503615B · 31846a |
| struct-tree-walk | 283 µs · 768B · 8a | 283.6 µs · 768B · 8a | 107.7 µs · 768B · 8a | 2.63× | — | 1.898 ns · 0B · 0a | 662 µs · 458379B · 5114a | 1.357 ms · 818511B · 11253a | 1.075 ms · 558960B · 6149a | 3.453 ms · 2570663B · 34797a | — | 1.151 µs · 600B · 17a |
| tail-ping-pong | 29.54 µs · 0B · 0a | 29.52 µs · 0B · 0a | 479.7 ns · 0B · 0a | 61.6× | — | 890.6 ns · 0B · 0a | 107.3 µs · 106112B · 2001a | 35.34 µs · 21856B · 85a | 110.1 µs · 13984B · 1726a | 117.1 µs · 14280B · 1731a | — | 555.7 µs · 348833B · 9016a |
| tail-sum | 29.22 µs · 0B · 0a | 29.33 µs · 0B · 0a | 672.8 ns · 0B · 0a | 43.4× | — | 302.5 ns · 0B · 0a | 107.5 µs · 106112B · 2001a | 35.35 µs · 21855B · 85a | 109.9 µs · 13984B · 1726a | 116.9 µs · 14280B · 1731a | — | 84.56 µs · 4608B · 1017a |
| typed-array-sum | 7.326 µs · 0B · 0a | 7.346 µs · 0B · 0a | 254.1 ns · 0B · 0a | 28.8× | 266 ns · 8B · 1a | 1.876 ns · 0B · 0a | 45.45 µs · 94208B · 513a | 8.273 µs · 4000B · 15a | 26.54 µs · 2080B · 238a | 17 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112 µs · 18424B · 2046a | 111 µs · 18424B · 2046a | 1.387 µs · 18424B · 2046a | 80.8× | — | 356.2 ns · 0B · 0a | 102.9 µs · 107608B · 2188a | — | 978 µs · 447254B · 12736a | 424.6 µs · 199937B · 7772a | — | 101 µs · 13545B · 1317a |

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
