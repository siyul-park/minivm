# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.986 µs · 0B · 0a | 8.944 µs · 0B · 0a | 5.302 µs · 0B · 0a | 1.69× | — | 2.22 µs · 1024B · 128a | 37.26 µs · 96288B · 388a | 16.98 µs · 14376B · 256a | 69.77 µs · 78016B · 770a | 14.11 µs · 5712B · 266a | — | 30.91 µs · 2308B · 276a |
| binary-trees | 1.962 ms · 768B · 8a | 1.965 ms · 768B · 8a | 594.8 µs · 768B · 8a | 3.3× | — | 288.9 µs · 201936B · 8414a | 3.851 ms · 1468190B · 62877a | 10.44 ms · 6995679B · 92703a | 8.837 ms · 5198701B · 69052a | 27.44 ms · 19457576B · 280714a | — | 29.52 ms · 15128174B · 446106a |
| branch-tree | 1.38 µs · 0B · 0a | 20.12 ns · 0B · 0a | 84.68 ns · 0B · 0a | 16.3× | 625.4 ns · 16B · 1a | 160.6 ns · 0B · 0a | 45.26 µs · 95384B · 660a | 15.28 µs · 2464B · 9a | 26.51 µs · 1992B · 196a | 23.98 µs · 2168B · 203a | — | 20.54 µs · 1640B · 219a |
| closure-counter | 5.571 µs · 64B · 2a | 5.8 µs · 64B · 2a | 910.1 ns · 64B · 2a | 6.12× | — | 40.11 ns · 0B · 0a | 40.93 µs · 92272B · 261a | 13.06 µs · 151B · 3a | 19.18 µs · 1264B · 13a | 78.77 µs · 58312B · 659a | — | 90.95 µs · 35629B · 922a |
| coroutine-gen | 79.91 µs · 128B · 2a | 80.25 µs · 128B · 2a | 80.01 µs · 128B · 2a | 0.999× | — | 42.3 µs · 208B · 7a | — | 336 µs · 149936B · 649a | 597.4 µs · 330649B · 6719a | 200.1 µs · 91800B · 3646a | — | — |
| fannkuch | 827.8 µs · 34608B · 1442a | 815.7 µs · 34608B · 1442a | 219.1 µs · 34608B · 1442a | 3.78× | — | 35.62 µs · 17280B · 720a | 2.034 ms · 456578B · 25621a | 1.548 ms · 443136B · 1460a | 19.16 ms · 14005336B · 164946a | 3.334 ms · 1367679B · 16944a | — | 4.688 ms · 2501412B · 88439a |
| fnv1a64 | 122 µs · 16400B · 2049a | 121.7 µs · 16400B · 2049a | 1.157 µs · 16400B · 2049a | 105× | — | 902.6 ns · 0B · 0a | 164.2 µs · 120808B · 3838a | — | 1.486 ms · 608260B · 18475a | 396.1 µs · 162921B · 7429a | — | 153.3 µs · 17282B · 2074a |
| host-call | 475.3 µs · 41752B · 3080a | 479 µs · 41752B · 3080a | 477.7 µs · 41752B · 3080a | 0.995× | — | 3.312 µs · 0B · 0a | 179.8 µs · 139264B · 3073a | 130.8 µs · 22536B · 1079a | 786.5 µs · 158048B · 14585a | 105.5 µs · 14920B · 1799a | — | 585 µs · 340636B · 9231a |
| i64-wide-fib | 28.25 ms · 2913456B · 364178a | 20.71 ms · 1942304B · 242785a | 682.2 µs · 16B · 1a | 41.4× | — | 238.8 µs · 0B · 0a | 22.35 µs · 90520B · 52a | — | 157.2 ms · 64193328B · 1942306a | 344.2 ms · 253472736B · 4248840a | — | 129.5 ms · 98686597B · 2138708a |
| indirect-recursive-fib | 1.2 ms · 0B · 0a | 1.201 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.8× | 100.4 µs · 8B · 1a | 34.75 µs · 0B · 0a | 2.097 ms · 319346B · 28655a | 1.947 ms · 704B · 2a | 2.536 ms · 4680B · 39a | 11.39 ms · 10158200B · 109494a | — | 24.32 ms · 13061324B · 394061a |
| iterative-fib | 1.129 µs · 0B · 0a | 1.072 µs · 0B · 0a | 78.43 ns · 0B · 0a | 14.4× | 85.32 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.55 µs · 90592B · 61a | 1.16 µs · 159B · 0a | 4.238 µs · 368B · 20a | 6.141 µs · 2448B · 88a | — | 6.159 µs · 944B · 81a |
| mandelbrot | 293.7 µs · 0B · 0a | 250.3 µs · 0B · 0a | 11.25 µs · 0B · 0a | 26.1× | — | 6.335 µs · 0B · 0a | 981.4 µs · 292345B · 25008a | 500 µs · 169408B · 661a | 8.392 ms · 5558644B · 104191a | 1.497 ms · 324977B · 23643a | — | 1.532 ms · 365402B · 22513a |
| map-ops | 71.21 µs · 8816B · 3a | 71.24 µs · 8816B · 3a | 11.09 µs · 8816B · 3a | 6.42× | — | 11.11 µs · 4904B · 3a | — | 281.9 µs · 114328B · 198a | 388.9 µs · 66728B · 4078a | — | — | 1.173 ms · 501522B · 15813a |
| mat-mul | 375.5 µs · 6216B · 6a | 375.2 µs · 6216B · 6a | 8.752 µs · 6216B · 6a | 42.9× | — | 5.689 µs · 6144B · 3a | 1.363 ms · 431714B · 34871a | 678.5 µs · 143168B · 478a | 1.218 ms · 61304B · 537a | 1.461 ms · 90713B · 9350a | — | 916.1 µs · 36616B · 6036a |
| n-body | 614.8 µs · 504B · 14a | 581.7 µs · 504B · 14a | 19.1 µs · 504B · 14a | 32.2× | — | 7.348 µs · 0B · 0a | 1.547 ms · 386898B · 35892a | 1.239 ms · 258913B · 2002a | 3.352 ms · 658435B · 81545a | 2.437 ms · 382714B · 34975a | — | 1.743 ms · 445467B · 20669a |
| n-queens | 430.2 µs · 120B · 6a | 450.7 µs · 120B · 6a | 31.62 µs · 120B · 6a | 13.6× | — | 6.015 µs · 0B · 0a | 955.3 µs · 234865B · 17784a | 576.5 µs · 14423B · 63a | 1.093 ms · 7424B · 35a | 1.439 ms · 410346B · 5147a | — | 2.002 ms · 766001B · 33371a |
| naive-checksum | 252.3 µs · 0B · 0a | 141.7 µs · 0B · 0a | 8.724 µs · 0B · 0a | 28.9× | — | 2.758 µs · 0B · 0a | 620.6 µs · 235753B · 18206a | 479.5 µs · 174767B · 682a | 886.2 µs · 155640B · 19402a | 801.1 µs · 163816B · 20409a | — | 312.2 µs · 8977B · 2071a |
| permutation-flips | 128.7 µs · 7680B · 128a | 128.6 µs · 7680B · 128a | 17.24 µs · 7680B · 128a | 7.47× | — | 1.876 ns · 0B · 0a | 565.6 µs · 292857B · 9856a | 239.6 µs · 78808B · 451a | 529.3 µs · 122504B · 765a | 494 µs · 111976B · 2368a | — | 1.031 µs · 528B · 13a |
| recursive-fib-20 | 710.6 µs · 0B · 0a | 717.4 µs · 0B · 0a | 47.21 µs · 0B · 0a | 15.1× | 42.77 µs · 8B · 1a | 21.51 µs · 0B · 0a | 1.934 ms · 319345B · 28655a | 2.169 ms · 704B · 2a | 2.964 ms · 4680B · 39a | 11.34 ms · 9807910B · 109494a | — | 10.67 ms · 8302694B · 192848a |
| recursive-fib-35 | 975.4 ms · 0B · 0a | 982 ms · 0B · 0a | 63.94 ms · 0B · 0a | 15.3× | 58.26 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.569 s · 312797392B · 39088167a | 2.96 s · 971008B · 3793a | 4.032 s · 375360B · 46373a | 15.56 s · 13378018000B · 149350000a | — | — |
| sieve | 20.44 µs · 1048B · 2a | 20.41 µs · 1048B · 2a | 975.9 ns · 1048B · 2a | 20.9× | 1.031 µs · 8B · 1a | 351 ns · 0B · 0a | 140.5 µs · 122504B · 1611a | 51.67 µs · 18416B · 44a | 84.32 µs · 1872B · 25a | 77.58 µs · 5704B · 30a | — | 81.41 µs · 4368B · 795a |
| sort-stress | 502.8 µs · 5136B · 512a | 512.2 µs · 5136B · 512a | 12.93 µs · 5136B · 512a | 38.9× | — | 5.896 µs · 1024B · 2a | 1.287 ms · 270945B · 19965a | 859 µs · 36568B · 365a | 1.793 ms · 49104B · 3621a | 1.774 ms · 23448B · 2034a | — | 923.4 µs · 17473B · 1708a |
| spectral-norm | 531.7 µs · 648B · 6a | 463.3 µs · 648B · 6a | 8.387 µs · 648B · 6a | 63.4× | — | 7.081 µs · 576B · 3a | 2.055 ms · 571370B · 54822a | 1.108 ms · 234784B · 925a | 2.447 ms · 275337B · 33729a | 4.799 ms · 2457133B · 52718a | — | 4.1 ms · 2448020B · 74233a |
| string-build | 726.3 µs · 85408B · 4107a | 720.7 µs · 85408B · 4107a | 601.4 µs · 85408B · 4107a | 1.21× | — | 403.8 µs · 855890B · 5001a | 2.424 ms · 2211273B · 36101a | 2.35 ms · 1078112B · 13859a | 3.16 ms · 1147384B · 26883a | 2.461 ms · 1326242B · 24469a | — | 3.125 ms · 2372779B · 41432a |
| string-compare | 1.092 ms · 21136B · 1284a | 1.142 ms · 21136B · 1284a | 218.4 µs · 21136B · 1284a | 5× | — | 63.14 µs · 9632B · 1585a | 1.658 ms · 296281B · 20528a | 1.132 ms · 59712B · 3431a | 2.333 ms · 50744B · 3848a | 1.808 ms · 209522B · 5520a | — | 2.379 ms · 503614B · 31846a |
| struct-tree-walk | 281 µs · 768B · 8a | 281.5 µs · 768B · 8a | 107.2 µs · 768B · 8a | 2.62× | — | 1.889 ns · 0B · 0a | 653.8 µs · 458379B · 5114a | 1.362 ms · 818512B · 11253a | 1.081 ms · 558960B · 6149a | 3.496 ms · 2570666B · 34797a | — | 1.202 µs · 600B · 17a |
| tail-ping-pong | 29.51 µs · 0B · 0a | 29.29 µs · 0B · 0a | 479.6 ns · 0B · 0a | 61.5× | — | 891.5 ns · 0B · 0a | 109.1 µs · 106112B · 2001a | 36.15 µs · 21856B · 85a | 112.3 µs · 13984B · 1726a | 118.4 µs · 14280B · 1731a | — | 555.2 µs · 348833B · 9016a |
| tail-sum | 29.45 µs · 0B · 0a | 29.27 µs · 0B · 0a | 675.2 ns · 0B · 0a | 43.6× | — | 300.8 ns · 0B · 0a | 109.7 µs · 106112B · 2001a | 36.67 µs · 21856B · 85a | 112.2 µs · 13984B · 1726a | 118.7 µs · 14280B · 1731a | — | 84.79 µs · 4608B · 1017a |
| typed-array-sum | 7.343 µs · 0B · 0a | 7.358 µs · 0B · 0a | 262.5 ns · 0B · 0a | 28× | 261.8 ns · 8B · 1a | 1.898 ns · 0B · 0a | 48.67 µs · 94208B · 513a | 8.442 µs · 4000B · 15a | 27.38 µs · 2080B · 238a | 17.24 µs · 2496B · 246a | — | — |
| xorshift-i64 | 113.1 µs · 18424B · 2046a | 113.1 µs · 18424B · 2046a | 1.406 µs · 18424B · 2046a | 80.4× | — | 360.2 ns · 0B · 0a | 104.9 µs · 107608B · 2188a | — | 994.7 µs · 447251B · 12735a | 418.3 µs · 199938B · 7772a | — | 101.6 µs · 13545B · 1317a |

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
