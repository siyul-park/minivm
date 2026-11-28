# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.286 µs · 0B · 0a | 8.234 µs · 0B · 0a | 5.404 µs · 0B · 0a | 1.53× | — | 2.263 µs · 1024B · 128a | 38 µs · 96288B · 388a | 17.2 µs · 14376B · 256a | 71.08 µs · 78016B · 770a | 14.55 µs · 5712B · 266a | — | 31.35 µs · 2308B · 276a |
| binary-trees | 1.674 ms · 768B · 8a | 1.695 ms · 768B · 8a | 559.2 µs · 768B · 8a | 2.99× | — | 300.4 µs · 201936B · 8414a | 3.961 ms · 1468245B · 62877a | 10.64 ms · 6995685B · 92703a | 8.94 ms · 5198700B · 69052a | 27.96 ms · 19457585B · 280714a | — | 29.01 ms · 15128168B · 446106a |
| branch-tree | 941.9 ns · 0B · 0a | 20.78 ns · 0B · 0a | 87.32 ns · 0B · 0a | 10.8× | 627.5 ns · 16B · 1a | 161.1 ns · 0B · 0a | 46.58 µs · 95384B · 660a | 15.15 µs · 2464B · 9a | 26.46 µs · 1992B · 196a | 24.5 µs · 2168B · 203a | — | 20.71 µs · 1640B · 219a |
| closure-counter | 5.206 µs · 64B · 2a | 5.237 µs · 64B · 2a | 919.2 ns · 64B · 2a | 5.66× | — | 40.56 ns · 0B · 0a | 41.1 µs · 92272B · 261a | 13 µs · 151B · 3a | 19.32 µs · 1264B · 13a | 82.67 µs · 58312B · 659a | — | 91.63 µs · 35629B · 922a |
| coroutine-gen | 66.75 µs · 128B · 2a | 66.36 µs · 128B · 2a | 66.27 µs · 128B · 2a | 1.01× | — | 42.32 µs · 208B · 7a | — | 336.2 µs · 149936B · 649a | 601.9 µs · 330650B · 6719a | 203.2 µs · 91800B · 3646a | — | — |
| fannkuch | 658.2 µs · 34608B · 1442a | 634.2 µs · 34608B · 1442a | 213.7 µs · 34608B · 1442a | 3.08× | — | 35.79 µs · 17280B · 720a | 2.021 ms · 456579B · 25621a | 1.549 ms · 443136B · 1460a | 19.64 ms · 14005347B · 164946a | 3.409 ms · 1367677B · 16944a | — | 4.733 ms · 2501436B · 88439a |
| fnv1a64 | 114.1 µs · 16400B · 2049a | 115.2 µs · 16400B · 2049a | 1.15 µs · 16400B · 2049a | 99.2× | — | 903.4 ns · 0B · 0a | 164.2 µs · 120808B · 3838a | — | 1.478 ms · 608257B · 18475a | 397.5 µs · 162921B · 7429a | — | 153.4 µs · 17282B · 2074a |
| host-call | 450.7 µs · 41752B · 3080a | 466.4 µs · 41752B · 3080a | 465.2 µs · 41752B · 3080a | 0.969× | — | 3.319 µs · 0B · 0a | 183.3 µs · 139264B · 3073a | 131.1 µs · 22536B · 1079a | 808.7 µs · 158049B · 14585a | 109.1 µs · 14920B · 1799a | — | 593.3 µs · 340638B · 9231a |
| i64-wide-fib | 26.96 ms · 2913440B · 364178a | 20.09 ms · 1942304B · 242785a | 681.3 µs · 16B · 1a | 39.6× | — | 239.3 µs · 0B · 0a | 23.09 µs · 90520B · 52a | — | 162.7 ms · 64193376B · 1942306a | 351.3 ms · 253472800B · 4248841a | — | 131.4 ms · 98686677B · 2138708a |
| indirect-recursive-fib | 1.021 ms · 0B · 0a | 1.019 ms · 0B · 0a | 60.6 µs · 0B · 0a | 16.9× | 99.22 µs · 8B · 1a | 35.4 µs · 0B · 0a | 2.068 ms · 319345B · 28655a | 1.946 ms · 704B · 2a | 2.568 ms · 4680B · 39a | 11.81 ms · 10158187B · 109494a | — | 24.23 ms · 13061304B · 394061a |
| iterative-fib | 890.8 ns · 0B · 0a | 879.7 ns · 0B · 0a | 75.27 ns · 0B · 0a | 11.8× | 85.69 ns · 8B · 1a | 12.5 ns · 0B · 0a | 25.71 µs · 90592B · 61a | 1.156 µs · 160B · 0a | 4.262 µs · 368B · 20a | 6.212 µs · 2448B · 88a | — | 6.156 µs · 944B · 81a |
| mandelbrot | 228.2 µs · 0B · 0a | 191.5 µs · 0B · 0a | 11.25 µs · 0B · 0a | 20.3× | — | 6.332 µs · 0B · 0a | 974.3 µs · 292345B · 25008a | 488.9 µs · 169408B · 661a | 8.417 ms · 5558656B · 104191a | 1.511 ms · 324978B · 23643a | — | 1.546 ms · 365401B · 22513a |
| map-churn | 2.517 ms · 786608B · 49a | 2.523 ms · 786608B · 49a | 682.7 µs · 786608B · 49a | 3.69× | — | 692.7 µs · 19024B · 5a | — | 11.67 ms · 7213821B · 6563a | 15.91 ms · 2369293B · 176707a | — | — | 53.06 ms · 24395138B · 725811a |
| map-iter | 3.449 ms · 249040B · 24645a | 3.444 ms · 249040B · 24645a | 3.396 ms · 249040B · 24645a | 1.02× | — | 540.8 µs · 19024B · 5a | — | 12.84 ms · 6177870B · 26325a | 23.19 ms · 13957101B · 201529a | — | — | 6.045 ms · 972975B · 93725a |
| map-ops | 61.44 µs · 12976B · 4a | 61.48 µs · 12976B · 4a | 12.01 µs · 12976B · 4a | 5.12× | — | 11.26 µs · 4904B · 3a | — | 284.1 µs · 114328B · 198a | 393.9 µs · 66728B · 4078a | — | — | 1.186 ms · 501525B · 15813a |
| map-string | 6.502 ms · 174184B · 9244a | 6.508 ms · 174184B · 9244a | 2.114 ms · 647864B · 13256a | 3.08× | — | 1.123 ms · 52632B · 2112a | 8.337 ms · 1463604B · 126292a | 9.514 ms · 967178B · 9917a | 16.18 ms · 590599B · 59508a | 8.888 ms · 1037343B · 80210a | — | 9.36 ms · 1281256B · 94104a |
| mat-mul | 314.1 µs · 6216B · 6a | 316.4 µs · 6216B · 6a | 8.852 µs · 6216B · 6a | 35.5× | — | 5.605 µs · 6144B · 3a | 1.369 ms · 431714B · 34871a | 680.2 µs · 143168B · 478a | 1.212 ms · 61304B · 537a | 1.47 ms · 90712B · 9350a | — | 927.5 µs · 36616B · 6036a |
| n-body | 501.2 µs · 504B · 14a | 415.8 µs · 504B · 14a | 19.12 µs · 504B · 14a | 26.2× | — | 7.368 µs · 0B · 0a | 1.512 ms · 386897B · 35892a | 1.222 ms · 258913B · 2002a | 3.415 ms · 658436B · 81545a | 2.456 ms · 382715B · 34975a | — | 1.769 ms · 445461B · 20669a |
| n-queens | 274.4 µs · 120B · 6a | 275.2 µs · 120B · 6a | 31.84 µs · 120B · 6a | 8.62× | — | 6.034 µs · 0B · 0a | 956.7 µs · 234865B · 17784a | 567 µs · 14423B · 63a | 1.109 ms · 7424B · 35a | 1.432 ms · 410346B · 5147a | — | 2.038 ms · 766004B · 33371a |
| naive-checksum | 188.8 µs · 0B · 0a | 108.4 µs · 0B · 0a | 8.762 µs · 0B · 0a | 21.5× | — | 2.763 µs · 0B · 0a | 624.5 µs · 235753B · 18206a | 480.9 µs · 174768B · 682a | 875.5 µs · 155640B · 19402a | 787.5 µs · 163817B · 20409a | — | 311.2 µs · 8977B · 2071a |
| permutation-flips | 103.9 µs · 7680B · 128a | 103.9 µs · 7680B · 128a | 17.65 µs · 7680B · 128a | 5.89× | — | 1.867 ns · 0B · 0a | 576 µs · 292857B · 9856a | 235.6 µs · 78807B · 451a | 530.7 µs · 122504B · 765a | 491.8 µs · 111976B · 2368a | — | 1.021 µs · 528B · 13a |
| recursive-fib-20 | 636.2 µs · 0B · 0a | 639.9 µs · 0B · 0a | 47.37 µs · 0B · 0a | 13.4× | 42.8 µs · 8B · 1a | 21.58 µs · 0B · 0a | 1.918 ms · 319346B · 28655a | 2.157 ms · 704B · 2a | 2.991 ms · 4680B · 39a | 11.97 ms · 9807907B · 109494a | — | 11.04 ms · 8302688B · 192847a |
| recursive-fib-35 | 870 ms · 0B · 0a | 872.7 ms · 0B · 0a | 64.51 ms · 0B · 0a | 13.5× | 58.27 ms · 8B · 1a | 29.08 ms · 0B · 0a | 2.568 s · 312797296B · 39088167a | 2.947 s · 971008B · 3793a | 4.063 s · 375360B · 46373a | 16.19 s · 13378020112B · 149349996a | — | — |
| sieve | 15.71 µs · 1048B · 2a | 15.12 µs · 1048B · 2a | 974 ns · 1048B · 2a | 16.1× | 1.041 µs · 8B · 1a | 372.4 ns · 0B · 0a | 143 µs · 122504B · 1611a | 51.95 µs · 18416B · 44a | 85.03 µs · 1872B · 25a | 78.18 µs · 5704B · 30a | — | 82.96 µs · 4368B · 795a |
| sort-stress | 391.3 µs · 5136B · 512a | 372.8 µs · 5136B · 512a | 13.04 µs · 5136B · 512a | 30× | — | 5.403 µs · 1024B · 2a | 1.302 ms · 270946B · 19965a | 848.1 µs · 36568B · 365a | 1.805 ms · 49104B · 3621a | 1.78 ms · 23448B · 2034a | — | 926.5 µs · 17474B · 1708a |
| spectral-norm | 425.3 µs · 648B · 6a | 346.5 µs · 648B · 6a | 8.398 µs · 648B · 6a | 50.6× | — | 7.075 µs · 576B · 3a | 2.052 ms · 571370B · 54822a | 1.087 ms · 234784B · 925a | 2.463 ms · 275337B · 33729a | 4.757 ms · 2457134B · 52718a | — | 4.092 ms · 2448018B · 74233a |
| string-build | 603.2 µs · 85408B · 4107a | 592.9 µs · 85408B · 4107a | 551.3 µs · 85408B · 4107a | 1.09× | — | 396.3 µs · 855889B · 5001a | 2.549 ms · 2211278B · 36101a | 2.397 ms · 1078111B · 13859a | 3.225 ms · 1147381B · 26883a | 2.536 ms · 1326243B · 24469a | — | 3.137 ms · 2372781B · 41432a |
| string-compare | 999.6 µs · 21136B · 1284a | 997.5 µs · 21136B · 1284a | 219.3 µs · 21136B · 1284a | 4.56× | — | 64.1 µs · 9632B · 1585a | 1.681 ms · 296282B · 20528a | 1.167 ms · 59712B · 3431a | 2.338 ms · 50744B · 3848a | 1.845 ms · 209521B · 5520a | — | 2.376 ms · 503615B · 31846a |
| struct-tree-walk | 254.2 µs · 768B · 8a | 252.6 µs · 768B · 8a | 102.5 µs · 768B · 8a | 2.48× | — | 1.866 ns · 0B · 0a | 669.3 µs · 458379B · 5114a | 1.386 ms · 818513B · 11253a | 1.1 ms · 558960B · 6149a | 3.563 ms · 2570663B · 34797a | — | 1.19 µs · 600B · 17a |
| tail-ping-pong | 21.72 µs · 0B · 0a | 21.7 µs · 0B · 0a | 480 ns · 0B · 0a | 45.3× | — | 892.3 ns · 0B · 0a | 111.7 µs · 106112B · 2001a | 36.18 µs · 21856B · 85a | 110.7 µs · 13984B · 1726a | 118.7 µs · 14280B · 1731a | — | 559 µs · 348832B · 9016a |
| tail-sum | 21.75 µs · 0B · 0a | 21.85 µs · 0B · 0a | 680 ns · 0B · 0a | 32× | — | 303 ns · 0B · 0a | 110.6 µs · 106112B · 2001a | 36.42 µs · 21856B · 85a | 110.7 µs · 13984B · 1726a | 118.5 µs · 14280B · 1731a | — | 84.5 µs · 4608B · 1017a |
| typed-array-sum | 5.695 µs · 0B · 0a | 5.713 µs · 0B · 0a | 264.7 ns · 0B · 0a | 21.5× | 267.9 ns · 8B · 1a | 1.896 ns · 0B · 0a | 47.17 µs · 94208B · 513a | 8.35 µs · 3999B · 15a | 27.16 µs · 2080B · 238a | 17.34 µs · 2496B · 246a | — | — |
| xorshift-i64 | 107.4 µs · 18424B · 2046a | 103.7 µs · 18424B · 2046a | 1.43 µs · 18424B · 2046a | 75.1× | — | 356.6 ns · 0B · 0a | 107.3 µs · 107608B · 2188a | — | 992.9 µs · 447258B · 12736a | 430.4 µs · 199938B · 7772a | — | 102.2 µs · 13545B · 1317a |

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
