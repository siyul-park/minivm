# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.896 µs · 0B · 0a | 8.867 µs · 0B · 0a | 5.328 µs · 0B · 0a | 1.67× | — | 2.318 µs · 1024B · 128a | 38.04 µs · 96289B · 388a | 17.22 µs · 14376B · 256a | 75.84 µs · 78016B · 770a | 15.94 µs · 5712B · 266a | — | 31.36 µs · 2308B · 276a |
| binary-trees | 1.97 ms · 768B · 8a | 1.962 ms · 768B · 8a | 595.3 µs · 768B · 8a | 3.31× | — | 297.4 µs · 201936B · 8414a | 3.898 ms · 1468184B · 62877a | 10.65 ms · 6995676B · 92703a | 8.866 ms · 5198706B · 69052a | 27.92 ms · 19457609B · 280714a | — | 28.74 ms · 15128158B · 446106a |
| branch-tree | 1.326 µs · 0B · 0a | 19.72 ns · 0B · 0a | 85.4 ns · 0B · 0a | 15.5× | 627.2 ns · 16B · 1a | 160.4 ns · 0B · 0a | 46.62 µs · 95384B · 660a | 15.5 µs · 2464B · 9a | 26.43 µs · 1992B · 196a | 23.84 µs · 2168B · 203a | — | 20.52 µs · 1640B · 219a |
| closure-counter | 5.524 µs · 64B · 2a | 5.495 µs · 64B · 2a | 904.1 ns · 64B · 2a | 6.11× | — | 40.12 ns · 0B · 0a | 42.2 µs · 92272B · 261a | 12.88 µs · 151B · 3a | 19.2 µs · 1264B · 13a | 78.22 µs · 58312B · 659a | — | 90.78 µs · 35628B · 922a |
| coroutine-gen | 78.96 µs · 128B · 2a | 78.7 µs · 128B · 2a | 78.96 µs · 128B · 2a | 1× | — | 41.99 µs · 208B · 7a | — | 335.7 µs · 149936B · 649a | 597.4 µs · 330649B · 6719a | 204.7 µs · 91800B · 3646a | — | — |
| fannkuch | 829.7 µs · 34608B · 1442a | 814.2 µs · 34608B · 1442a | 214 µs · 34608B · 1442a | 3.88× | — | 35.67 µs · 17280B · 720a | 2.038 ms · 456578B · 25621a | 1.557 ms · 443136B · 1460a | 19.38 ms · 14005341B · 164946a | 3.335 ms · 1367678B · 16944a | — | 4.803 ms · 2501419B · 88439a |
| fnv1a64 | 121.2 µs · 16400B · 2049a | 121.9 µs · 16400B · 2049a | 1.167 µs · 16400B · 2049a | 104× | — | 902.7 ns · 0B · 0a | 164.9 µs · 120808B · 3838a | — | 1.469 ms · 608253B · 18475a | 400.4 µs · 162922B · 7429a | — | 155.2 µs · 17282B · 2074a |
| host-call | 462 µs · 41752B · 3080a | 472.6 µs · 41752B · 3080a | 471.7 µs · 41752B · 3080a | 0.979× | — | 3.312 µs · 0B · 0a | 181.9 µs · 139264B · 3073a | 130.9 µs · 22536B · 1079a | 795.4 µs · 158049B · 14585a | 105.8 µs · 14920B · 1799a | — | 599.9 µs · 340638B · 9231a |
| i64-wide-fib | 27.96 ms · 2913456B · 364178a | 20.69 ms · 1942304B · 242785a | 681 µs · 16B · 1a | 41.1× | — | 238.6 µs · 0B · 0a | 24.02 µs · 90520B · 52a | — | 157.9 ms · 64193328B · 1942305a | 356.9 ms · 253472800B · 4248840a | — | 130.7 ms · 98686592B · 2138708a |
| indirect-recursive-fib | 1.187 ms · 0B · 0a | 1.187 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.6× | 99.03 µs · 8B · 1a | 35.26 µs · 0B · 0a | 2.048 ms · 319345B · 28655a | 1.969 ms · 704B · 2a | 2.552 ms · 4680B · 39a | 11.9 ms · 10158182B · 109494a | — | 24.17 ms · 13061307B · 394061a |
| iterative-fib | 1.134 µs · 0B · 0a | 1.066 µs · 0B · 0a | 80.53 ns · 0B · 0a | 14.1× | 85.44 ns · 8B · 1a | 11.91 ns · 0B · 0a | 24.13 µs · 90592B · 61a | 1.154 µs · 160B · 0a | 4.231 µs · 368B · 20a | 6.146 µs · 2448B · 88a | — | 6.17 µs · 944B · 81a |
| mandelbrot | 291 µs · 0B · 0a | 249.3 µs · 0B · 0a | 11.22 µs · 0B · 0a | 25.9× | — | 6.325 µs · 0B · 0a | 953.5 µs · 292345B · 25008a | 490.2 µs · 169408B · 661a | 8.288 ms · 5558650B · 104191a | 1.473 ms · 324978B · 23643a | — | 1.517 ms · 365402B · 22513a |
| map-ops | 70.45 µs · 8816B · 3a | 70.55 µs · 8816B · 3a | 11.19 µs · 8816B · 3a | 6.3× | — | 10.91 µs · 4904B · 3a | — | 281.4 µs · 114327B · 198a | 379.4 µs · 66728B · 4078a | — | — | 1.146 ms · 501524B · 15813a |
| mat-mul | 375.9 µs · 6216B · 6a | 374.3 µs · 6216B · 6a | 8.744 µs · 6216B · 6a | 43× | — | 5.563 µs · 6144B · 3a | 1.389 ms · 431714B · 34871a | 675.7 µs · 143168B · 478a | 1.219 ms · 61304B · 537a | 1.442 ms · 90712B · 9350a | — | 927.8 µs · 36614B · 6036a |
| n-body | 613.5 µs · 504B · 14a | 582.2 µs · 504B · 14a | 19.03 µs · 504B · 14a | 32.2× | — | 7.343 µs · 0B · 0a | 1.512 ms · 386898B · 35892a | 1.223 ms · 258913B · 2002a | 3.321 ms · 658435B · 81545a | 2.404 ms · 382715B · 34975a | — | 1.747 ms · 445459B · 20669a |
| n-queens | 429.9 µs · 120B · 6a | 430.3 µs · 120B · 6a | 31.6 µs · 120B · 6a | 13.6× | — | 6.013 µs · 0B · 0a | 947.9 µs · 234865B · 17784a | 584.9 µs · 14423B · 63a | 1.1 ms · 7424B · 35a | 1.428 ms · 410345B · 5147a | — | 2.007 ms · 766005B · 33371a |
| naive-checksum | 252.9 µs · 0B · 0a | 141.8 µs · 0B · 0a | 8.737 µs · 0B · 0a | 28.9× | — | 2.758 µs · 0B · 0a | 626 µs · 235753B · 18206a | 481.3 µs · 174767B · 682a | 866.4 µs · 155640B · 19402a | 776.3 µs · 163816B · 20409a | — | 313 µs · 8976B · 2071a |
| permutation-flips | 128.9 µs · 7680B · 128a | 128.5 µs · 7680B · 128a | 17.17 µs · 7680B · 128a | 7.51× | — | 1.869 ns · 0B · 0a | 562.6 µs · 292857B · 9856a | 245 µs · 78808B · 451a | 531 µs · 122504B · 765a | 494.1 µs · 111976B · 2368a | — | 1.016 µs · 528B · 13a |
| recursive-fib-20 | 710.3 µs · 0B · 0a | 717.3 µs · 0B · 0a | 47.27 µs · 0B · 0a | 15× | 42.76 µs · 8B · 1a | 21.46 µs · 0B · 0a | 1.943 ms · 319345B · 28655a | 2.173 ms · 704B · 2a | 2.961 ms · 4680B · 39a | 11.68 ms · 9807912B · 109494a | — | 10.72 ms · 8302710B · 192848a |
| recursive-fib-35 | 971.4 ms · 0B · 0a | 974.1 ms · 0B · 0a | 63.99 ms · 0B · 0a | 15.2× | 58.2 ms · 8B · 1a | 29.11 ms · 0B · 0a | 2.564 s · 312797248B · 39088168a | 3.128 s · 971008B · 3793a | 4.033 s · 375360B · 46373a | 15.9 s · 13378017664B · 149349964a | — | — |
| sieve | 20.33 µs · 1048B · 2a | 20.3 µs · 1048B · 2a | 941.4 ns · 1048B · 2a | 21.6× | 1.028 µs · 8B · 1a | 350.6 ns · 0B · 0a | 139.2 µs · 122504B · 1611a | 52.79 µs · 18415B · 44a | 84.25 µs · 1872B · 25a | 76.42 µs · 5704B · 30a | — | 81.19 µs · 4368B · 795a |
| sort-stress | 505.3 µs · 5136B · 512a | 509.4 µs · 5136B · 512a | 12.81 µs · 5136B · 512a | 39.5× | — | 5.796 µs · 1024B · 2a | 1.27 ms · 270945B · 19965a | 856.8 µs · 36567B · 365a | 1.796 ms · 49104B · 3621a | 1.783 ms · 23448B · 2034a | — | 927 µs · 17473B · 1708a |
| spectral-norm | 534.2 µs · 648B · 6a | 460.2 µs · 648B · 6a | 8.439 µs · 648B · 6a | 63.3× | — | 7.041 µs · 576B · 3a | 2.067 ms · 571371B · 54822a | 1.095 ms · 234784B · 925a | 2.482 ms · 275337B · 33729a | 4.711 ms · 2457140B · 52718a | — | 4.201 ms · 2447997B · 74233a |
| string-build | 725.2 µs · 85408B · 4107a | 718.3 µs · 85408B · 4107a | 598.6 µs · 85408B · 4107a | 1.21× | — | 368.7 µs · 855889B · 5001a | 2.433 ms · 2211270B · 36101a | 2.311 ms · 1078110B · 13859a | 3.132 ms · 1147384B · 26883a | 2.479 ms · 1326244B · 24469a | — | 3.148 ms · 2372779B · 41432a |
| string-compare | 1.085 ms · 21136B · 1284a | 1.084 ms · 21136B · 1284a | 219.6 µs · 21136B · 1284a | 4.94× | — | 63.74 µs · 9632B · 1585a | 1.682 ms · 296281B · 20528a | 1.147 ms · 59712B · 3431a | 2.32 ms · 50744B · 3848a | 1.815 ms · 209521B · 5520a | — | 2.38 ms · 503614B · 31846a |
| struct-tree-walk | 282.9 µs · 768B · 8a | 282.1 µs · 768B · 8a | 107.3 µs · 768B · 8a | 2.64× | — | 1.895 ns · 0B · 0a | 655.3 µs · 458379B · 5114a | 1.381 ms · 818513B · 11253a | 1.083 ms · 558960B · 6149a | 3.536 ms · 2570664B · 34797a | — | 1.242 µs · 600B · 17a |
| tail-ping-pong | 29.3 µs · 0B · 0a | 29.39 µs · 0B · 0a | 481.9 ns · 0B · 0a | 60.8× | — | 891.2 ns · 0B · 0a | 109.4 µs · 106112B · 2001a | 36.06 µs · 21856B · 85a | 112.6 µs · 13984B · 1726a | 116.6 µs · 14280B · 1731a | — | 559.8 µs · 348834B · 9016a |
| tail-sum | 29.31 µs · 0B · 0a | 29.39 µs · 0B · 0a | 672.5 ns · 0B · 0a | 43.6× | — | 301.1 ns · 0B · 0a | 108.4 µs · 106112B · 2001a | 36.19 µs · 21856B · 85a | 113.2 µs · 13984B · 1726a | 117.3 µs · 14280B · 1731a | — | 84.73 µs · 4608B · 1017a |
| typed-array-sum | 7.361 µs · 0B · 0a | 7.358 µs · 0B · 0a | 260.8 ns · 0B · 0a | 28.2× | 266.8 ns · 8B · 1a | 1.864 ns · 0B · 0a | 46.1 µs · 94208B · 513a | 8.293 µs · 4000B · 15a | 27.26 µs · 2080B · 238a | 17.26 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.9 µs · 18424B · 2046a | 112.2 µs · 18424B · 2046a | 1.342 µs · 18424B · 2046a | 82.6× | — | 360.9 ns · 0B · 0a | 103.3 µs · 107608B · 2188a | — | 985 µs · 447252B · 12735a | 436.5 µs · 199938B · 7772a | — | 101.4 µs · 13545B · 1317a |

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
