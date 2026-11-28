# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.217 µs · 0B · 0a | 8.206 µs · 0B · 0a | 5.317 µs · 0B · 0a | 1.55× | — | 2.227 µs · 1024B · 128a | 37.75 µs · 96288B · 388a | 17.53 µs · 14376B · 256a | 72.7 µs · 78016B · 770a | 14.48 µs · 5712B · 266a | — | 30.94 µs · 2308B · 276a |
| binary-trees | 1.65 ms · 768B · 8a | 1.645 ms · 768B · 8a | 559.4 µs · 768B · 8a | 2.95× | — | 307 µs · 201936B · 8414a | 3.939 ms · 1468182B · 62877a | 10.89 ms · 6995689B · 92703a | 9.323 ms · 5198711B · 69052a | 28.92 ms · 19457597B · 280714a | — | 28.74 ms · 15128176B · 446106a |
| branch-tree | 943.6 ns · 0B · 0a | 19.78 ns · 0B · 0a | 87.31 ns · 0B · 0a | 10.8× | 625.4 ns · 16B · 1a | 160.3 ns · 0B · 0a | 46.62 µs · 95384B · 660a | 15.76 µs · 2464B · 9a | 26.72 µs · 1992B · 196a | 24.16 µs · 2168B · 203a | — | 20.47 µs · 1640B · 219a |
| closure-counter | 5.213 µs · 64B · 2a | 5.141 µs · 64B · 2a | 913.6 ns · 64B · 2a | 5.71× | — | 40.5 ns · 0B · 0a | 43.02 µs · 92273B · 261a | 12.96 µs · 151B · 3a | 19.03 µs · 1264B · 13a | 81.59 µs · 58312B · 659a | — | 89.52 µs · 35629B · 922a |
| coroutine-gen | 58.07 µs · 128B · 2a | 57.74 µs · 128B · 2a | 58.15 µs · 128B · 2a | 0.999× | — | 43.34 µs · 208B · 7a | — | 332.7 µs · 149936B · 649a | 601 µs · 330649B · 6719a | 201.7 µs · 91800B · 3646a | — | — |
| fannkuch | 654.6 µs · 34608B · 1442a | 629.5 µs · 34608B · 1442a | 215.6 µs · 34608B · 1442a | 3.04× | — | 35.76 µs · 17280B · 720a | 2.011 ms · 456579B · 25621a | 1.553 ms · 443136B · 1460a | 19.39 ms · 14005361B · 164946a | 3.355 ms · 1367679B · 16944a | — | 4.763 ms · 2501433B · 88439a |
| fnv1a64 | 115.8 µs · 16400B · 2049a | 115.4 µs · 16400B · 2049a | 1.149 µs · 16400B · 2049a | 101× | — | 902.5 ns · 0B · 0a | 163.2 µs · 120808B · 3838a | — | 1.487 ms · 608255B · 18475a | 398.2 µs · 162921B · 7429a | — | 153.1 µs · 17282B · 2074a |
| host-call | 462.2 µs · 41752B · 3080a | 479.9 µs · 41752B · 3080a | 477 µs · 41752B · 3080a | 0.969× | — | 3.312 µs · 0B · 0a | 185.1 µs · 139264B · 3073a | 130.5 µs · 22536B · 1079a | 796.6 µs · 158048B · 14585a | 105.6 µs · 14920B · 1799a | — | 552.1 µs · 340638B · 9231a |
| i64-wide-fib | 26.81 ms · 2913456B · 364178a | 20.12 ms · 1942304B · 242785a | 681.4 µs · 16B · 1a | 39.3× | — | 239 µs · 0B · 0a | 24.2 µs · 90520B · 52a | — | 153.7 ms · 64193304B · 1942305a | 349.9 ms · 253473104B · 4248843a | — | 122.9 ms · 98686608B · 2138708a |
| indirect-recursive-fib | 971.1 µs · 0B · 0a | 972.1 µs · 0B · 0a | 60.52 µs · 0B · 0a | 16× | 98.89 µs · 8B · 1a | 35.03 µs · 0B · 0a | 2.039 ms · 319345B · 28655a | 1.95 ms · 703B · 2a | 2.524 ms · 4680B · 39a | 11.6 ms · 10158181B · 109494a | — | 24.98 ms · 13061339B · 394061a |
| iterative-fib | 892.8 ns · 0B · 0a | 880.5 ns · 0B · 0a | 82.83 ns · 0B · 0a | 10.8× | 85.08 ns · 8B · 1a | 12.42 ns · 0B · 0a | 25.37 µs · 90592B · 61a | 1.163 µs · 159B · 0a | 4.227 µs · 368B · 20a | 6.001 µs · 2448B · 88a | — | 6.171 µs · 944B · 81a |
| mandelbrot | 227.3 µs · 0B · 0a | 191.2 µs · 0B · 0a | 11.24 µs · 0B · 0a | 20.2× | — | 6.325 µs · 0B · 0a | 929 µs · 292346B · 25008a | 490.6 µs · 169408B · 661a | 8.23 ms · 5558655B · 104191a | 1.461 ms · 324977B · 23643a | — | 1.525 ms · 365403B · 22513a |
| map-churn | 2.457 ms · 786608B · 49a | 2.445 ms · 786608B · 49a | 638.9 µs · 786608B · 49a | 3.85× | — | 684.7 µs · 19024B · 5a | — | 11.8 ms · 7213788B · 6563a | 15.42 ms · 2369294B · 176707a | — | — | 51.51 ms · 24395154B · 725811a |
| map-iter | 3.054 ms · 249040B · 24645a | 3.049 ms · 249040B · 24645a | 3 ms · 249040B · 24645a | 1.02× | — | 535.1 µs · 19024B · 5a | — | 12.79 ms · 6177906B · 26325a | 23.16 ms · 13957134B · 201529a | — | — | 5.956 ms · 972942B · 93724a |
| map-ops | 60.92 µs · 12976B · 4a | 60.79 µs · 12976B · 4a | 11.8 µs · 12976B · 4a | 5.16× | — | 10.99 µs · 4904B · 3a | — | 284.6 µs · 114328B · 198a | 378.8 µs · 66728B · 4078a | — | — | 1.102 ms · 501523B · 15813a |
| map-string | 6.491 ms · 174184B · 9244a | 6.515 ms · 174184B · 9244a | 2.09 ms · 601416B · 12766a | 3.11× | — | 1.111 ms · 52632B · 2112a | 8.104 ms · 1463604B · 126292a | 9.441 ms · 967150B · 9917a | 15.92 ms · 590603B · 59508a | 8.856 ms · 1037356B · 80210a | — | 9.369 ms · 1281213B · 94103a |
| mat-mul | 312.2 µs · 6216B · 6a | 311.8 µs · 6216B · 6a | 8.638 µs · 6216B · 6a | 36.1× | — | 5.579 µs · 6144B · 3a | 1.357 ms · 431715B · 34871a | 677 µs · 143168B · 478a | 1.211 ms · 61304B · 537a | 1.46 ms · 90712B · 9350a | — | 921.5 µs · 36614B · 6036a |
| n-body | 494.8 µs · 504B · 14a | 413.1 µs · 504B · 14a | 18.85 µs · 504B · 14a | 26.2× | — | 7.358 µs · 0B · 0a | 1.518 ms · 386899B · 35892a | 1.228 ms · 258913B · 2002a | 3.33 ms · 658435B · 81545a | 2.432 ms · 382714B · 34975a | — | 1.743 ms · 445463B · 20669a |
| n-queens | 279 µs · 120B · 6a | 279 µs · 120B · 6a | 31.82 µs · 120B · 6a | 8.77× | — | 6.016 µs · 0B · 0a | 955.4 µs · 234865B · 17784a | 575.6 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.429 ms · 410346B · 5147a | — | 2.007 ms · 766004B · 33371a |
| naive-checksum | 188.6 µs · 0B · 0a | 108.4 µs · 0B · 0a | 8.765 µs · 0B · 0a | 21.5× | — | 2.759 µs · 0B · 0a | 614.8 µs · 235753B · 18206a | 483.9 µs · 174768B · 682a | 875.1 µs · 155641B · 19402a | 781.6 µs · 163817B · 20409a | — | 310.7 µs · 8976B · 2071a |
| permutation-flips | 103.4 µs · 7680B · 128a | 103.4 µs · 7680B · 128a | 17.24 µs · 7680B · 128a | 6× | — | 1.897 ns · 0B · 0a | 564.2 µs · 292857B · 9856a | 236.8 µs · 78808B · 451a | 525.3 µs · 122504B · 765a | 494.4 µs · 111976B · 2368a | — | 1.049 µs · 528B · 13a |
| recursive-fib-20 | 580.4 µs · 0B · 0a | 595.7 µs · 0B · 0a | 47.15 µs · 0B · 0a | 12.3× | 42.76 µs · 8B · 1a | 21.43 µs · 0B · 0a | 1.955 ms · 319346B · 28655a | 2.222 ms · 704B · 2a | 2.96 ms · 4680B · 39a | 11.57 ms · 9807914B · 109494a | — | 10.65 ms · 8302706B · 192848a |
| recursive-fib-35 | 792.9 ms · 0B · 0a | 796.2 ms · 0B · 0a | 63.99 ms · 0B · 0a | 12.4× | 58.2 ms · 8B · 1a | 29.23 ms · 0B · 0a | 2.559 s · 312797424B · 39088168a | 3.065 s · 971008B · 3793a | 4.023 s · 375360B · 46373a | 15.99 s · 13378018512B · 149349967a | — | — |
| sieve | 16.38 µs · 1048B · 2a | 15.29 µs · 1048B · 2a | 950.9 ns · 1048B · 2a | 17.2× | 1.037 µs · 8B · 1a | 371.8 ns · 0B · 0a | 141.3 µs · 122504B · 1611a | 51.28 µs · 18416B · 44a | 84.14 µs · 1872B · 25a | 77.84 µs · 5704B · 30a | — | 80.45 µs · 4368B · 795a |
| sort-stress | 387.7 µs · 5136B · 512a | 370.5 µs · 5136B · 512a | 12.77 µs · 5136B · 512a | 30.4× | — | 5.382 µs · 1024B · 2a | 1.263 ms · 270945B · 19965a | 847 µs · 36568B · 365a | 1.792 ms · 49104B · 3621a | 1.78 ms · 23448B · 2034a | — | 922.4 µs · 17473B · 1708a |
| spectral-norm | 434 µs · 648B · 6a | 349.3 µs · 648B · 6a | 8.476 µs · 648B · 6a | 51.2× | — | 7.039 µs · 576B · 3a | 1.995 ms · 571371B · 54822a | 1.084 ms · 234784B · 925a | 2.419 ms · 275337B · 33729a | 4.715 ms · 2457136B · 52718a | — | 4.081 ms · 2448004B · 74233a |
| string-build | 598.7 µs · 85408B · 4107a | 586.8 µs · 85408B · 4107a | 536.5 µs · 85408B · 4107a | 1.12× | — | 374.3 µs · 855889B · 5001a | 2.461 ms · 2211275B · 36101a | 2.329 ms · 1078110B · 13859a | 3.142 ms · 1147382B · 26883a | 2.506 ms · 1326243B · 24469a | — | 3.132 ms · 2372778B · 41432a |
| string-compare | 989.4 µs · 21136B · 1284a | 992.5 µs · 21136B · 1284a | 218.2 µs · 21136B · 1284a | 4.53× | — | 63.44 µs · 9632B · 1585a | 1.657 ms · 296282B · 20528a | 1.127 ms · 59712B · 3431a | 2.33 ms · 50744B · 3848a | 1.831 ms · 209522B · 5520a | — | 2.384 ms · 503609B · 31846a |
| struct-tree-walk | 247.7 µs · 768B · 8a | 247.6 µs · 768B · 8a | 102 µs · 768B · 8a | 2.43× | — | 1.898 ns · 0B · 0a | 659.8 µs · 458378B · 5114a | 1.354 ms · 818512B · 11253a | 1.085 ms · 558960B · 6149a | 3.53 ms · 2570662B · 34797a | — | 1.19 µs · 600B · 17a |
| tail-ping-pong | 21.52 µs · 0B · 0a | 21.71 µs · 0B · 0a | 478.2 ns · 0B · 0a | 45× | — | 891.2 ns · 0B · 0a | 112.7 µs · 106112B · 2001a | 36.45 µs · 21856B · 85a | 112.4 µs · 13984B · 1726a | 116.9 µs · 14280B · 1731a | — | 539.9 µs · 348830B · 9016a |
| tail-sum | 21.73 µs · 0B · 0a | 21.58 µs · 0B · 0a | 669.2 ns · 0B · 0a | 32.5× | — | 302.5 ns · 0B · 0a | 109.8 µs · 106112B · 2001a | 36.29 µs · 21856B · 85a | 112 µs · 13984B · 1726a | 116.8 µs · 14280B · 1731a | — | 84.69 µs · 4608B · 1017a |
| typed-array-sum | 5.675 µs · 0B · 0a | 5.671 µs · 0B · 0a | 266 ns · 0B · 0a | 21.3× | 260.5 ns · 8B · 1a | 1.875 ns · 0B · 0a | 45.79 µs · 94208B · 513a | 8.511 µs · 3999B · 15a | 27.58 µs · 2080B · 238a | 17.12 µs · 2496B · 246a | — | — |
| xorshift-i64 | 104.8 µs · 18424B · 2046a | 104.5 µs · 18424B · 2046a | 1.347 µs · 18424B · 2046a | 77.8× | — | 356.9 ns · 0B · 0a | 104.2 µs · 107608B · 2188a | — | 994.7 µs · 447255B · 12735a | 431.8 µs · 199938B · 7772a | — | 101.9 µs · 13545B · 1317a |

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
