# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.926 µs · 0B · 0a | 8.889 µs · 0B · 0a | 5.416 µs · 0B · 0a | 1.65× | — | 2.208 µs · 1024B · 128a | 41.28 µs · 96288B · 388a | 17.71 µs · 14376B · 256a | 76.64 µs · 78016B · 770a | 16.19 µs · 5712B · 266a | — | 31.16 µs · 2308B · 276a |
| binary-trees | 2.022 ms · 768B · 8a | 1.96 ms · 768B · 8a | 596.6 µs · 768B · 8a | 3.39× | — | 286.1 µs · 201936B · 8414a | 3.756 ms · 1468186B · 62877a | 10.66 ms · 6995673B · 92703a | 8.888 ms · 5198694B · 69052a | 27.9 ms · 19457600B · 280714a | — | 27.98 ms · 15128177B · 446106a |
| branch-tree | 1.333 µs · 0B · 0a | 19.76 ns · 0B · 0a | 87.92 ns · 0B · 0a | 15.2× | 627.1 ns · 16B · 1a | 160.6 ns · 0B · 0a | 46.61 µs · 95385B · 660a | 15.26 µs · 2463B · 9a | 26.41 µs · 1992B · 196a | 24.05 µs · 2168B · 203a | — | 20.57 µs · 1640B · 219a |
| closure-counter | 5.558 µs · 64B · 2a | 5.532 µs · 64B · 2a | 919.6 ns · 64B · 2a | 6.04× | — | 40.11 ns · 0B · 0a | 44.37 µs · 92272B · 261a | 12.9 µs · 152B · 3a | 19.24 µs · 1264B · 13a | 81.09 µs · 58312B · 659a | — | 88.71 µs · 35628B · 922a |
| coroutine-gen | 78.56 µs · 128B · 2a | 78.86 µs · 128B · 2a | 78.42 µs · 128B · 2a | 1× | — | 42.84 µs · 208B · 7a | — | 340.3 µs · 149936B · 649a | 612.1 µs · 330650B · 6719a | 204.7 µs · 91800B · 3646a | — | — |
| fannkuch | 829 µs · 34608B · 1442a | 821.1 µs · 34608B · 1442a | 214.9 µs · 34608B · 1442a | 3.86× | — | 36.36 µs · 17280B · 720a | 1.97 ms · 456579B · 25621a | 1.588 ms · 443136B · 1460a | 19.77 ms · 14005339B · 164946a | 3.375 ms · 1367679B · 16944a | — | 4.724 ms · 2501444B · 88439a |
| fnv1a64 | 121.4 µs · 16400B · 2049a | 121.3 µs · 16400B · 2049a | 1.162 µs · 16400B · 2049a | 105× | — | 903.3 ns · 0B · 0a | 167.9 µs · 120809B · 3838a | — | 1.47 ms · 608252B · 18475a | 387.8 µs · 162921B · 7429a | — | 156 µs · 17282B · 2074a |
| host-call | 483.8 µs · 41752B · 3080a | 487.7 µs · 41752B · 3080a | 472.9 µs · 41752B · 3080a | 1.02× | — | 3.312 µs · 0B · 0a | 187.5 µs · 139264B · 3073a | 129.9 µs · 22536B · 1079a | 799.7 µs · 158048B · 14585a | 106.5 µs · 14920B · 1799a | — | 550.7 µs · 340644B · 9231a |
| i64-wide-fib | 28.22 ms · 2913456B · 364178a | 20.87 ms · 1942288B · 242785a | 679.9 µs · 16B · 1a | 41.5× | — | 238.9 µs · 0B · 0a | 24.6 µs · 90520B · 52a | — | 157 ms · 64193344B · 1942306a | 350.5 ms · 253472752B · 4248840a | — | 124.7 ms · 98686613B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 1.19 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.6× | 100.1 µs · 8B · 1a | 35.06 µs · 0B · 0a | 2.058 ms · 319346B · 28655a | 1.95 ms · 703B · 2a | 2.548 ms · 4680B · 39a | 11.67 ms · 10158182B · 109494a | — | 23.98 ms · 13061300B · 394061a |
| iterative-fib | 1.139 µs · 0B · 0a | 1.068 µs · 0B · 0a | 79.01 ns · 0B · 0a | 14.4× | 86.13 ns · 8B · 1a | 11.91 ns · 0B · 0a | 25.68 µs · 90592B · 61a | 1.17 µs · 159B · 0a | 4.262 µs · 368B · 20a | 6.117 µs · 2448B · 88a | — | 6.158 µs · 944B · 81a |
| mandelbrot | 292.3 µs · 0B · 0a | 248.9 µs · 0B · 0a | 11.26 µs · 0B · 0a | 26× | — | 6.319 µs · 0B · 0a | 943.2 µs · 292345B · 25008a | 490.3 µs · 169408B · 661a | 8.092 ms · 5558657B · 104191a | 1.452 ms · 324978B · 23643a | — | 1.518 ms · 365402B · 22513a |
| map-ops | 70.28 µs · 8816B · 3a | 70.28 µs · 8816B · 3a | 10.86 µs · 8816B · 3a | 6.47× | — | 10.92 µs · 4904B · 3a | — | 279.8 µs · 114328B · 198a | 383 µs · 66728B · 4078a | — | — | 1.097 ms · 501522B · 15813a |
| mat-mul | 375.7 µs · 6216B · 6a | 375.1 µs · 6216B · 6a | 8.703 µs · 6216B · 6a | 43.2× | — | 5.544 µs · 6144B · 3a | 1.336 ms · 431714B · 34871a | 674.9 µs · 143168B · 478a | 1.22 ms · 61304B · 537a | 1.439 ms · 90712B · 9350a | — | 927.8 µs · 36615B · 6036a |
| n-body | 616.8 µs · 504B · 14a | 583.4 µs · 504B · 14a | 18.96 µs · 504B · 14a | 32.5× | — | 7.344 µs · 0B · 0a | 1.478 ms · 386897B · 35892a | 1.215 ms · 258913B · 2002a | 3.249 ms · 658437B · 81545a | 2.377 ms · 382714B · 34975a | — | 1.736 ms · 445473B · 20669a |
| n-queens | 430.1 µs · 120B · 6a | 430.5 µs · 120B · 6a | 31.89 µs · 120B · 6a | 13.5× | — | 6.006 µs · 0B · 0a | 929.6 µs · 234865B · 17784a | 576 µs · 14423B · 63a | 1.101 ms · 7424B · 35a | 1.393 ms · 410346B · 5147a | — | 2.001 ms · 766009B · 33371a |
| naive-checksum | 253 µs · 0B · 0a | 141.7 µs · 0B · 0a | 8.781 µs · 0B · 0a | 28.8× | — | 2.759 µs · 0B · 0a | 626.4 µs · 235753B · 18206a | 481.8 µs · 174768B · 682a | 862 µs · 155640B · 19402a | 775.7 µs · 163817B · 20409a | — | 313.2 µs · 8977B · 2071a |
| permutation-flips | 128.6 µs · 7680B · 128a | 128.8 µs · 7680B · 128a | 16.71 µs · 7680B · 128a | 7.69× | — | 1.869 ns · 0B · 0a | 578 µs · 292858B · 9856a | 245.3 µs · 78808B · 451a | 531.1 µs · 122504B · 765a | 494.3 µs · 111976B · 2368a | — | 985.8 ns · 528B · 13a |
| recursive-fib-20 | 712.3 µs · 0B · 0a | 718 µs · 0B · 0a | 47.4 µs · 0B · 0a | 15× | 42.75 µs · 8B · 1a | 21.42 µs · 0B · 0a | 1.929 ms · 319345B · 28655a | 2.177 ms · 704B · 2a | 2.964 ms · 4680B · 39a | 11.75 ms · 9807916B · 109494a | — | 10.42 ms · 8302694B · 192848a |
| recursive-fib-35 | 973.2 ms · 0B · 0a | 976 ms · 0B · 0a | 63.9 ms · 0B · 0a | 15.2× | 58.88 ms · 8B · 1a | 29.11 ms · 0B · 0a | 2.559 s · 312797264B · 39088167a | 2.963 s · 971008B · 3793a | 4.047 s · 375360B · 46373a | 15.74 s · 13378018000B · 149349978a | — | — |
| sieve | 20.41 µs · 1048B · 2a | 20.61 µs · 1048B · 2a | 957.8 ns · 1048B · 2a | 21.3× | 1.021 µs · 8B · 1a | 350.7 ns · 0B · 0a | 139.2 µs · 122504B · 1611a | 52.41 µs · 18416B · 44a | 84.34 µs · 1872B · 25a | 76.49 µs · 5704B · 30a | — | 81.56 µs · 4368B · 795a |
| sort-stress | 505.4 µs · 5136B · 512a | 509.6 µs · 5136B · 512a | 12.9 µs · 5136B · 512a | 39.2× | — | 5.809 µs · 1024B · 2a | 1.266 ms · 270945B · 19965a | 859.3 µs · 36567B · 365a | 1.795 ms · 49104B · 3621a | 1.784 ms · 23448B · 2034a | — | 926.6 µs · 17475B · 1708a |
| spectral-norm | 533.4 µs · 648B · 6a | 459.7 µs · 648B · 6a | 8.463 µs · 648B · 6a | 63× | — | 7.037 µs · 576B · 3a | 2.011 ms · 571370B · 54822a | 1.089 ms · 234784B · 925a | 2.501 ms · 275338B · 33729a | 5.002 ms · 2457142B · 52718a | — | 4.033 ms · 2448003B · 74233a |
| string-build | 721.8 µs · 85408B · 4107a | 714.2 µs · 85408B · 4107a | 588.4 µs · 85408B · 4107a | 1.23× | — | 377.5 µs · 855891B · 5001a | 2.43 ms · 2211276B · 36101a | 2.33 ms · 1078110B · 13859a | 3.165 ms · 1147383B · 26883a | 2.477 ms · 1326243B · 24469a | — | 3.084 ms · 2372780B · 41432a |
| string-compare | 1.084 ms · 21136B · 1284a | 1.087 ms · 21136B · 1284a | 221.9 µs · 21136B · 1284a | 4.89× | — | 62.57 µs · 9632B · 1585a | 1.654 ms · 296281B · 20528a | 1.131 ms · 59712B · 3431a | 2.323 ms · 50744B · 3848a | 1.795 ms · 209521B · 5520a | — | 2.368 ms · 503616B · 31846a |
| struct-tree-walk | 282.9 µs · 768B · 8a | 283.8 µs · 768B · 8a | 107.9 µs · 768B · 8a | 2.62× | — | 1.894 ns · 0B · 0a | 657.4 µs · 458379B · 5114a | 1.361 ms · 818511B · 11253a | 1.073 ms · 558960B · 6149a | 3.54 ms · 2570662B · 34797a | — | 1.17 µs · 600B · 17a |
| tail-ping-pong | 29.39 µs · 0B · 0a | 29.46 µs · 0B · 0a | 477 ns · 0B · 0a | 61.6× | — | 891.5 ns · 0B · 0a | 109.2 µs · 106112B · 2001a | 35.83 µs · 21856B · 85a | 111.1 µs · 13984B · 1726a | 115.4 µs · 14280B · 1731a | — | 543.2 µs · 348834B · 9016a |
| tail-sum | 29.35 µs · 0B · 0a | 29.38 µs · 0B · 0a | 666.7 ns · 0B · 0a | 44× | — | 301.5 ns · 0B · 0a | 110.2 µs · 106112B · 2001a | 35.64 µs · 21856B · 85a | 110.9 µs · 13984B · 1726a | 115.6 µs · 14280B · 1731a | — | 84.92 µs · 4608B · 1017a |
| typed-array-sum | 7.369 µs · 0B · 0a | 7.357 µs · 0B · 0a | 262.1 ns · 0B · 0a | 28.1× | 267.2 ns · 8B · 1a | 1.866 ns · 0B · 0a | 46.09 µs · 94208B · 513a | 8.361 µs · 3999B · 15a | 27.25 µs · 2080B · 238a | 17.12 µs · 2496B · 246a | — | — |
| xorshift-i64 | 109.1 µs · 18424B · 2046a | 109.1 µs · 18424B · 2046a | 1.382 µs · 18424B · 2046a | 78.9× | — | 361.8 ns · 0B · 0a | 102.5 µs · 107608B · 2188a | — | 953.7 µs · 447256B · 12735a | 416.5 µs · 199938B · 7772a | — | 101.3 µs · 13545B · 1317a |

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
