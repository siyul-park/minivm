# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.009 µs · 0B · 0a | 8.947 µs · 0B · 0a | 5.462 µs · 0B · 0a | 1.65× | — | 2.573 µs · 1024B · 128a | 41.23 µs · 96288B · 388a | 17.52 µs · 14376B · 256a | 71.9 µs · 78016B · 770a | 14.14 µs · 5712B · 266a | — | 30.88 µs · 2308B · 276a |
| binary-trees | 1.972 ms · 768B · 8a | 1.973 ms · 768B · 8a | 591.5 µs · 768B · 8a | 3.33× | — | 297.8 µs · 201936B · 8414a | 3.926 ms · 1468187B · 62877a | 10.56 ms · 6995689B · 92703a | 8.961 ms · 5198697B · 69052a | 28 ms · 19457610B · 280714a | — | 30.52 ms · 15128168B · 446106a |
| branch-tree | 1.317 µs · 0B · 0a | 19.91 ns · 0B · 0a | 86.05 ns · 0B · 0a | 15.3× | 625.7 ns · 16B · 1a | 160.6 ns · 0B · 0a | 46.27 µs · 95385B · 660a | 15.46 µs · 2463B · 9a | 26.96 µs · 1992B · 196a | 24.38 µs · 2168B · 203a | — | 20.59 µs · 1640B · 219a |
| closure-counter | 5.569 µs · 64B · 2a | 5.568 µs · 64B · 2a | 917.2 ns · 64B · 2a | 6.07× | — | 40.11 ns · 0B · 0a | 41.66 µs · 92272B · 261a | 13 µs · 151B · 3a | 19.37 µs · 1264B · 13a | 80.07 µs · 58312B · 659a | — | 90.73 µs · 35629B · 922a |
| coroutine-gen | 78.96 µs · 128B · 2a | 79.05 µs · 128B · 2a | 79.01 µs · 128B · 2a | 0.999× | — | 42.75 µs · 208B · 7a | — | 328.7 µs · 149936B · 649a | 611.4 µs · 330650B · 6719a | 202.5 µs · 91800B · 3646a | — | — |
| fannkuch | 834.5 µs · 34608B · 1442a | 821.4 µs · 34608B · 1442a | 224.4 µs · 34608B · 1442a | 3.72× | — | 35.9 µs · 17280B · 720a | 2.072 ms · 456578B · 25621a | 1.526 ms · 443136B · 1460a | 19.58 ms · 14005354B · 164946a | 3.362 ms · 1367677B · 16944a | — | 4.877 ms · 2501440B · 88439a |
| fnv1a64 | 126.3 µs · 16400B · 2049a | 127.2 µs · 16400B · 2049a | 1.158 µs · 16400B · 2049a | 109× | — | 902.5 ns · 0B · 0a | 163.8 µs · 120808B · 3838a | — | 1.522 ms · 608248B · 18475a | 405.2 µs · 162921B · 7429a | — | 154.3 µs · 17282B · 2074a |
| host-call | 470.1 µs · 41752B · 3080a | 477.3 µs · 41752B · 3080a | 476.8 µs · 41752B · 3080a | 0.986× | — | 3.313 µs · 0B · 0a | 180.7 µs · 139264B · 3073a | 131.9 µs · 22536B · 1079a | 812.3 µs · 158048B · 14585a | 108.1 µs · 14920B · 1799a | — | 604.1 µs · 340638B · 9231a |
| i64-wide-fib | 28.93 ms · 2913456B · 364178a | 21.11 ms · 1942304B · 242785a | 679.6 µs · 16B · 1a | 42.6× | — | 239.2 µs · 0B · 0a | 24.47 µs · 90520B · 52a | — | 160.6 ms · 64193304B · 1942306a | 351.6 ms · 253472544B · 4248839a | — | 134.1 ms · 98686618B · 2138708a |
| indirect-recursive-fib | 1.201 ms · 0B · 0a | 1.201 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.9× | 99.2 µs · 8B · 1a | 35.1 µs · 0B · 0a | 2.101 ms · 319345B · 28655a | 1.951 ms · 703B · 2a | 2.555 ms · 4680B · 39a | 11.89 ms · 10158190B · 109494a | — | 24.62 ms · 13061322B · 394061a |
| iterative-fib | 1.128 µs · 0B · 0a | 1.077 µs · 0B · 0a | 82.43 ns · 0B · 0a | 13.7× | 86.09 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.47 µs · 90592B · 61a | 1.164 µs · 160B · 0a | 4.297 µs · 368B · 20a | 6.121 µs · 2448B · 88a | — | 6.203 µs · 944B · 81a |
| mandelbrot | 290.5 µs · 0B · 0a | 250.5 µs · 0B · 0a | 11.37 µs · 0B · 0a | 25.5× | — | 6.35 µs · 0B · 0a | 983.4 µs · 292345B · 25008a | 491.6 µs · 169408B · 661a | 8.236 ms · 5558649B · 104191a | 1.536 ms · 324979B · 23643a | — | 1.545 ms · 365404B · 22513a |
| map-ops | 71.19 µs · 8816B · 3a | 70.93 µs · 8816B · 3a | 11 µs · 8816B · 3a | 6.47× | — | 10.87 µs · 4904B · 3a | — | 282.4 µs · 114328B · 198a | 388.8 µs · 66728B · 4078a | — | — | 1.207 ms · 501526B · 15813a |
| mat-mul | 378.1 µs · 6216B · 6a | 374.6 µs · 6216B · 6a | 8.703 µs · 6216B · 6a | 43.4× | — | 5.657 µs · 6144B · 3a | 1.384 ms · 431715B · 34871a | 671.1 µs · 143168B · 478a | 1.219 ms · 61304B · 537a | 1.462 ms · 90712B · 9350a | — | 918.9 µs · 36614B · 6036a |
| n-body | 613.1 µs · 504B · 14a | 580.1 µs · 504B · 14a | 18.89 µs · 504B · 14a | 32.4× | — | 7.344 µs · 0B · 0a | 1.527 ms · 386898B · 35892a | 1.22 ms · 258912B · 2002a | 3.383 ms · 658435B · 81545a | 2.496 ms · 382714B · 34975a | — | 1.754 ms · 445477B · 20669a |
| n-queens | 432.7 µs · 120B · 6a | 431.3 µs · 120B · 6a | 31.49 µs · 120B · 6a | 13.7× | — | 6.007 µs · 0B · 0a | 955.9 µs · 234865B · 17784a | 569 µs · 14423B · 63a | 1.101 ms · 7424B · 35a | 1.451 ms · 410346B · 5147a | — | 2.04 ms · 766007B · 33371a |
| naive-checksum | 251.7 µs · 0B · 0a | 141.9 µs · 0B · 0a | 8.759 µs · 0B · 0a | 28.7× | — | 2.759 µs · 0B · 0a | 623.6 µs · 235753B · 18206a | 483.8 µs · 174768B · 682a | 898.4 µs · 155640B · 19402a | 813.4 µs · 163817B · 20409a | — | 311.2 µs · 8977B · 2071a |
| permutation-flips | 129.4 µs · 7680B · 128a | 129.3 µs · 7680B · 128a | 17.16 µs · 7680B · 128a | 7.54× | — | 1.876 ns · 0B · 0a | 575.8 µs · 292858B · 9856a | 245.2 µs · 78808B · 451a | 530.5 µs · 122504B · 765a | 498.5 µs · 111976B · 2368a | — | 1.046 µs · 528B · 13a |
| recursive-fib-20 | 711.7 µs · 0B · 0a | 717.5 µs · 0B · 0a | 47.12 µs · 0B · 0a | 15.1× | 42.73 µs · 8B · 1a | 21.46 µs · 0B · 0a | 1.957 ms · 319345B · 28655a | 2.155 ms · 704B · 2a | 2.974 ms · 4680B · 39a | 11.49 ms · 9807928B · 109494a | — | 11.44 ms · 8302686B · 192847a |
| recursive-fib-35 | 974.5 ms · 0B · 0a | 983 ms · 0B · 0a | 64.05 ms · 0B · 0a | 15.2× | 58.15 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.619 s · 312797344B · 39088168a | 2.938 s · 971008B · 3793a | 4.047 s · 375360B · 46373a | 15.77 s · 13378018960B · 149349981a | — | — |
| sieve | 20.5 µs · 1048B · 2a | 20.41 µs · 1048B · 2a | 929.7 ns · 1048B · 2a | 22× | 1.027 µs · 8B · 1a | 351 ns · 0B · 0a | 139.7 µs · 122504B · 1611a | 51.28 µs · 18416B · 44a | 84.7 µs · 1872B · 25a | 77.07 µs · 5704B · 30a | — | 80.98 µs · 4368B · 795a |
| sort-stress | 501.4 µs · 5136B · 512a | 508.4 µs · 5136B · 512a | 12.83 µs · 5136B · 512a | 39.1× | — | 5.795 µs · 1024B · 2a | 1.302 ms · 270945B · 19965a | 840.2 µs · 36568B · 365a | 1.789 ms · 49104B · 3621a | 1.774 ms · 23448B · 2034a | — | 926.9 µs · 17474B · 1708a |
| spectral-norm | 529.1 µs · 648B · 6a | 457.8 µs · 648B · 6a | 8.414 µs · 648B · 6a | 62.9× | — | 7.032 µs · 576B · 3a | 2.086 ms · 571371B · 54822a | 1.088 ms · 234784B · 925a | 2.462 ms · 275338B · 33729a | 4.701 ms · 2457137B · 52718a | — | 4.228 ms · 2448031B · 74233a |
| string-build | 729.5 µs · 85408B · 4107a | 724.4 µs · 85408B · 4107a | 616.2 µs · 85408B · 4107a | 1.18× | — | 356.6 µs · 855890B · 5001a | 2.413 ms · 2211274B · 36101a | 2.294 ms · 1078113B · 13859a | 3.132 ms · 1147385B · 26883a | 2.457 ms · 1326243B · 24469a | — | 3.14 ms · 2372780B · 41432a |
| string-compare | 1.085 ms · 21136B · 1284a | 1.085 ms · 21136B · 1284a | 220 µs · 21136B · 1284a | 4.93× | — | 64.47 µs · 9632B · 1585a | 1.68 ms · 296281B · 20528a | 1.127 ms · 59712B · 3431a | 2.328 ms · 50744B · 3848a | 1.797 ms · 209521B · 5520a | — | 2.378 ms · 503610B · 31846a |
| struct-tree-walk | 282 µs · 768B · 8a | 281.4 µs · 768B · 8a | 107.5 µs · 768B · 8a | 2.62× | — | 1.888 ns · 0B · 0a | 652.8 µs · 458379B · 5114a | 1.369 ms · 818512B · 11253a | 1.081 ms · 558960B · 6149a | 3.577 ms · 2570665B · 34797a | — | 1.221 µs · 600B · 17a |
| tail-ping-pong | 29.37 µs · 0B · 0a | 29.35 µs · 0B · 0a | 480.2 ns · 0B · 0a | 61.2× | — | 890.7 ns · 0B · 0a | 108.3 µs · 106112B · 2001a | 36.02 µs · 21856B · 85a | 113.1 µs · 13984B · 1726a | 118.9 µs · 14280B · 1731a | — | 552.9 µs · 348834B · 9016a |
| tail-sum | 29.45 µs · 0B · 0a | 29.52 µs · 0B · 0a | 659.2 ns · 0B · 0a | 44.7× | — | 300.9 ns · 0B · 0a | 108.9 µs · 106112B · 2001a | 36.22 µs · 21856B · 85a | 114.2 µs · 13984B · 1726a | 119 µs · 14280B · 1731a | — | 84.38 µs · 4608B · 1017a |
| typed-array-sum | 7.367 µs · 0B · 0a | 7.362 µs · 0B · 0a | 260.6 ns · 0B · 0a | 28.3× | 260.3 ns · 8B · 1a | 1.898 ns · 0B · 0a | 46.77 µs · 94208B · 513a | 8.443 µs · 4000B · 15a | 27.44 µs · 2080B · 238a | 17.57 µs · 2496B · 246a | — | — |
| xorshift-i64 | 113.7 µs · 18424B · 2046a | 112.8 µs · 18424B · 2046a | 1.349 µs · 18424B · 2046a | 84.3× | — | 360.2 ns · 0B · 0a | 103.5 µs · 107608B · 2188a | — | 1.011 ms · 447249B · 12735a | 434 µs · 199938B · 7772a | — | 101.3 µs · 13545B · 1317a |

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
