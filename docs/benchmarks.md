# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.206 µs · 0B · 0a | 8.195 µs · 0B · 0a | 5.376 µs · 0B · 0a | 1.53× | — | 2.175 µs · 1024B · 128a | 38.72 µs · 96288B · 388a | 20.18 µs · 14376B · 256a | 78.07 µs · 78016B · 770a | 14.16 µs · 5712B · 266a | — | 30.68 µs · 2308B · 276a |
| binary-trees | 1.677 ms · 768B · 8a | 1.663 ms · 768B · 8a | 568.9 µs · 768B · 8a | 2.95× | — | 294.1 µs · 201936B · 8414a | 3.796 ms · 1468184B · 62877a | 10.29 ms · 6995826B · 92703a | 8.75 ms · 5198696B · 69052a | 27.07 ms · 19457580B · 280714a | — | 28.69 ms · 15128176B · 446106a |
| branch-tree | 932.9 ns · 0B · 0a | 18.71 ns · 0B · 0a | 84.57 ns · 0B · 0a | 11× | 625.5 ns · 16B · 1a | 160.5 ns · 0B · 0a | 44.84 µs · 95384B · 660a | 15.15 µs · 2463B · 9a | 26.37 µs · 1992B · 196a | 24.46 µs · 2168B · 203a | — | 20.61 µs · 1640B · 219a |
| closure-counter | 5.256 µs · 64B · 2a | 5.215 µs · 64B · 2a | 906.5 ns · 64B · 2a | 5.8× | — | 40.51 ns · 0B · 0a | 41.08 µs · 92272B · 261a | 12.95 µs · 151B · 3a | 19.34 µs · 1264B · 13a | 78.74 µs · 58312B · 659a | — | 88.5 µs · 35629B · 922a |
| coroutine-gen | 66.55 µs · 128B · 2a | 66.72 µs · 128B · 2a | 66.52 µs · 128B · 2a | 1× | — | 42.11 µs · 208B · 7a | — | 327.7 µs · 149936B · 649a | 603.4 µs · 330649B · 6719a | 200.5 µs · 91800B · 3646a | — | — |
| fannkuch | 657 µs · 34608B · 1442a | 639.3 µs · 34608B · 1442a | 214.3 µs · 34608B · 1442a | 3.07× | — | 35.61 µs · 17280B · 720a | 2.012 ms · 456579B · 25621a | 1.532 ms · 443136B · 1460a | 19.32 ms · 14005374B · 164946a | 3.391 ms · 1367676B · 16944a | — | 4.762 ms · 2501454B · 88439a |
| fnv1a64 | 115.4 µs · 16400B · 2049a | 116.5 µs · 16400B · 2049a | 1.143 µs · 16400B · 2049a | 101× | — | 903.4 ns · 0B · 0a | 161 µs · 120808B · 3838a | — | 1.445 ms · 608254B · 18475a | 397.1 µs · 162921B · 7429a | — | 154.7 µs · 17282B · 2074a |
| host-call | 459.1 µs · 41752B · 3080a | 472.2 µs · 41752B · 3080a | 470.9 µs · 41752B · 3080a | 0.975× | — | 3.321 µs · 0B · 0a | 180.2 µs · 139264B · 3073a | 130.4 µs · 22536B · 1079a | 803.6 µs · 158049B · 14585a | 108.7 µs · 14920B · 1799a | — | 585.6 µs · 340640B · 9231a |
| i64-wide-fib | 26.95 ms · 2913440B · 364178a | 20.43 ms · 1942304B · 242785a | 680.8 µs · 16B · 1a | 39.6× | — | 239 µs · 0B · 0a | 21.72 µs · 90520B · 52a | — | 160.6 ms · 64193256B · 1942305a | 338.5 ms · 253472784B · 4248841a | — | 131.8 ms · 98686576B · 2138708a |
| indirect-recursive-fib | 1.016 ms · 0B · 0a | 1.018 ms · 0B · 0a | 60.48 µs · 0B · 0a | 16.8× | 100.6 µs · 8B · 1a | 35.04 µs · 0B · 0a | 2.071 ms · 319345B · 28655a | 1.936 ms · 704B · 2a | 2.562 ms · 4680B · 39a | 11.38 ms · 10158194B · 109494a | — | 24.91 ms · 13061321B · 394061a |
| iterative-fib | 875.4 ns · 0B · 0a | 880.7 ns · 0B · 0a | 83.36 ns · 0B · 0a | 10.5× | 85.15 ns · 8B · 1a | 12.47 ns · 0B · 0a | 23.99 µs · 90592B · 61a | 1.156 µs · 160B · 0a | 4.266 µs · 368B · 20a | 6.112 µs · 2448B · 88a | — | 6.113 µs · 944B · 81a |
| mandelbrot | 229 µs · 0B · 0a | 192.5 µs · 0B · 0a | 11.28 µs · 0B · 0a | 20.3× | — | 6.331 µs · 0B · 0a | 963.8 µs · 292346B · 25008a | 485.9 µs · 169408B · 661a | 8.081 ms · 5558650B · 104191a | 1.507 ms · 324979B · 23643a | — | 1.526 ms · 365405B · 22513a |
| map-churn | 2.493 ms · 786608B · 49a | 2.493 ms · 786608B · 49a | 670.5 µs · 786608B · 49a | 3.72× | — | 688.6 µs · 19024B · 5a | — | 11.53 ms · 7213808B · 6563a | 15.92 ms · 2369300B · 176707a | — | — | 53.28 ms · 24395128B · 725811a |
| map-iter | 3.446 ms · 249040B · 24645a | 3.435 ms · 249040B · 24645a | 3.385 ms · 249040B · 24645a | 1.02× | — | 536.7 µs · 19024B · 5a | — | 12.61 ms · 6177881B · 26325a | 22.45 ms · 13957105B · 201529a | — | — | 5.983 ms · 972954B · 93724a |
| map-ops | 61.64 µs · 12976B · 4a | 61.8 µs · 12976B · 4a | 11.87 µs · 12976B · 4a | 5.19× | — | 11.16 µs · 4904B · 3a | — | 276.2 µs · 114328B · 198a | 385.2 µs · 66728B · 4078a | — | — | 1.176 ms · 501518B · 15813a |
| map-string | 6.474 ms · 174184B · 9244a | 6.482 ms · 174184B · 9244a | 2.096 ms · 634224B · 14018a | 3.09× | — | 1.115 ms · 52632B · 2112a | 8.334 ms · 1463603B · 126292a | 9.491 ms · 967122B · 9917a | 16.22 ms · 590612B · 59508a | 8.868 ms · 1037320B · 80210a | — | 9.311 ms · 1281226B · 94104a |
| mat-mul | 312.9 µs · 6216B · 6a | 313.2 µs · 6216B · 6a | 8.751 µs · 6216B · 6a | 35.8× | — | 5.634 µs · 6144B · 3a | 1.385 ms · 431714B · 34871a | 666.4 µs · 143168B · 478a | 1.208 ms · 61304B · 537a | 1.463 ms · 90712B · 9350a | — | 922.9 µs · 36613B · 6036a |
| n-body | 500.2 µs · 504B · 14a | 415.3 µs · 504B · 14a | 18.79 µs · 504B · 14a | 26.6× | — | 7.358 µs · 0B · 0a | 1.535 ms · 386898B · 35892a | 1.22 ms · 258914B · 2002a | 3.407 ms · 658436B · 81545a | 2.471 ms · 382714B · 34975a | — | 1.765 ms · 445467B · 20669a |
| n-queens | 273.9 µs · 120B · 6a | 274.7 µs · 120B · 6a | 31.7 µs · 120B · 6a | 8.64× | — | 6.01 µs · 0B · 0a | 957.7 µs · 234865B · 17784a | 566.6 µs · 14423B · 63a | 1.108 ms · 7424B · 35a | 1.459 ms · 410345B · 5147a | — | 2.018 ms · 766011B · 33371a |
| naive-checksum | 188.8 µs · 0B · 0a | 108.1 µs · 0B · 0a | 8.745 µs · 0B · 0a | 21.6× | — | 2.758 µs · 0B · 0a | 620.1 µs · 235753B · 18206a | 480.1 µs · 174769B · 682a | 896.9 µs · 155641B · 19402a | 803 µs · 163817B · 20409a | — | 310.1 µs · 8977B · 2071a |
| permutation-flips | 103.6 µs · 7680B · 128a | 103.6 µs · 7680B · 128a | 17.01 µs · 7680B · 128a | 6.09× | — | 1.864 ns · 0B · 0a | 567.1 µs · 292857B · 9856a | 234.1 µs · 78808B · 451a | 532.3 µs · 122504B · 765a | 498.1 µs · 111976B · 2368a | — | 1.036 µs · 528B · 13a |
| recursive-fib-20 | 638.4 µs · 0B · 0a | 640.4 µs · 0B · 0a | 47.24 µs · 0B · 0a | 13.5× | 42.83 µs · 8B · 1a | 21.48 µs · 0B · 0a | 1.946 ms · 319345B · 28655a | 2.165 ms · 704B · 2a | 2.987 ms · 4680B · 39a | 11.51 ms · 9807927B · 109494a | — | 11.34 ms · 8302714B · 192848a |
| recursive-fib-35 | 868.7 ms · 0B · 0a | 869.6 ms · 0B · 0a | 64.02 ms · 0B · 0a | 13.6× | 58.21 ms · 8B · 1a | 29.19 ms · 0B · 0a | 2.612 s · 312797264B · 39088168a | 2.953 s · 971008B · 3793a | 4.054 s · 375360B · 46373a | 15.7 s · 13378018560B · 149349995a | — | — |
| sieve | 15.24 µs · 1048B · 2a | 15.14 µs · 1048B · 2a | 968.3 ns · 1048B · 2a | 15.7× | 1.037 µs · 8B · 1a | 372 ns · 0B · 0a | 140.3 µs · 122504B · 1611a | 51.06 µs · 18416B · 44a | 84.81 µs · 1872B · 25a | 77.21 µs · 5704B · 30a | — | 81.36 µs · 4368B · 795a |
| sort-stress | 389.1 µs · 5136B · 512a | 374.3 µs · 5136B · 512a | 12.91 µs · 5136B · 512a | 30.1× | — | 5.447 µs · 1024B · 2a | 1.296 ms · 270946B · 19965a | 845.6 µs · 36567B · 365a | 1.803 ms · 49104B · 3621a | 1.774 ms · 23448B · 2034a | — | 921.8 µs · 17473B · 1708a |
| spectral-norm | 429.5 µs · 648B · 6a | 345 µs · 648B · 6a | 8.522 µs · 648B · 6a | 50.4× | — | 7.102 µs · 576B · 3a | 2.036 ms · 571370B · 54822a | 1.09 ms · 234784B · 925a | 2.478 ms · 275337B · 33729a | 4.673 ms · 2457134B · 52718a | — | 4.08 ms · 2448013B · 74233a |
| string-build | 601.4 µs · 85408B · 4107a | 592.1 µs · 85408B · 4107a | 474.5 µs · 85408B · 4107a | 1.27× | — | 410.5 µs · 855890B · 5001a | 2.481 ms · 2211275B · 36101a | 2.333 ms · 1078112B · 13859a | 3.207 ms · 1147384B · 26883a | 2.478 ms · 1326242B · 24469a | — | 3.127 ms · 2372772B · 41432a |
| string-compare | 995.9 µs · 21136B · 1284a | 997.9 µs · 21136B · 1284a | 218.7 µs · 21136B · 1284a | 4.55× | — | 65.51 µs · 9632B · 1585a | 1.678 ms · 296281B · 20528a | 1.129 ms · 59711B · 3431a | 2.331 ms · 50744B · 3848a | 1.848 ms · 209521B · 5520a | — | 2.369 ms · 503620B · 31846a |
| struct-tree-walk | 251.2 µs · 768B · 8a | 250.8 µs · 768B · 8a | 102.9 µs · 768B · 8a | 2.44× | — | 1.864 ns · 0B · 0a | 649.5 µs · 458378B · 5114a | 1.338 ms · 818514B · 11253a | 1.078 ms · 558960B · 6149a | 3.408 ms · 2570666B · 34797a | — | 1.221 µs · 600B · 17a |
| tail-ping-pong | 21.62 µs · 0B · 0a | 21.67 µs · 0B · 0a | 475.8 ns · 0B · 0a | 45.4× | — | 891.5 ns · 0B · 0a | 110 µs · 106112B · 2001a | 35.71 µs · 21856B · 85a | 111.6 µs · 13984B · 1726a | 119.7 µs · 14280B · 1731a | — | 557.4 µs · 348834B · 9016a |
| tail-sum | 21.76 µs · 0B · 0a | 21.63 µs · 0B · 0a | 681.2 ns · 0B · 0a | 31.9× | — | 304.3 ns · 0B · 0a | 109.7 µs · 106112B · 2001a | 35.67 µs · 21856B · 85a | 112.1 µs · 13984B · 1726a | 119.5 µs · 14280B · 1731a | — | 84.33 µs · 4608B · 1017a |
| typed-array-sum | 5.678 µs · 0B · 0a | 5.658 µs · 0B · 0a | 264.5 ns · 0B · 0a | 21.5× | 268 ns · 8B · 1a | 1.895 ns · 0B · 0a | 44.03 µs · 94208B · 513a | 8.327 µs · 4000B · 15a | 27.19 µs · 2080B · 238a | 17.5 µs · 2496B · 246a | — | — |
| xorshift-i64 | 107.6 µs · 18424B · 2046a | 107.3 µs · 18424B · 2046a | 1.379 µs · 18424B · 2046a | 78× | — | 356.1 ns · 0B · 0a | 104 µs · 107608B · 2188a | — | 990.3 µs · 447251B · 12735a | 430.4 µs · 199938B · 7772a | — | 101.6 µs · 13545B · 1317a |

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
