# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.262 µs · 0B · 0a | 8.271 µs · 0B · 0a | 5.418 µs · 0B · 0a | 1.52× | — | 2.281 µs · 1024B · 128a | 36.69 µs · 96288B · 388a | 16.86 µs · 14375B · 256a | 69.67 µs · 78016B · 770a | 14.94 µs · 5712B · 266a | — | 30.87 µs · 2308B · 276a |
| binary-trees | 1.688 ms · 768B · 8a | 1.667 ms · 768B · 8a | 553 µs · 768B · 8a | 3.05× | — | 283.2 µs · 201936B · 8414a | 3.834 ms · 1468183B · 62877a | 10.28 ms · 6995685B · 92703a | 8.893 ms · 5198856B · 69052a | 26.72 ms · 19457607B · 280714a | — | 29.25 ms · 15128173B · 446106a |
| branch-tree | 937.7 ns · 0B · 0a | 18.87 ns · 0B · 0a | 81.96 ns · 0B · 0a | 11.4× | 627.1 ns · 16B · 1a | 160.5 ns · 0B · 0a | 44.39 µs · 95384B · 660a | 15.28 µs · 2463B · 9a | 26.5 µs · 1992B · 196a | 23.94 µs · 2168B · 203a | — | 20.59 µs · 1640B · 219a |
| closure-counter | 5.197 µs · 64B · 2a | 5.169 µs · 64B · 2a | 915.8 ns · 64B · 2a | 5.67× | — | 40.12 ns · 0B · 0a | 40.33 µs · 92272B · 261a | 13.08 µs · 151B · 3a | 19.28 µs · 1264B · 13a | 79.24 µs · 58312B · 659a | — | 89.29 µs · 35629B · 922a |
| coroutine-gen | 67.13 µs · 128B · 2a | 66.92 µs · 128B · 2a | 67.11 µs · 128B · 2a | 1× | — | 42.22 µs · 208B · 7a | — | 326.9 µs · 149936B · 649a | 591.5 µs · 330650B · 6719a | 198.5 µs · 91800B · 3646a | — | — |
| fannkuch | 660.4 µs · 34608B · 1442a | 633.7 µs · 34608B · 1442a | 221.2 µs · 34608B · 1442a | 2.98× | — | 35.87 µs · 17280B · 720a | 2.031 ms · 456580B · 25621a | 1.553 ms · 443138B · 1460a | 19.46 ms · 14005336B · 164946a | 3.369 ms · 1367681B · 16944a | — | 4.755 ms · 2501417B · 88439a |
| fnv1a64 | 119.8 µs · 16400B · 2049a | 118.5 µs · 16400B · 2049a | 1.153 µs · 16400B · 2049a | 104× | — | 902.6 ns · 0B · 0a | 160.6 µs · 120808B · 3838a | — | 1.441 ms · 608255B · 18475a | 388.2 µs · 162921B · 7429a | — | 155.2 µs · 17282B · 2074a |
| host-call | 456.1 µs · 41752B · 3080a | 471.7 µs · 41752B · 3080a | 469.3 µs · 41752B · 3080a | 0.972× | — | 3.312 µs · 0B · 0a | 178 µs · 139264B · 3073a | 132.5 µs · 22536B · 1079a | 827.9 µs · 158048B · 14585a | 104.9 µs · 14920B · 1799a | — | 584.8 µs · 340638B · 9231a |
| i64-wide-fib | 27.43 ms · 2913456B · 364178a | 20.58 ms · 1942304B · 242785a | 680.3 µs · 16B · 1a | 40.3× | — | 238.8 µs · 0B · 0a | 22.77 µs · 90520B · 52a | — | 156.3 ms · 64193296B · 1942306a | 338.1 ms · 253472608B · 4248839a | — | 133.2 ms · 98686570B · 2138708a |
| indirect-recursive-fib | 1.025 ms · 0B · 0a | 1.012 ms · 0B · 0a | 60.49 µs · 0B · 0a | 16.9× | 100.5 µs · 8B · 1a | 35.1 µs · 0B · 0a | 2.088 ms · 319345B · 28655a | 1.947 ms · 704B · 2a | 2.562 ms · 4680B · 39a | 11.26 ms · 10158190B · 109494a | — | 24.19 ms · 13061318B · 394061a |
| iterative-fib | 886.8 ns · 0B · 0a | 881.1 ns · 0B · 0a | 74.29 ns · 0B · 0a | 11.9× | 86.17 ns · 8B · 1a | 11.9 ns · 0B · 0a | 23.89 µs · 90592B · 61a | 1.19 µs · 160B · 0a | 4.254 µs · 368B · 20a | 6.096 µs · 2448B · 88a | — | 6.157 µs · 944B · 81a |
| mandelbrot | 229.5 µs · 0B · 0a | 191.3 µs · 0B · 0a | 11.24 µs · 0B · 0a | 20.4× | — | 6.321 µs · 0B · 0a | 957.4 µs · 292345B · 25008a | 481.8 µs · 169408B · 661a | 8.144 ms · 5558643B · 104191a | 1.539 ms · 324978B · 23643a | — | 1.54 ms · 365392B · 22513a |
| map-churn | 2.409 ms · 786608B · 49a | 2.415 ms · 786608B · 49a | 616 µs · 786608B · 49a | 3.91× | — | 685.6 µs · 19024B · 5a | — | 11.54 ms · 7213826B · 6564a | 15.78 ms · 2369291B · 176707a | — | — | 53.67 ms · 24395141B · 725811a |
| map-iter | 3.383 ms · 249040B · 24645a | 3.377 ms · 249040B · 24645a | 3.323 ms · 249040B · 24645a | 1.02× | — | 534 µs · 19024B · 5a | — | 12.34 ms · 6177856B · 26325a | 22.22 ms · 13957133B · 201529a | — | — | 5.921 ms · 972929B · 93724a |
| map-ops | 60.97 µs · 12976B · 4a | 60.98 µs · 12976B · 4a | 11.18 µs · 12976B · 4a | 5.45× | — | 10.75 µs · 4904B · 3a | — | 272.7 µs · 114328B · 198a | 385.6 µs · 66728B · 4078a | — | — | 1.185 ms · 501519B · 15813a |
| map-string | 6.507 ms · 174184B · 9244a | 6.527 ms · 174184B · 9244a | 2.115 ms · 758256B · 16026a | 3.08× | — | 1.114 ms · 52632B · 2112a | 8.378 ms · 1463607B · 126292a | 9.433 ms · 967147B · 9917a | 16.11 ms · 590606B · 59508a | 8.786 ms · 1037295B · 80209a | — | 9.279 ms · 1281220B · 94104a |
| mat-mul | 311.9 µs · 6216B · 6a | 311.3 µs · 6216B · 6a | 8.376 µs · 6216B · 6a | 37.2× | — | 5.333 µs · 6144B · 3a | 1.362 ms · 431715B · 34871a | 670.8 µs · 143168B · 478a | 1.204 ms · 61304B · 537a | 1.443 ms · 90712B · 9350a | — | 922.6 µs · 36614B · 6036a |
| n-body | 498.1 µs · 504B · 14a | 411.7 µs · 504B · 14a | 18.82 µs · 504B · 14a | 26.5× | — | 7.346 µs · 0B · 0a | 1.499 ms · 386898B · 35892a | 1.215 ms · 258913B · 2002a | 3.299 ms · 658436B · 81545a | 2.427 ms · 382714B · 34975a | — | 1.72 ms · 445466B · 20669a |
| n-queens | 276.9 µs · 120B · 6a | 277.6 µs · 120B · 6a | 31.6 µs · 120B · 6a | 8.76× | — | 5.995 µs · 0B · 0a | 948 µs · 234865B · 17784a | 570.4 µs · 14423B · 63a | 1.097 ms · 7424B · 35a | 1.425 ms · 410346B · 5147a | — | 2.059 ms · 766008B · 33371a |
| naive-checksum | 189.1 µs · 0B · 0a | 108 µs · 0B · 0a | 8.723 µs · 0B · 0a | 21.7× | — | 2.759 µs · 0B · 0a | 617 µs · 235753B · 18206a | 480.9 µs · 174768B · 682a | 881.6 µs · 155640B · 19402a | 791.5 µs · 163816B · 20409a | — | 312.9 µs · 8977B · 2071a |
| permutation-flips | 103.7 µs · 7680B · 128a | 103.9 µs · 7680B · 128a | 16.79 µs · 7680B · 128a | 6.18× | — | 1.877 ns · 0B · 0a | 554.5 µs · 292857B · 9856a | 231.5 µs · 78808B · 451a | 523.4 µs · 122504B · 765a | 493.1 µs · 111976B · 2368a | — | 1.04 µs · 528B · 13a |
| recursive-fib-20 | 641.9 µs · 0B · 0a | 643.2 µs · 0B · 0a | 47.34 µs · 0B · 0a | 13.6× | 42.73 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.953 ms · 319345B · 28655a | 2.172 ms · 704B · 2a | 2.971 ms · 4680B · 39a | 11.33 ms · 9807907B · 109494a | — | 10.73 ms · 8302651B · 192847a |
| recursive-fib-35 | 874.9 ms · 0B · 0a | 875.7 ms · 0B · 0a | 63.95 ms · 0B · 0a | 13.7× | 58.2 ms · 8B · 1a | 29.06 ms · 0B · 0a | 2.588 s · 312797536B · 39088168a | 2.957 s · 971008B · 3793a | 4.037 s · 375360B · 46373a | 15.33 s · 13378018880B · 149349974a | — | — |
| sieve | 15.23 µs · 1048B · 2a | 15.18 µs · 1048B · 2a | 920.9 ns · 1048B · 2a | 16.5× | 1.044 µs · 8B · 1a | 350.8 ns · 0B · 0a | 138.1 µs · 122504B · 1611a | 50.22 µs · 18415B · 44a | 84.36 µs · 1872B · 25a | 77.3 µs · 5704B · 30a | — | 80.92 µs · 4368B · 795a |
| sort-stress | 386.9 µs · 5136B · 512a | 368.7 µs · 5136B · 512a | 12.7 µs · 5136B · 512a | 30.5× | — | 5.792 µs · 1024B · 2a | 1.276 ms · 270946B · 19965a | 843.1 µs · 36567B · 365a | 1.79 ms · 49104B · 3621a | 1.782 ms · 23448B · 2034a | — | 926.5 µs · 17475B · 1708a |
| spectral-norm | 430.1 µs · 648B · 6a | 347.6 µs · 648B · 6a | 8.294 µs · 648B · 6a | 51.9× | — | 7.031 µs · 576B · 3a | 2.044 ms · 571370B · 54822a | 1.078 ms · 234784B · 925a | 2.457 ms · 275337B · 33729a | 4.635 ms · 2457133B · 52718a | — | 4.14 ms · 2448011B · 74233a |
| string-build | 610.7 µs · 85408B · 4107a | 603 µs · 85408B · 4107a | 473.5 µs · 85408B · 4107a | 1.29× | — | 355.8 µs · 855889B · 5001a | 2.365 ms · 2211271B · 36101a | 2.297 ms · 1078109B · 13859a | 3.11 ms · 1147382B · 26883a | 2.464 ms · 1326241B · 24469a | — | 3.135 ms · 2372775B · 41432a |
| string-compare | 1.005 ms · 21136B · 1284a | 993.2 µs · 21136B · 1284a | 217.4 µs · 21136B · 1284a | 4.62× | — | 64 µs · 9632B · 1585a | 1.662 ms · 296281B · 20528a | 1.126 ms · 59712B · 3431a | 2.325 ms · 50744B · 3848a | 1.799 ms · 209521B · 5520a | — | 2.361 ms · 503605B · 31846a |
| struct-tree-walk | 253.9 µs · 768B · 8a | 253.6 µs · 768B · 8a | 102 µs · 768B · 8a | 2.49× | — | 1.876 ns · 0B · 0a | 635.1 µs · 458379B · 5114a | 1.354 ms · 818514B · 11253a | 1.073 ms · 558961B · 6149a | 3.415 ms · 2570663B · 34797a | — | 1.216 µs · 600B · 17a |
| tail-ping-pong | 21.67 µs · 0B · 0a | 21.64 µs · 0B · 0a | 478.1 ns · 0B · 0a | 45.3× | — | 890.9 ns · 0B · 0a | 107.1 µs · 106112B · 2001a | 35.47 µs · 21856B · 85a | 111.7 µs · 13984B · 1726a | 117.8 µs · 14280B · 1731a | — | 546.3 µs · 348831B · 9016a |
| tail-sum | 21.54 µs · 0B · 0a | 21.76 µs · 0B · 0a | 663 ns · 0B · 0a | 32.5× | — | 300.8 ns · 0B · 0a | 107.6 µs · 106112B · 2001a | 35.5 µs · 21856B · 85a | 112.1 µs · 13984B · 1726a | 117 µs · 14280B · 1731a | — | 84.23 µs · 4608B · 1017a |
| typed-array-sum | 5.654 µs · 0B · 0a | 5.671 µs · 0B · 0a | 259 ns · 0B · 0a | 21.8× | 264.8 ns · 8B · 1a | 1.899 ns · 0B · 0a | 45.21 µs · 94208B · 513a | 8.575 µs · 4000B · 15a | 27.61 µs · 2080B · 238a | 17.5 µs · 2496B · 246a | — | — |
| xorshift-i64 | 109.8 µs · 18424B · 2046a | 109.5 µs · 18424B · 2046a | 1.358 µs · 18424B · 2046a | 80.9× | — | 360.3 ns · 0B · 0a | 104.6 µs · 107608B · 2188a | — | 1 ms · 447252B · 12735a | 425 µs · 199938B · 7772a | — | 101.5 µs · 13545B · 1317a |

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
