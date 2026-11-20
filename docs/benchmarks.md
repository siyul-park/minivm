# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.114 µs · 0B · 0a | 9.073 µs · 0B · 0a | 5.369 µs · 0B · 0a | 1.7× | — | 2.26 µs · 1024B · 128a | 42.7 µs · 96288B · 388a | 19.51 µs · 14376B · 256a | 71.57 µs · 78016B · 770a | 13.86 µs · 5712B · 266a | — | 30.85 µs · 2308B · 276a |
| binary-trees | 1.97 ms · 768B · 8a | 1.964 ms · 768B · 8a | 596.2 µs · 768B · 8a | 3.3× | — | 290.1 µs · 201936B · 8414a | 3.858 ms · 1468248B · 62877a | 10.58 ms · 6995674B · 92703a | 8.754 ms · 5198692B · 69052a | 27.12 ms · 19457582B · 280714a | — | 29.14 ms · 15128186B · 446106a |
| branch-tree | 1.338 µs · 0B · 0a | 1.875 ns · 0B · 0a | 87.93 ns · 0B · 0a | 15.2× | 629.9 ns · 16B · 1a | 160.5 ns · 0B · 0a | 46.27 µs · 95384B · 660a | 15.49 µs · 2464B · 9a | 26.72 µs · 1992B · 196a | 24.1 µs · 2168B · 203a | — | 20.52 µs · 1640B · 219a |
| closure-counter | 5.524 µs · 64B · 2a | 5.496 µs · 64B · 2a | 925.5 ns · 64B · 2a | 5.97× | — | 40.49 ns · 0B · 0a | 40.76 µs · 92272B · 261a | 12.96 µs · 152B · 3a | 19.15 µs · 1264B · 13a | 79 µs · 58312B · 659a | — | 88.05 µs · 35629B · 922a |
| coroutine-gen | 78.87 µs · 128B · 2a | 79.3 µs · 128B · 2a | 79.31 µs · 128B · 2a | 0.995× | — | 42.85 µs · 208B · 7a | — | 331.4 µs · 149936B · 649a | 591.4 µs · 330649B · 6719a | 203 µs · 91800B · 3646a | — | — |
| fannkuch | 827.8 µs · 34608B · 1442a | 814.7 µs · 34608B · 1442a | 232 µs · 34608B · 1442a | 3.57× | — | 35.6 µs · 17280B · 720a | 2.009 ms · 456578B · 25621a | 1.543 ms · 443136B · 1460a | 18.85 ms · 14005346B · 164946a | 3.304 ms · 1367677B · 16944a | — | 4.746 ms · 2501437B · 88439a |
| fnv1a64 | 122 µs · 16400B · 2049a | 121.9 µs · 16400B · 2049a | 1.15 µs · 16400B · 2049a | 106× | — | 902.6 ns · 0B · 0a | 162.3 µs · 120808B · 3838a | — | 1.456 ms · 608254B · 18475a | 391.5 µs · 162921B · 7429a | — | 152.9 µs · 17282B · 2074a |
| host-call | 465.1 µs · 41752B · 3080a | 471.7 µs · 41752B · 3080a | 471.7 µs · 41752B · 3080a | 0.986× | — | 3.319 µs · 0B · 0a | 181 µs · 139264B · 3073a | 142 µs · 22536B · 1079a | 794 µs · 158049B · 14585a | 109.9 µs · 14920B · 1799a | — | 560.8 µs · 340640B · 9231a |
| i64-wide-fib | 27.8 ms · 2913440B · 364178a | 20.5 ms · 1942304B · 242785a | 681.8 µs · 16B · 1a | 40.8× | — | 239 µs · 0B · 0a | 23.01 µs · 90520B · 52a | — | 154.7 ms · 64193344B · 1942306a | 345.4 ms · 253473136B · 4248844a | — | 127.6 ms · 98686608B · 2138708a |
| indirect-recursive-fib | 1.188 ms · 0B · 0a | 1.188 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.6× | 98.52 µs · 8B · 1a | 35.02 µs · 0B · 0a | 2.061 ms · 319345B · 28655a | 1.95 ms · 703B · 2a | 2.537 ms · 4680B · 39a | 11.32 ms · 10158184B · 109494a | — | 23.84 ms · 13061323B · 394061a |
| iterative-fib | 1.135 µs · 0B · 0a | 1.071 µs · 0B · 0a | 82.64 ns · 0B · 0a | 13.7× | 85.13 ns · 8B · 1a | 12.47 ns · 0B · 0a | 24.46 µs · 90592B · 61a | 1.192 µs · 160B · 0a | 4.263 µs · 368B · 20a | 6.052 µs · 2448B · 88a | — | 6.259 µs · 944B · 81a |
| mandelbrot | 291.8 µs · 0B · 0a | 248.7 µs · 0B · 0a | 11.25 µs · 0B · 0a | 25.9× | — | 6.319 µs · 0B · 0a | 963.1 µs · 292345B · 25008a | 490.6 µs · 169408B · 661a | 8.106 ms · 5558658B · 104191a | 1.479 ms · 324979B · 23643a | — | 1.518 ms · 365402B · 22513a |
| map-ops | 70.83 µs · 8816B · 3a | 70.75 µs · 8816B · 3a | 10.79 µs · 8816B · 3a | 6.57× | — | 10.83 µs · 4904B · 3a | — | 281.1 µs · 114327B · 198a | 374.7 µs · 66728B · 4078a | — | — | 1.102 ms · 501522B · 15813a |
| mat-mul | 376.1 µs · 6216B · 6a | 374.8 µs · 6216B · 6a | 8.636 µs · 6216B · 6a | 43.5× | — | 5.562 µs · 6144B · 3a | 1.354 ms · 431714B · 34871a | 680.1 µs · 143168B · 478a | 1.214 ms · 61304B · 537a | 1.443 ms · 90712B · 9350a | — | 925.9 µs · 36613B · 6036a |
| n-body | 614.7 µs · 504B · 14a | 583.5 µs · 504B · 14a | 18.89 µs · 504B · 14a | 32.5× | — | 7.356 µs · 0B · 0a | 1.475 ms · 386897B · 35892a | 1.23 ms · 258914B · 2002a | 3.279 ms · 658436B · 81545a | 2.392 ms · 382714B · 34975a | — | 1.734 ms · 445468B · 20669a |
| n-queens | 429.9 µs · 120B · 6a | 429.6 µs · 120B · 6a | 31.6 µs · 120B · 6a | 13.6× | — | 6.015 µs · 0B · 0a | 948 µs · 234865B · 17784a | 574.7 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.423 ms · 410346B · 5147a | — | 1.998 ms · 766012B · 33371a |
| naive-checksum | 250.5 µs · 0B · 0a | 141.9 µs · 0B · 0a | 8.745 µs · 0B · 0a | 28.6× | — | 2.759 µs · 0B · 0a | 618.3 µs · 235753B · 18206a | 485.5 µs · 174768B · 682a | 869.5 µs · 155640B · 19402a | 785.5 µs · 163817B · 20409a | — | 310.9 µs · 8977B · 2071a |
| permutation-flips | 129.1 µs · 7680B · 128a | 128.3 µs · 7680B · 128a | 17.09 µs · 7680B · 128a | 7.55× | — | 1.87 ns · 0B · 0a | 550.8 µs · 292857B · 9856a | 235.1 µs · 78808B · 451a | 522.5 µs · 122504B · 765a | 494.4 µs · 111976B · 2368a | — | 997.9 ns · 528B · 13a |
| recursive-fib-20 | 711.6 µs · 0B · 0a | 718.5 µs · 0B · 0a | 47.16 µs · 0B · 0a | 15.1× | 42.75 µs · 8B · 1a | 21.5 µs · 0B · 0a | 1.942 ms · 319345B · 28655a | 2.26 ms · 704B · 2a | 2.942 ms · 4680B · 39a | 11.25 ms · 9807921B · 109494a | — | 10.82 ms · 8302654B · 192847a |
| recursive-fib-35 | 970.5 ms · 0B · 0a | 977.5 ms · 0B · 0a | 64.1 ms · 0B · 0a | 15.1× | 58.21 ms · 8B · 1a | 29.11 ms · 0B · 0a | 2.552 s · 312797584B · 39088170a | 2.974 s · 971008B · 3793a | 4.013 s · 375360B · 46373a | 15.26 s · 13378018192B · 149349962a | — | — |
| sieve | 20.42 µs · 1048B · 2a | 20.26 µs · 1048B · 2a | 927.9 ns · 1048B · 2a | 22× | 1.037 µs · 8B · 1a | 371.8 ns · 0B · 0a | 136.1 µs · 122504B · 1611a | 51.65 µs · 18416B · 44a | 84.07 µs · 1872B · 25a | 77.32 µs · 5704B · 30a | — | 80.56 µs · 4368B · 795a |
| sort-stress | 502.6 µs · 5136B · 512a | 508.1 µs · 5136B · 512a | 12.84 µs · 5136B · 512a | 39.2× | — | 5.343 µs · 1024B · 2a | 1.24 ms · 270945B · 19965a | 861.6 µs · 36567B · 365a | 1.804 ms · 49104B · 3621a | 1.783 ms · 23448B · 2034a | — | 920.3 µs · 17474B · 1708a |
| spectral-norm | 533.2 µs · 648B · 6a | 462.6 µs · 648B · 6a | 8.704 µs · 648B · 6a | 61.3× | — | 7.043 µs · 576B · 3a | 1.988 ms · 571371B · 54822a | 1.086 ms · 234784B · 925a | 2.415 ms · 275337B · 33729a | 4.594 ms · 2457135B · 52718a | — | 4.068 ms · 2448018B · 74233a |
| string-build | 715.3 µs · 85408B · 4107a | 712 µs · 85408B · 4107a | 493.8 µs · 85408B · 4107a | 1.45× | — | 368.2 µs · 855890B · 5001a | 2.441 ms · 2211274B · 36101a | 2.342 ms · 1078111B · 13859a | 3.111 ms · 1147384B · 26883a | 2.43 ms · 1326246B · 24469a | — | 3.085 ms · 2372776B · 41432a |
| string-compare | 1.086 ms · 21136B · 1284a | 1.081 ms · 21136B · 1284a | 222.9 µs · 21136B · 1284a | 4.87× | — | 63.77 µs · 9632B · 1585a | 1.641 ms · 296281B · 20528a | 1.141 ms · 59712B · 3431a | 2.33 ms · 50744B · 3848a | 1.813 ms · 209521B · 5520a | — | 2.4 ms · 503618B · 31846a |
| struct-tree-walk | 283.3 µs · 768B · 8a | 283.3 µs · 768B · 8a | 107.7 µs · 768B · 8a | 2.63× | — | 1.896 ns · 0B · 0a | 646.7 µs · 458379B · 5114a | 1.351 ms · 818512B · 11253a | 1.078 ms · 558960B · 6149a | 3.44 ms · 2570665B · 34797a | — | 1.17 µs · 600B · 17a |
| tail-ping-pong | 29.45 µs · 0B · 0a | 29.4 µs · 0B · 0a | 487.9 ns · 0B · 0a | 60.4× | — | 890.8 ns · 0B · 0a | 109.2 µs · 106112B · 2001a | 36.66 µs · 21856B · 85a | 113 µs · 13984B · 1726a | 116.1 µs · 14280B · 1731a | — | 531.5 µs · 348832B · 9016a |
| tail-sum | 29.24 µs · 0B · 0a | 29.42 µs · 0B · 0a | 678.1 ns · 0B · 0a | 43.1× | — | 302.7 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 37.49 µs · 21856B · 85a | 111.9 µs · 13984B · 1726a | 117.4 µs · 14280B · 1731a | — | 83.75 µs · 4608B · 1017a |
| typed-array-sum | 7.368 µs · 0B · 0a | 7.367 µs · 0B · 0a | 268 ns · 0B · 0a | 27.5× | 265.5 ns · 8B · 1a | 1.864 ns · 0B · 0a | 45.53 µs · 94208B · 513a | 8.569 µs · 4000B · 15a | 27.46 µs · 2080B · 238a | 17.17 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.5 µs · 18424B · 2046a | 110 µs · 18424B · 2046a | 1.368 µs · 18424B · 2046a | 80.8× | — | 356.7 ns · 0B · 0a | 105.9 µs · 107608B · 2188a | — | 980.9 µs · 447252B · 12736a | 427.8 µs · 199938B · 7772a | — | 101.7 µs · 13545B · 1317a |

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
