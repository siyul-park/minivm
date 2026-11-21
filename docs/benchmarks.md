# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.294 µs · 0B · 0a | 9.267 µs · 0B · 0a | 5.371 µs · 0B · 0a | 1.73× | — | 2.267 µs · 1024B · 128a | 37.34 µs · 96288B · 388a | 16.88 µs · 14376B · 256a | 68.49 µs · 78016B · 770a | 14.45 µs · 5712B · 266a | — | 31.39 µs · 2308B · 276a |
| binary-trees | 1.969 ms · 768B · 8a | 1.965 ms · 768B · 8a | 592.6 µs · 768B · 8a | 3.32× | — | 297.3 µs · 201936B · 8414a | 3.879 ms · 1468182B · 62877a | 11.69 ms · 6995712B · 92703a | 10.37 ms · 5198726B · 69052a | 27.36 ms · 19457644B · 280714a | — | 30.93 ms · 15128188B · 446106a |
| branch-tree | 1.348 µs · 0B · 0a | 19.77 ns · 0B · 0a | 84.45 ns · 0B · 0a | 16× | 625.4 ns · 16B · 1a | 160.4 ns · 0B · 0a | 45.07 µs · 95384B · 660a | 15.6 µs · 2464B · 9a | 26.91 µs · 1992B · 196a | 24.41 µs · 2168B · 203a | — | 20.68 µs · 1640B · 219a |
| closure-counter | 5.534 µs · 64B · 2a | 5.542 µs · 64B · 2a | 906.6 ns · 64B · 2a | 6.1× | — | 40.11 ns · 0B · 0a | 39.67 µs · 92272B · 261a | 12.84 µs · 151B · 3a | 19.32 µs · 1264B · 13a | 80.19 µs · 58312B · 659a | — | 93.74 µs · 35629B · 922a |
| coroutine-gen | 79.23 µs · 128B · 2a | 79.36 µs · 128B · 2a | 79.24 µs · 128B · 2a | 1× | — | 42.42 µs · 208B · 7a | — | 340 µs · 149935B · 649a | 601.1 µs · 330649B · 6719a | 199.8 µs · 91800B · 3646a | — | — |
| fannkuch | 830.7 µs · 34608B · 1442a | 818.8 µs · 34608B · 1442a | 218.4 µs · 34608B · 1442a | 3.8× | — | 36.78 µs · 17280B · 720a | 2.032 ms · 456578B · 25621a | 1.638 ms · 443137B · 1460a | 19.98 ms · 14005371B · 164946a | 3.397 ms · 1367680B · 16944a | — | 4.882 ms · 2501446B · 88439a |
| fnv1a64 | 125.7 µs · 16400B · 2049a | 126.9 µs · 16400B · 2049a | 1.157 µs · 16400B · 2049a | 109× | — | 902.6 ns · 0B · 0a | 161.8 µs · 120808B · 3838a | — | 1.465 ms · 608254B · 18475a | 400.8 µs · 162921B · 7429a | — | 156.5 µs · 17282B · 2074a |
| host-call | 476.6 µs · 41752B · 3080a | 480.9 µs · 41752B · 3080a | 490.3 µs · 41752B · 3080a | 0.972× | — | 3.313 µs · 0B · 0a | 179.4 µs · 139264B · 3073a | 138 µs · 22536B · 1079a | 832.9 µs · 158049B · 14585a | 108 µs · 14920B · 1799a | — | 600.1 µs · 340640B · 9231a |
| i64-wide-fib | 29.26 ms · 2913456B · 364178a | 21.31 ms · 1942304B · 242785a | 679.5 µs · 16B · 1a | 43.1× | — | 238.7 µs · 0B · 0a | 23.15 µs · 90520B · 52a | — | 157.2 ms · 64193296B · 1942305a | 347.6 ms · 253472656B · 4248840a | — | 132.6 ms · 98686581B · 2138708a |
| indirect-recursive-fib | 1.189 ms · 0B · 0a | 1.186 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.7× | 99.07 µs · 8B · 1a | 34.8 µs · 0B · 0a | 2.115 ms · 319345B · 28655a | 1.948 ms · 704B · 2a | 2.546 ms · 4680B · 39a | 11.56 ms · 10158184B · 109494a | — | 26.43 ms · 13061331B · 394061a |
| iterative-fib | 1.132 µs · 0B · 0a | 1.073 µs · 0B · 0a | 77.69 ns · 0B · 0a | 14.6× | 85.92 ns · 8B · 1a | 11.91 ns · 0B · 0a | 23.55 µs · 90592B · 61a | 1.152 µs · 160B · 0a | 4.29 µs · 368B · 20a | 6.177 µs · 2448B · 88a | — | 6.24 µs · 944B · 81a |
| mandelbrot | 291.9 µs · 0B · 0a | 249.8 µs · 0B · 0a | 11.27 µs · 0B · 0a | 25.9× | — | 6.328 µs · 0B · 0a | 959.4 µs · 292346B · 25008a | 483.4 µs · 169408B · 661a | 8.082 ms · 5558647B · 104191a | 1.508 ms · 324978B · 23643a | — | 1.601 ms · 365405B · 22513a |
| map-ops | 70.69 µs · 8816B · 3a | 70.64 µs · 8816B · 3a | 10.65 µs · 8816B · 3a | 6.64× | — | 10.76 µs · 4904B · 3a | — | 274.4 µs · 114328B · 198a | 388.7 µs · 66728B · 4078a | — | — | 1.183 ms · 501517B · 15813a |
| mat-mul | 375.5 µs · 6216B · 6a | 375.1 µs · 6216B · 6a | 8.518 µs · 6216B · 6a | 44.1× | — | 5.307 µs · 6144B · 3a | 1.385 ms · 431714B · 34871a | 670.1 µs · 143168B · 478a | 1.201 ms · 61304B · 537a | 1.488 ms · 90712B · 9350a | — | 923.3 µs · 36615B · 6036a |
| n-body | 613.4 µs · 504B · 14a | 580 µs · 504B · 14a | 19.07 µs · 504B · 14a | 32.2× | — | 7.351 µs · 0B · 0a | 1.542 ms · 386898B · 35892a | 1.223 ms · 258913B · 2002a | 3.386 ms · 658435B · 81545a | 2.443 ms · 382715B · 34975a | — | 1.766 ms · 445465B · 20669a |
| n-queens | 430.8 µs · 120B · 6a | 430.6 µs · 120B · 6a | 31.62 µs · 120B · 6a | 13.6× | — | 6 µs · 0B · 0a | 960.3 µs · 234865B · 17784a | 569.7 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.435 ms · 410346B · 5147a | — | 2.039 ms · 766008B · 33371a |
| naive-checksum | 252 µs · 0B · 0a | 141.4 µs · 0B · 0a | 8.705 µs · 0B · 0a | 28.9× | — | 2.759 µs · 0B · 0a | 619.9 µs · 235753B · 18206a | 475.7 µs · 174768B · 682a | 904.7 µs · 155640B · 19402a | 799.1 µs · 163817B · 20409a | — | 311.1 µs · 8977B · 2071a |
| permutation-flips | 129.9 µs · 7680B · 128a | 130.8 µs · 7680B · 128a | 17.23 µs · 7680B · 128a | 7.54× | — | 1.877 ns · 0B · 0a | 558.2 µs · 292858B · 9856a | 232.4 µs · 78808B · 451a | 525.3 µs · 122504B · 765a | 496.5 µs · 111976B · 2368a | — | 1.039 µs · 528B · 13a |
| recursive-fib-20 | 708.8 µs · 0B · 0a | 718.2 µs · 0B · 0a | 47.4 µs · 0B · 0a | 15× | 43.24 µs · 8B · 1a | 21.53 µs · 0B · 0a | 1.961 ms · 319346B · 28655a | 2.156 ms · 704B · 2a | 2.963 ms · 4680B · 39a | 11.25 ms · 9807907B · 109494a | — | 11.51 ms · 8302733B · 192848a |
| recursive-fib-35 | 968.7 ms · 0B · 0a | 980 ms · 0B · 0a | 64 ms · 0B · 0a | 15.1× | 58.2 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.671 s · 312797824B · 39088170a | 2.958 s · 971008B · 3793a | 4.027 s · 375360B · 46373a | 15.3 s · 13378020912B · 149349994a | — | — |
| sieve | 20.34 µs · 1048B · 2a | 20.49 µs · 1048B · 2a | 909.5 ns · 1048B · 2a | 22.4× | 1.017 µs · 8B · 1a | 350.8 ns · 0B · 0a | 134.7 µs · 122504B · 1611a | 49.68 µs · 18416B · 44a | 84.12 µs · 1872B · 25a | 76.44 µs · 5704B · 30a | — | 81.56 µs · 4368B · 795a |
| sort-stress | 503.5 µs · 5136B · 512a | 509.7 µs · 5136B · 512a | 12.72 µs · 5136B · 512a | 39.6× | — | 5.765 µs · 1024B · 2a | 1.312 ms · 270945B · 19965a | 844.9 µs · 36567B · 365a | 1.792 ms · 49104B · 3621a | 1.787 ms · 23448B · 2034a | — | 928.2 µs · 17473B · 1708a |
| spectral-norm | 532 µs · 648B · 6a | 459.3 µs · 648B · 6a | 8.395 µs · 648B · 6a | 63.4× | — | 7.032 µs · 576B · 3a | 2.072 ms · 571372B · 54822a | 1.075 ms · 234784B · 925a | 2.513 ms · 275337B · 33729a | 4.632 ms · 2457136B · 52718a | — | 4.151 ms · 2447996B · 74233a |
| string-build | 738.9 µs · 85408B · 4107a | 734.1 µs · 85408B · 4107a | 618.6 µs · 85408B · 4107a | 1.19× | — | 344 µs · 855890B · 5001a | 2.39 ms · 2211276B · 36101a | 2.299 ms · 1078112B · 13859a | 3.133 ms · 1147387B · 26883a | 2.465 ms · 1326243B · 24469a | — | 3.031 ms · 2372776B · 41432a |
| string-compare | 1.093 ms · 21136B · 1284a | 1.093 ms · 21136B · 1284a | 221.6 µs · 21136B · 1284a | 4.93× | — | 65.01 µs · 9632B · 1585a | 1.687 ms · 296281B · 20528a | 1.143 ms · 59712B · 3431a | 2.33 ms · 50744B · 3848a | 1.814 ms · 209521B · 5520a | — | 2.457 ms · 503621B · 31846a |
| struct-tree-walk | 282.6 µs · 768B · 8a | 282.4 µs · 768B · 8a | 107.4 µs · 768B · 8a | 2.63× | — | 1.892 ns · 0B · 0a | 633.6 µs · 458378B · 5114a | 1.348 ms · 818511B · 11253a | 1.054 ms · 558960B · 6149a | 3.398 ms · 2570663B · 34797a | — | 1.223 µs · 600B · 17a |
| tail-ping-pong | 29.42 µs · 0B · 0a | 29.22 µs · 0B · 0a | 480.8 ns · 0B · 0a | 61.2× | — | 890.8 ns · 0B · 0a | 105.7 µs · 106112B · 2001a | 36.87 µs · 21855B · 85a | 111.6 µs · 13984B · 1726a | 120 µs · 14280B · 1731a | — | 552.7 µs · 348831B · 9016a |
| tail-sum | 29.37 µs · 0B · 0a | 29.45 µs · 0B · 0a | 665.7 ns · 0B · 0a | 44.1× | — | 301 ns · 0B · 0a | 107.3 µs · 106112B · 2001a | 35.43 µs · 21856B · 85a | 112.1 µs · 13984B · 1726a | 119.6 µs · 14280B · 1731a | — | 84.77 µs · 4608B · 1017a |
| typed-array-sum | 7.365 µs · 0B · 0a | 7.374 µs · 0B · 0a | 260.9 ns · 0B · 0a | 28.2× | 262.5 ns · 8B · 1a | 1.899 ns · 0B · 0a | 42.41 µs · 94208B · 513a | 8.206 µs · 4000B · 15a | 27.91 µs · 2080B · 238a | 17.55 µs · 2496B · 246a | — | — |
| xorshift-i64 | 115.6 µs · 18424B · 2046a | 114.6 µs · 18424B · 2046a | 1.284 µs · 18424B · 2046a | 90× | — | 360.2 ns · 0B · 0a | 100.3 µs · 107608B · 2188a | — | 991.9 µs · 447254B · 12736a | 428.1 µs · 199938B · 7772a | — | 102.8 µs · 13546B · 1317a |

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
