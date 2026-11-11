# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.894 µs · 0B · 0a | 5.401 µs · 0B · 0a | 1.65× | — | 2.254 µs · 1024B · 128a | 39.46 µs · 96289B · 388a | 17.76 µs · 14376B · 256a | 74.76 µs · 78016B · 770a | 14.45 µs · 5712B · 266a | — | 31.05 µs · 2308B · 276a |
| binary-trees | 1.949 ms · 768B · 8a | 590.2 µs · 768B · 8a | 3.3× | — | 290.9 µs · 201936B · 8414a | 3.843 ms · 1468187B · 62877a | 10.76 ms · 6995684B · 92703a | 8.992 ms · 5198698B · 69052a | 27.78 ms · 19457601B · 280714a | — | 28.83 ms · 15128160B · 446106a |
| branch-tree | 1.346 µs · 0B · 0a | 143.1 ns · 0B · 0a | 9.41× | 625.6 ns · 16B · 1a | 160.7 ns · 0B · 0a | 58.2 µs · 95384B · 660a | 15.84 µs · 2464B · 9a | 26.6 µs · 1992B · 196a | 24.03 µs · 2168B · 203a | — | 20.58 µs · 1640B · 219a |
| closure-counter | 5.584 µs · 64B · 2a | 908.4 ns · 64B · 2a | 6.15× | — | 40.39 ns · 0B · 0a | 41.6 µs · 92272B · 261a | 13.03 µs · 151B · 3a | 19.14 µs · 1264B · 13a | 81.17 µs · 58312B · 659a | — | 91.11 µs · 35629B · 922a |
| coroutine-gen | 76.98 µs · 128B · 2a | 77.49 µs · 128B · 2a | 0.994× | — | 41.7 µs · 208B · 7a | — | 332.4 µs · 149936B · 649a | 612.2 µs · 330650B · 6719a | 203.2 µs · 91800B · 3646a | — | — |
| fannkuch | 831.8 µs · 34608B · 1442a | 227.7 µs · 34608B · 1442a | 3.65× | — | 35.77 µs · 17280B · 720a | 2.006 ms · 456578B · 25621a | 1.55 ms · 443138B · 1460a | 19.41 ms · 14005352B · 164946a | 3.424 ms · 1367683B · 16944a | — | 4.893 ms · 2501434B · 88439a |
| fnv1a64 | 124 µs · 16400B · 2049a | 1.159 µs · 16400B · 2049a | 107× | — | 902.4 ns · 0B · 0a | 164.3 µs · 120808B · 3838a | — | 1.518 ms · 608254B · 18475a | 387 µs · 162921B · 7429a | — | 155.2 µs · 17282B · 2074a |
| host-call | 723.7 µs · 196592B · 14073a | 730.7 µs · 196592B · 14073a | 0.99× | — | 3.312 µs · 0B · 0a | 181.7 µs · 139265B · 3073a | 130.2 µs · 22536B · 1079a | 809 µs · 158048B · 14585a | 104.8 µs · 14920B · 1799a | — | 578.4 µs · 340639B · 9231a |
| i64-wide-fib | 28.09 ms · 2913440B · 364178a | 679.4 µs · 16B · 1a | 41.4× | — | 243.7 µs · 0B · 0a | 24.07 µs · 90520B · 52a | — | 159.1 ms · 64193352B · 1942306a | 354.4 ms · 253472768B · 4248841a | — | 129.6 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 1.184 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.6× | 98.9 µs · 8B · 1a | 35.03 µs · 0B · 0a | 2.106 ms · 319345B · 28655a | 1.949 ms · 703B · 2a | 2.547 ms · 4680B · 39a | 11.58 ms · 10158200B · 109494a | — | 24.56 ms · 13061355B · 394062a |
| iterative-fib | 1.118 µs · 0B · 0a | 78.19 ns · 0B · 0a | 14.3× | 85.07 ns · 8B · 1a | 12.46 ns · 0B · 0a | 25.24 µs · 90592B · 61a | 1.18 µs · 160B · 0a | 4.251 µs · 368B · 20a | 6.193 µs · 2448B · 88a | — | 6.171 µs · 944B · 81a |
| mandelbrot | 292.3 µs · 0B · 0a | 10.93 µs · 0B · 0a | 26.7× | — | 6.322 µs · 0B · 0a | 973 µs · 292345B · 25008a | 484.8 µs · 169408B · 661a | 8.204 ms · 5558650B · 104191a | 1.526 ms · 324979B · 23643a | — | 1.527 ms · 365404B · 22513a |
| map-ops | 69.79 µs · 8816B · 3a | 69.75 µs · 8816B · 3a | 1× | — | 10.87 µs · 4904B · 3a | — | 280.4 µs · 114328B · 198a | 384.5 µs · 66728B · 4078a | — | — | 1.113 ms · 501522B · 15813a |
| mat-mul | 370.3 µs · 6216B · 6a | 8.691 µs · 6216B · 6a | 42.6× | — | 5.476 µs · 6144B · 3a | 1.365 ms · 431715B · 34871a | 677.3 µs · 143168B · 478a | 1.199 ms · 61304B · 537a | 1.448 ms · 90713B · 9350a | — | 920.7 µs · 36615B · 6036a |
| n-body | 608.6 µs · 504B · 14a | 18.75 µs · 504B · 14a | 32.5× | — | 7.355 µs · 0B · 0a | 1.495 ms · 386899B · 35892a | 1.22 ms · 258913B · 2002a | 3.313 ms · 658435B · 81545a | 2.439 ms · 382714B · 34975a | — | 1.749 ms · 445465B · 20669a |
| n-queens | 427.7 µs · 120B · 6a | 31.6 µs · 120B · 6a | 13.5× | — | 6.025 µs · 0B · 0a | 955.1 µs · 234865B · 17784a | 570 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.439 ms · 410345B · 5147a | — | 2.002 ms · 765997B · 33371a |
| permutation-flips | 128 µs · 7680B · 128a | 17.54 µs · 7680B · 128a | 7.3× | — | 1.875 ns · 0B · 0a | 555.6 µs · 292858B · 9856a | 238.5 µs · 78808B · 451a | 526.1 µs · 122504B · 765a | 498 µs · 111976B · 2368a | — | 1.006 µs · 528B · 13a |
| recursive-fib-20 | 716 µs · 0B · 0a | 47.28 µs · 0B · 0a | 15.1× | 42.74 µs · 8B · 1a | 21.43 µs · 0B · 0a | 1.946 ms · 319345B · 28655a | 2.21 ms · 704B · 2a | 2.951 ms · 4680B · 39a | 11.47 ms · 9807917B · 109494a | — | 11.18 ms · 8302736B · 192848a |
| recursive-fib-35 | 969.7 ms · 0B · 0a | 63.89 ms · 0B · 0a | 15.2× | 58.22 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.575 s · 312797184B · 39088168a | 3.05 s · 971008B · 3793a | 4.022 s · 375360B · 46373a | 15.76 s · 13378019808B · 149349998a | — | — |
| sieve | 20.08 µs · 1048B · 2a | 945.6 ns · 1048B · 2a | 21.2× | 1.037 µs · 8B · 1a | 371.8 ns · 0B · 0a | 138.6 µs · 122504B · 1611a | 51.04 µs · 18416B · 44a | 84.01 µs · 1872B · 25a | 77.53 µs · 5704B · 30a | — | 81.14 µs · 4368B · 795a |
| sort-stress | 504.4 µs · 5136B · 512a | 12.64 µs · 5136B · 512a | 39.9× | — | 5.332 µs · 1024B · 2a | 1.277 ms · 270945B · 19965a | 844 µs · 36567B · 365a | 1.783 ms · 49104B · 3621a | 1.777 ms · 23448B · 2034a | — | 922.3 µs · 17474B · 1708a |
| spectral-norm | 529.3 µs · 648B · 6a | 8.323 µs · 648B · 6a | 63.6× | — | 7.033 µs · 576B · 3a | 2.074 ms · 571371B · 54822a | 1.085 ms · 234784B · 925a | 2.446 ms · 275337B · 33729a | 4.711 ms · 2457132B · 52718a | — | 4.187 ms · 2448014B · 74233a |
| string-build | 724.5 µs · 85408B · 4107a | 494.4 µs · 85408B · 4107a | 1.47× | — | 369.8 µs · 855890B · 5001a | 2.483 ms · 2211275B · 36101a | 2.321 ms · 1078112B · 13859a | 3.172 ms · 1147384B · 26883a | 2.517 ms · 1326245B · 24469a | — | 3.132 ms · 2372778B · 41432a |
| string-compare | 1.082 ms · 21136B · 1284a | 234.1 µs · 21136B · 1284a | 4.62× | — | 63.15 µs · 9632B · 1585a | 1.665 ms · 296281B · 20528a | 1.133 ms · 59712B · 3431a | 2.319 ms · 50744B · 3848a | 1.797 ms · 209521B · 5520a | — | 2.407 ms · 503611B · 31846a |
| struct-tree-walk | 279.8 µs · 768B · 8a | 107.5 µs · 768B · 8a | 2.6× | — | 1.874 ns · 0B · 0a | 666.8 µs · 458379B · 5114a | 1.38 ms · 818512B · 11253a | 1.112 ms · 558960B · 6149a | 3.601 ms · 2570668B · 34797a | — | 1.179 µs · 600B · 17a |
| tail-ping-pong | 28.84 µs · 0B · 0a | 479.8 ns · 0B · 0a | 60.1× | — | 891 ns · 0B · 0a | 109.3 µs · 106112B · 2001a | 35.82 µs · 21855B · 85a | 111.2 µs · 13984B · 1726a | 116.5 µs · 14280B · 1731a | — | 542.3 µs · 348830B · 9016a |
| tail-sum | 28.78 µs · 0B · 0a | 674.1 ns · 0B · 0a | 42.7× | — | 302.7 ns · 0B · 0a | 109.8 µs · 106112B · 2001a | 35.8 µs · 21855B · 85a | 111.3 µs · 13984B · 1726a | 116.7 µs · 14280B · 1731a | — | 84.37 µs · 4608B · 1017a |
| typed-array-sum | 7.248 µs · 0B · 0a | 258.3 ns · 0B · 0a | 28.1× | 263.9 ns · 8B · 1a | 1.875 ns · 0B · 0a | 46.2 µs · 94208B · 513a | 8.45 µs · 4000B · 15a | 27.16 µs · 2080B · 238a | 17.16 µs · 2496B · 246a | — | — |
| xorshift-i64 | 109.6 µs · 18424B · 2046a | 1.41 µs · 18424B · 2046a | 77.7× | — | 356.4 ns · 0B · 0a | 104.7 µs · 107608B · 2188a | — | 1 ms · 447253B · 12735a | 434.4 µs · 199937B · 7772a | — | 101.9 µs · 13545B · 1317a |

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
