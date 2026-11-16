# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.263 µs · 0B · 0a | 9.259 µs · 0B · 0a | 5.358 µs · 0B · 0a | 1.73× | — | 2.209 µs · 1024B · 128a | 37.89 µs · 96288B · 388a | 20.49 µs · 14376B · 256a | 78.79 µs · 78016B · 770a | 14.99 µs · 5712B · 266a | — | 30.91 µs · 2308B · 276a |
| binary-trees | 1.971 ms · 768B · 8a | 1.969 ms · 768B · 8a | 596.1 µs · 768B · 8a | 3.31× | — | 284.5 µs · 201936B · 8414a | 3.86 ms · 1468185B · 62877a | 10.43 ms · 6995675B · 92703a | 8.82 ms · 5198702B · 69052a | 27.7 ms · 19457597B · 280714a | — | 28.8 ms · 15128177B · 446106a |
| branch-tree | 1.345 µs · 0B · 0a | 18.76 ns · 0B · 0a | 85.05 ns · 0B · 0a | 15.8× | 625.2 ns · 16B · 1a | 160.5 ns · 0B · 0a | 46.13 µs · 95384B · 660a | 15.07 µs · 2463B · 9a | 26.47 µs · 1992B · 196a | 24.16 µs · 2168B · 203a | — | 20.64 µs · 1640B · 219a |
| closure-counter | 5.497 µs · 64B · 2a | 5.496 µs · 64B · 2a | 914.9 ns · 64B · 2a | 6.01× | — | 40.46 ns · 0B · 0a | 40.84 µs · 92273B · 261a | 13.15 µs · 151B · 3a | 18.98 µs · 1264B · 13a | 78.72 µs · 58312B · 659a | — | 89.91 µs · 35628B · 922a |
| coroutine-gen | 79.01 µs · 128B · 2a | 79.26 µs · 128B · 2a | 79.14 µs · 128B · 2a | 0.998× | — | 42.37 µs · 208B · 7a | — | 327.5 µs · 149936B · 649a | 593 µs · 330649B · 6719a | 198.7 µs · 91800B · 3646a | — | — |
| fannkuch | 830.4 µs · 34608B · 1442a | 813.7 µs · 34608B · 1442a | 219.5 µs · 34608B · 1442a | 3.78× | — | 35.63 µs · 17280B · 720a | 2.043 ms · 456578B · 25621a | 1.534 ms · 443135B · 1460a | 19.32 ms · 14005351B · 164946a | 3.338 ms · 1367681B · 16944a | — | 4.837 ms · 2501447B · 88439a |
| fnv1a64 | 122.6 µs · 16400B · 2049a | 124 µs · 16400B · 2049a | 1.159 µs · 16400B · 2049a | 106× | — | 902.4 ns · 0B · 0a | 163.5 µs · 120808B · 3838a | — | 1.411 ms · 608253B · 18475a | 383.9 µs · 162921B · 7429a | — | 153 µs · 17282B · 2074a |
| host-call | 457.4 µs · 41752B · 3080a | 470.5 µs · 41752B · 3080a | 468.3 µs · 41752B · 3080a | 0.977× | — | 3.312 µs · 0B · 0a | 179.6 µs · 139264B · 3073a | 129.7 µs · 22536B · 1079a | 782 µs · 158048B · 14585a | 104.3 µs · 14920B · 1799a | — | 562.6 µs · 340638B · 9231a |
| i64-wide-fib | 28.23 ms · 2913456B · 364178a | 20.98 ms · 1942304B · 242785a | 681 µs · 16B · 1a | 41.5× | — | 239.2 µs · 0B · 0a | 23.88 µs · 90520B · 52a | — | 157 ms · 64193368B · 1942306a | 347.1 ms · 253473200B · 4248844a | — | 130.6 ms · 98686629B · 2138708a |
| indirect-recursive-fib | 1.19 ms · 0B · 0a | 1.186 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.7× | 100.1 µs · 8B · 1a | 35.04 µs · 0B · 0a | 2.088 ms · 319346B · 28655a | 1.949 ms · 704B · 2a | 2.527 ms · 4680B · 39a | 11.51 ms · 10158180B · 109494a | — | 24.09 ms · 13061331B · 394061a |
| iterative-fib | 1.135 µs · 0B · 0a | 1.071 µs · 0B · 0a | 82.28 ns · 0B · 0a | 13.8× | 84.69 ns · 8B · 1a | 12.41 ns · 0B · 0a | 23.41 µs · 90592B · 61a | 1.143 µs · 160B · 0a | 4.242 µs · 368B · 20a | 6.104 µs · 2448B · 88a | — | 6.168 µs · 944B · 81a |
| mandelbrot | 290.3 µs · 0B · 0a | 247.3 µs · 0B · 0a | 11.25 µs · 0B · 0a | 25.8× | — | 6.324 µs · 0B · 0a | 968.2 µs · 292345B · 25008a | 484.7 µs · 169408B · 661a | 8.22 ms · 5558658B · 104191a | 1.518 ms · 324979B · 23643a | — | 1.526 ms · 365406B · 22513a |
| map-ops | 71.05 µs · 8816B · 3a | 70.51 µs · 8816B · 3a | 10.9 µs · 8816B · 3a | 6.52× | — | 10.86 µs · 4904B · 3a | — | 278.8 µs · 114327B · 198a | 388.4 µs · 66728B · 4078a | — | — | 1.11 ms · 501521B · 15813a |
| mat-mul | 374.6 µs · 6216B · 6a | 374.1 µs · 6216B · 6a | 8.645 µs · 6216B · 6a | 43.3× | — | 5.557 µs · 6144B · 3a | 1.379 ms · 431715B · 34871a | 667.9 µs · 143168B · 478a | 1.207 ms · 61304B · 537a | 1.452 ms · 90712B · 9350a | — | 921 µs · 36614B · 6036a |
| n-body | 609.9 µs · 504B · 14a | 581.7 µs · 504B · 14a | 19.04 µs · 504B · 14a | 32× | — | 7.357 µs · 0B · 0a | 1.518 ms · 386898B · 35892a | 1.212 ms · 258913B · 2002a | 3.344 ms · 658435B · 81545a | 2.418 ms · 382714B · 34975a | — | 1.736 ms · 445470B · 20669a |
| n-queens | 429.5 µs · 120B · 6a | 430.1 µs · 120B · 6a | 31.64 µs · 120B · 6a | 13.6× | — | 6.004 µs · 0B · 0a | 949.4 µs · 234865B · 17784a | 564.4 µs · 14424B · 63a | 1.112 ms · 7424B · 35a | 1.41 ms · 410346B · 5147a | — | 1.992 ms · 766008B · 33371a |
| naive-checksum | 252.8 µs · 0B · 0a | 142 µs · 0B · 0a | 8.719 µs · 0B · 0a | 29× | — | 2.76 µs · 0B · 0a | 622.4 µs · 235753B · 18206a | 475.8 µs · 174768B · 682a | 870.1 µs · 155640B · 19402a | 780.4 µs · 163817B · 20409a | — | 310.9 µs · 8977B · 2071a |
| permutation-flips | 128.8 µs · 7680B · 128a | 128.6 µs · 7680B · 128a | 17.29 µs · 7680B · 128a | 7.45× | — | 1.88 ns · 0B · 0a | 565.5 µs · 292857B · 9856a | 233.1 µs · 78808B · 451a | 524.6 µs · 122504B · 765a | 492.6 µs · 111976B · 2368a | — | 998.5 ns · 528B · 13a |
| recursive-fib-20 | 708 µs · 0B · 0a | 725.4 µs · 0B · 0a | 47.23 µs · 0B · 0a | 15× | 42.74 µs · 8B · 1a | 21.5 µs · 0B · 0a | 1.926 ms · 319345B · 28655a | 2.179 ms · 704B · 2a | 2.977 ms · 4680B · 39a | 11.59 ms · 9807909B · 109494a | — | 10.72 ms · 8302691B · 192848a |
| recursive-fib-35 | 969.1 ms · 0B · 0a | 979.2 ms · 0B · 0a | 64.01 ms · 0B · 0a | 15.1× | 58.22 ms · 8B · 1a | 29.1 ms · 0B · 0a | 2.576 s · 312797280B · 39088167a | 2.97 s · 971008B · 3793a | 4.054 s · 375360B · 46373a | 15.89 s · 13378019472B · 149349975a | — | — |
| sieve | 20.39 µs · 1048B · 2a | 20.71 µs · 1048B · 2a | 957.7 ns · 1048B · 2a | 21.3× | 1.027 µs · 8B · 1a | 371.9 ns · 0B · 0a | 141 µs · 122504B · 1611a | 50.59 µs · 18416B · 44a | 84.6 µs · 1872B · 25a | 76.68 µs · 5704B · 30a | — | 82.2 µs · 4368B · 795a |
| sort-stress | 505.7 µs · 5136B · 512a | 519.6 µs · 5136B · 512a | 12.62 µs · 5136B · 512a | 40.1× | — | 5.347 µs · 1024B · 2a | 1.281 ms · 270945B · 19965a | 842.7 µs · 36568B · 365a | 1.797 ms · 49104B · 3621a | 1.773 ms · 23448B · 2034a | — | 923.8 µs · 17474B · 1708a |
| spectral-norm | 542 µs · 648B · 6a | 461.8 µs · 648B · 6a | 8.353 µs · 648B · 6a | 64.9× | — | 7.034 µs · 576B · 3a | 2.027 ms · 571371B · 54822a | 1.078 ms · 234784B · 925a | 2.479 ms · 275337B · 33729a | 4.594 ms · 2457134B · 52718a | — | 4.046 ms · 2448011B · 74233a |
| string-build | 719.5 µs · 85408B · 4107a | 715 µs · 85408B · 4107a | 495.2 µs · 85408B · 4107a | 1.45× | — | 366.1 µs · 855891B · 5001a | 2.417 ms · 2211276B · 36101a | 2.328 ms · 1078111B · 13859a | 3.152 ms · 1147390B · 26883a | 2.463 ms · 1326245B · 24469a | — | 3.087 ms · 2372782B · 41432a |
| string-compare | 1.082 ms · 21136B · 1284a | 1.082 ms · 21136B · 1284a | 221.7 µs · 21136B · 1284a | 4.88× | — | 63.25 µs · 9632B · 1585a | 1.656 ms · 296281B · 20528a | 1.132 ms · 59712B · 3431a | 2.323 ms · 50744B · 3848a | 1.813 ms · 209521B · 5520a | — | 2.353 ms · 503610B · 31846a |
| struct-tree-walk | 286.5 µs · 768B · 8a | 284.2 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.66× | — | 1.898 ns · 0B · 0a | 650.1 µs · 458378B · 5114a | 1.369 ms · 818512B · 11253a | 1.074 ms · 558960B · 6149a | 3.523 ms · 2570661B · 34797a | — | 1.153 µs · 600B · 17a |
| tail-ping-pong | 29.41 µs · 0B · 0a | 29.49 µs · 0B · 0a | 487.4 ns · 0B · 0a | 60.3× | — | 890.7 ns · 0B · 0a | 109.6 µs · 106112B · 2001a | 35.38 µs · 21855B · 85a | 112.6 µs · 13984B · 1726a | 116.7 µs · 14280B · 1731a | — | 550.4 µs · 348834B · 9016a |
| tail-sum | 29.48 µs · 0B · 0a | 29.63 µs · 0B · 0a | 674.9 ns · 0B · 0a | 43.7× | — | 302.5 ns · 0B · 0a | 109.5 µs · 106112B · 2001a | 35.73 µs · 21856B · 85a | 111.6 µs · 13984B · 1726a | 116.8 µs · 14280B · 1731a | — | 84.54 µs · 4608B · 1017a |
| typed-array-sum | 7.32 µs · 0B · 0a | 7.292 µs · 0B · 0a | 254.7 ns · 0B · 0a | 28.7× | 261.4 ns · 8B · 1a | 1.876 ns · 0B · 0a | 45.76 µs · 94208B · 513a | 8.269 µs · 3999B · 15a | 27.41 µs · 2080B · 238a | 17.1 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.4 µs · 18424B · 2046a | 112.4 µs · 18424B · 2046a | 1.371 µs · 18424B · 2046a | 82× | — | 356.7 ns · 0B · 0a | 103.9 µs · 107608B · 2188a | — | 975.5 µs · 447255B · 12736a | 425.5 µs · 199937B · 7772a | — | 101.6 µs · 13545B · 1317a |

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
