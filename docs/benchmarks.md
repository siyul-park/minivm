# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.905 µs · 0B · 0a | 8.898 µs · 0B · 0a | 5.364 µs · 0B · 0a | 1.66× | — | 2.238 µs · 1024B · 128a | 38.37 µs · 96289B · 388a | 17.77 µs · 14376B · 256a | 78.28 µs · 78016B · 770a | 14.81 µs · 5712B · 266a | — | 31.4 µs · 2308B · 276a |
| binary-trees | 1.966 ms · 768B · 8a | 1.967 ms · 768B · 8a | 596 µs · 768B · 8a | 3.3× | — | 334.5 µs · 201937B · 8414a | 4.13 ms · 1468190B · 62877a | 10.74 ms · 6995677B · 92703a | 9.083 ms · 5198697B · 69052a | 28.24 ms · 19457605B · 280714a | — | 29.5 ms · 15128177B · 446106a |
| branch-tree | 1.345 µs · 0B · 0a | 19.69 ns · 0B · 0a | 89.82 ns · 0B · 0a | 15× | 623.5 ns · 16B · 1a | 160.4 ns · 0B · 0a | 47.28 µs · 95384B · 660a | 15.11 µs · 2464B · 9a | 26.49 µs · 1992B · 196a | 24.31 µs · 2168B · 203a | — | 20.66 µs · 1640B · 219a |
| closure-counter | 5.554 µs · 64B · 2a | 5.494 µs · 64B · 2a | 933.4 ns · 64B · 2a | 5.95× | — | 40.53 ns · 0B · 0a | 42.82 µs · 92272B · 261a | 13.08 µs · 151B · 3a | 19.36 µs · 1264B · 13a | 82.41 µs · 58312B · 659a | — | 89.93 µs · 35628B · 922a |
| coroutine-gen | 78.78 µs · 128B · 2a | 78.99 µs · 128B · 2a | 78.74 µs · 128B · 2a | 1× | — | 41.73 µs · 208B · 7a | — | 335.8 µs · 149936B · 649a | 615.7 µs · 330649B · 6719a | 204 µs · 91800B · 3646a | — | — |
| fannkuch | 831.2 µs · 34608B · 1442a | 820.1 µs · 34608B · 1442a | 226.5 µs · 34608B · 1442a | 3.67× | — | 35.98 µs · 17280B · 720a | 2.054 ms · 456578B · 25621a | 1.552 ms · 443136B · 1460a | 19.48 ms · 14005332B · 164946a | 3.441 ms · 1367682B · 16944a | — | 4.897 ms · 2501439B · 88439a |
| fnv1a64 | 126.5 µs · 16400B · 2049a | 125.1 µs · 16400B · 2049a | 1.163 µs · 16400B · 2049a | 109× | — | 902.4 ns · 0B · 0a | 166.4 µs · 120808B · 3838a | — | 1.512 ms · 608253B · 18475a | 403.5 µs · 162921B · 7429a | — | 153.1 µs · 17282B · 2074a |
| host-call | 460.1 µs · 41752B · 3080a | 476 µs · 41752B · 3080a | 475 µs · 41752B · 3080a | 0.969× | — | 3.316 µs · 0B · 0a | 185.1 µs · 139264B · 3073a | 131.7 µs · 22536B · 1079a | 814.7 µs · 158048B · 14585a | 105.7 µs · 14920B · 1799a | — | 581.9 µs · 340638B · 9231a |
| i64-wide-fib | 28.16 ms · 2913440B · 364178a | 21.06 ms · 1942304B · 242785a | 680.5 µs · 16B · 1a | 41.4× | — | 239.4 µs · 0B · 0a | 24.7 µs · 90520B · 52a | — | 164 ms · 64193488B · 1942306a | 357.3 ms · 253472736B · 4248841a | — | 135.5 ms · 98686592B · 2138708a |
| indirect-recursive-fib | 1.194 ms · 0B · 0a | 1.193 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.7× | 98.71 µs · 8B · 1a | 35.26 µs · 0B · 0a | 2.103 ms · 319345B · 28655a | 1.945 ms · 704B · 2a | 2.534 ms · 4680B · 39a | 11.99 ms · 10158189B · 109494a | — | 25.07 ms · 13061343B · 394061a |
| iterative-fib | 1.137 µs · 0B · 0a | 1.074 µs · 0B · 0a | 85.59 ns · 0B · 0a | 13.3× | 84.59 ns · 8B · 1a | 12.41 ns · 0B · 0a | 24.62 µs · 90592B · 61a | 1.155 µs · 160B · 0a | 4.259 µs · 368B · 20a | 6.317 µs · 2448B · 88a | — | 6.201 µs · 944B · 81a |
| mandelbrot | 290.6 µs · 0B · 0a | 247.7 µs · 0B · 0a | 11.25 µs · 0B · 0a | 25.8× | — | 6.322 µs · 0B · 0a | 982.7 µs · 292345B · 25008a | 509.9 µs · 169407B · 661a | 8.451 ms · 5558652B · 104191a | 1.542 ms · 324980B · 23643a | — | 1.539 ms · 365400B · 22513a |
| map-ops | 71.43 µs · 8816B · 3a | 71.53 µs · 8816B · 3a | 11.23 µs · 8816B · 3a | 6.36× | — | 11.13 µs · 4904B · 3a | — | 282.2 µs · 114327B · 198a | 386.4 µs · 66728B · 4078a | — | — | 1.161 ms · 501522B · 15813a |
| mat-mul | 376.2 µs · 6216B · 6a | 375 µs · 6216B · 6a | 9.005 µs · 6216B · 6a | 41.8× | — | 5.855 µs · 6144B · 3a | 1.405 ms · 431714B · 34871a | 673.2 µs · 143168B · 478a | 1.214 ms · 61304B · 537a | 1.465 ms · 90712B · 9350a | — | 928.1 µs · 36613B · 6036a |
| n-body | 612.4 µs · 504B · 14a | 583.3 µs · 504B · 14a | 19.3 µs · 504B · 14a | 31.7× | — | 7.358 µs · 0B · 0a | 1.543 ms · 386898B · 35892a | 1.278 ms · 258913B · 2002a | 3.391 ms · 658436B · 81545a | 2.446 ms · 382714B · 34975a | — | 1.749 ms · 445471B · 20669a |
| n-queens | 430.7 µs · 120B · 6a | 430.9 µs · 120B · 6a | 31.64 µs · 120B · 6a | 13.6× | — | 6.008 µs · 0B · 0a | 971.9 µs · 234865B · 17784a | 574.7 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.453 ms · 410346B · 5147a | — | 2.034 ms · 766003B · 33371a |
| naive-checksum | 252.1 µs · 0B · 0a | 142.2 µs · 0B · 0a | 8.73 µs · 0B · 0a | 28.9× | — | 2.759 µs · 0B · 0a | 631.8 µs · 235753B · 18206a | 485.4 µs · 174768B · 682a | 887.6 µs · 155641B · 19402a | 803.8 µs · 163817B · 20409a | — | 312 µs · 8977B · 2071a |
| permutation-flips | 129.1 µs · 7680B · 128a | 129.3 µs · 7680B · 128a | 17.95 µs · 7680B · 128a | 7.19× | — | 1.876 ns · 0B · 0a | 580.4 µs · 292857B · 9856a | 239.6 µs · 78808B · 451a | 530.9 µs · 122504B · 765a | 506.1 µs · 111976B · 2368a | — | 1.071 µs · 528B · 13a |
| recursive-fib-20 | 712.7 µs · 0B · 0a | 719.6 µs · 0B · 0a | 47.75 µs · 0B · 0a | 14.9× | 42.77 µs · 8B · 1a | 21.46 µs · 0B · 0a | 1.975 ms · 319346B · 28655a | 2.194 ms · 703B · 2a | 2.978 ms · 4680B · 39a | 12.19 ms · 9807915B · 109494a | — | 11.51 ms · 8302702B · 192848a |
| recursive-fib-35 | 971.3 ms · 0B · 0a | 981.1 ms · 0B · 0a | 63.93 ms · 0B · 0a | 15.2× | 58.2 ms · 8B · 1a | 29.07 ms · 0B · 0a | 2.608 s · 312797328B · 39088167a | 3.052 s · 971008B · 3793a | 4.071 s · 375360B · 46373a | 16.52 s · 13378018464B · 149349995a | — | — |
| sieve | 20.55 µs · 1048B · 2a | 20.45 µs · 1048B · 2a | 1.01 µs · 1048B · 2a | 20.3× | 1.029 µs · 8B · 1a | 371.9 ns · 0B · 0a | 146 µs · 122504B · 1611a | 51.54 µs · 18415B · 44a | 84.14 µs · 1872B · 25a | 77.46 µs · 5704B · 30a | — | 81.92 µs · 4368B · 795a |
| sort-stress | 505.1 µs · 5136B · 512a | 509.6 µs · 5136B · 512a | 12.97 µs · 5136B · 512a | 38.9× | — | 5.444 µs · 1024B · 2a | 1.309 ms · 270945B · 19965a | 858.6 µs · 36567B · 365a | 1.805 ms · 49104B · 3621a | 1.772 ms · 23448B · 2034a | — | 925.1 µs · 17476B · 1708a |
| spectral-norm | 534.9 µs · 648B · 6a | 462.4 µs · 648B · 6a | 8.48 µs · 648B · 6a | 63.1× | — | 7.095 µs · 576B · 3a | 2.09 ms · 571370B · 54822a | 1.099 ms · 234784B · 925a | 2.534 ms · 275337B · 33729a | 4.839 ms · 2457133B · 52718a | — | 4.313 ms · 2448027B · 74233a |
| string-build | 726.8 µs · 85408B · 4107a | 720.4 µs · 85408B · 4107a | 613.1 µs · 85408B · 4107a | 1.19× | — | 433.2 µs · 855891B · 5001a | 2.545 ms · 2211272B · 36101a | 2.39 ms · 1078111B · 13859a | 3.196 ms · 1147383B · 26883a | 2.509 ms · 1326243B · 24469a | — | 3.166 ms · 2372782B · 41432a |
| string-compare | 1.09 ms · 21136B · 1284a | 1.091 ms · 21136B · 1284a | 223.3 µs · 21136B · 1284a | 4.88× | — | 64.87 µs · 9632B · 1585a | 1.672 ms · 296281B · 20528a | 1.14 ms · 59712B · 3431a | 2.323 ms · 50744B · 3848a | 1.846 ms · 209521B · 5520a | — | 2.39 ms · 503610B · 31846a |
| struct-tree-walk | 282 µs · 768B · 8a | 282.4 µs · 768B · 8a | 107.6 µs · 768B · 8a | 2.62× | — | 1.876 ns · 0B · 0a | 669.1 µs · 458379B · 5114a | 1.432 ms · 818511B · 11253a | 1.107 ms · 558961B · 6149a | 3.61 ms · 2570667B · 34797a | — | 1.245 µs · 600B · 17a |
| tail-ping-pong | 29.36 µs · 0B · 0a | 29.48 µs · 0B · 0a | 485.1 ns · 0B · 0a | 60.5× | — | 890.9 ns · 0B · 0a | 113.8 µs · 106112B · 2001a | 36.84 µs · 21856B · 85a | 114.8 µs · 13984B · 1726a | 118.2 µs · 14280B · 1731a | — | 561.8 µs · 348832B · 9016a |
| tail-sum | 29.45 µs · 0B · 0a | 29.48 µs · 0B · 0a | 674.7 ns · 0B · 0a | 43.7× | — | 302.6 ns · 0B · 0a | 112.4 µs · 106112B · 2001a | 37.01 µs · 21856B · 85a | 114.9 µs · 13984B · 1726a | 118.6 µs · 14280B · 1731a | — | 84.72 µs · 4608B · 1017a |
| typed-array-sum | 7.346 µs · 0B · 0a | 7.364 µs · 0B · 0a | 266.5 ns · 0B · 0a | 27.6× | 263 ns · 8B · 1a | 1.881 ns · 0B · 0a | 48.21 µs · 94208B · 513a | 8.587 µs · 4000B · 15a | 27.73 µs · 2080B · 238a | 17.32 µs · 2496B · 246a | — | — |
| xorshift-i64 | 114 µs · 18424B · 2046a | 113.8 µs · 18424B · 2046a | 1.473 µs · 18424B · 2046a | 77.4× | — | 356.1 ns · 0B · 0a | 106.7 µs · 107608B · 2188a | — | 1.024 ms · 447258B · 12736a | 452.2 µs · 199938B · 7772a | — | 102.4 µs · 13546B · 1317a |

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
