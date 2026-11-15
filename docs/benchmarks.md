# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.919 µs · 0B · 0a | 8.888 µs · 0B · 0a | 5.388 µs · 0B · 0a | 1.66× | — | 2.277 µs · 1024B · 128a | 37.23 µs · 96289B · 388a | 17.2 µs · 14376B · 256a | 71.57 µs · 78016B · 770a | 14.61 µs · 5712B · 266a | — | 31.33 µs · 2308B · 276a |
| binary-trees | 1.988 ms · 768B · 8a | 1.987 ms · 768B · 8a | 597.4 µs · 768B · 8a | 3.33× | — | 297.8 µs · 201936B · 8414a | 4.174 ms · 1468190B · 62877a | 10.75 ms · 6995671B · 92703a | 8.994 ms · 5198700B · 69052a | 27.92 ms · 19457610B · 280714a | — | 30.18 ms · 15128181B · 446106a |
| branch-tree | 1.333 µs · 0B · 0a | 18.63 ns · 0B · 0a | 83.44 ns · 0B · 0a | 16× | 628.3 ns · 16B · 1a | 160.4 ns · 0B · 0a | 54.42 µs · 95384B · 660a | 15.82 µs · 2463B · 9a | 27.1 µs · 1992B · 196a | 24.46 µs · 2168B · 203a | — | 20.68 µs · 1640B · 219a |
| closure-counter | 5.555 µs · 64B · 2a | 5.531 µs · 64B · 2a | 921.8 ns · 64B · 2a | 6.03× | — | 40.8 ns · 0B · 0a | 41.76 µs · 92272B · 261a | 12.87 µs · 152B · 3a | 19.26 µs · 1264B · 13a | 78.89 µs · 58312B · 659a | — | 94.83 µs · 35628B · 922a |
| coroutine-gen | 79.38 µs · 128B · 2a | 79.29 µs · 128B · 2a | 79.09 µs · 128B · 2a | 1× | — | 41.75 µs · 208B · 7a | — | 328.6 µs · 149936B · 649a | 602.1 µs · 330649B · 6719a | 204 µs · 91800B · 3646a | — | — |
| fannkuch | 833.5 µs · 34608B · 1442a | 836.8 µs · 34608B · 1442a | 223.7 µs · 34608B · 1442a | 3.73× | — | 36.99 µs · 17280B · 720a | 2.07 ms · 456579B · 25621a | 1.548 ms · 443136B · 1460a | 19.72 ms · 14005343B · 164946a | 3.34 ms · 1367676B · 16944a | — | 4.958 ms · 2501434B · 88439a |
| fnv1a64 | 125.4 µs · 16400B · 2049a | 125.4 µs · 16400B · 2049a | 1.155 µs · 16400B · 2049a | 109× | — | 902.4 ns · 0B · 0a | 164.5 µs · 120808B · 3838a | — | 1.497 ms · 608257B · 18475a | 401 µs · 162921B · 7429a | — | 155.3 µs · 17282B · 2074a |
| host-call | 476.4 µs · 41752B · 3080a | 495.2 µs · 41752B · 3080a | 492.2 µs · 41752B · 3080a | 0.968× | — | 3.312 µs · 0B · 0a | 182.3 µs · 139264B · 3073a | 137.3 µs · 22536B · 1079a | 818.8 µs · 158048B · 14585a | 110.1 µs · 14920B · 1799a | — | 600.9 µs · 340635B · 9231a |
| i64-wide-fib | 28.54 ms · 2913456B · 364178a | 21.54 ms · 1942304B · 242785a | 681.2 µs · 16B · 1a | 41.9× | — | 239.3 µs · 0B · 0a | 23.59 µs · 90520B · 52a | — | 159.1 ms · 64193216B · 1942305a | 349.9 ms · 253472928B · 4248841a | — | 136.4 ms · 98686618B · 2138708a |
| indirect-recursive-fib | 1.194 ms · 0B · 0a | 1.209 ms · 0B · 0a | 60.55 µs · 0B · 0a | 19.7× | 98.86 µs · 8B · 1a | 35.15 µs · 0B · 0a | 2.122 ms · 319345B · 28655a | 1.954 ms · 703B · 2a | 2.536 ms · 4680B · 39a | 11.49 ms · 10158198B · 109494a | — | 27 ms · 13061362B · 394061a |
| iterative-fib | 1.133 µs · 0B · 0a | 1.071 µs · 0B · 0a | 76.81 ns · 0B · 0a | 14.8× | 87.16 ns · 8B · 1a | 12.47 ns · 0B · 0a | 24.15 µs · 90592B · 61a | 1.179 µs · 159B · 0a | 4.293 µs · 368B · 20a | 6.192 µs · 2448B · 88a | — | 6.246 µs · 944B · 81a |
| mandelbrot | 290.7 µs · 0B · 0a | 247.7 µs · 0B · 0a | 11.25 µs · 0B · 0a | 25.9× | — | 6.324 µs · 0B · 0a | 962.4 µs · 292345B · 25008a | 494.1 µs · 169408B · 661a | 8.059 ms · 5558656B · 104191a | 1.519 ms · 324978B · 23643a | — | 1.573 ms · 365401B · 22513a |
| map-ops | 70.79 µs · 8816B · 3a | 70.69 µs · 8816B · 3a | 11.11 µs · 8816B · 3a | 6.37× | — | 11.06 µs · 4904B · 3a | — | 276.6 µs · 114327B · 198a | 384.6 µs · 66728B · 4078a | — | — | 1.194 ms · 501521B · 15813a |
| mat-mul | 374.5 µs · 6216B · 6a | 374.3 µs · 6216B · 6a | 8.746 µs · 6216B · 6a | 42.8× | — | 5.659 µs · 6144B · 3a | 1.387 ms · 431714B · 34871a | 674.9 µs · 143168B · 478a | 1.219 ms · 61304B · 537a | 1.475 ms · 90712B · 9350a | — | 924.9 µs · 36613B · 6036a |
| n-body | 610.5 µs · 504B · 14a | 582.9 µs · 504B · 14a | 19.32 µs · 504B · 14a | 31.6× | — | 7.357 µs · 0B · 0a | 1.556 ms · 386898B · 35892a | 1.225 ms · 258913B · 2002a | 3.342 ms · 658435B · 81545a | 2.484 ms · 382715B · 34975a | — | 1.798 ms · 445472B · 20669a |
| n-queens | 430 µs · 120B · 6a | 429.5 µs · 120B · 6a | 31.62 µs · 120B · 6a | 13.6× | — | 6.014 µs · 0B · 0a | 961.7 µs · 234865B · 17784a | 570.4 µs · 14423B · 63a | 1.093 ms · 7424B · 35a | 1.458 ms · 410346B · 5147a | — | 2.051 ms · 766008B · 33371a |
| naive-checksum | 253.1 µs · 0B · 0a | 142.4 µs · 0B · 0a | 8.763 µs · 0B · 0a | 28.9× | — | 2.759 µs · 0B · 0a | 628.2 µs · 235753B · 18206a | 480.1 µs · 174767B · 682a | 898.6 µs · 155640B · 19402a | 806.6 µs · 163816B · 20409a | — | 314 µs · 8977B · 2071a |
| permutation-flips | 129.6 µs · 7680B · 128a | 129 µs · 7680B · 128a | 17.62 µs · 7680B · 128a | 7.35× | — | 1.864 ns · 0B · 0a | 570.4 µs · 292857B · 9856a | 236.3 µs · 78808B · 451a | 530.3 µs · 122504B · 765a | 496.5 µs · 111976B · 2368a | — | 1.07 µs · 528B · 13a |
| recursive-fib-20 | 712.1 µs · 0B · 0a | 718.1 µs · 0B · 0a | 47.23 µs · 0B · 0a | 15.1× | 42.74 µs · 8B · 1a | 21.55 µs · 0B · 0a | 1.966 ms · 319346B · 28655a | 2.175 ms · 704B · 2a | 2.962 ms · 4680B · 39a | 11.34 ms · 9807909B · 109494a | — | 11.56 ms · 8302721B · 192848a |
| recursive-fib-35 | 970.3 ms · 0B · 0a | 979.1 ms · 0B · 0a | 63.92 ms · 0B · 0a | 15.2× | 58.2 ms · 8B · 1a | 29.24 ms · 0B · 0a | 2.653 s · 312797584B · 39088168a | 2.953 s · 971008B · 3793a | 4.034 s · 375360B · 46373a | 15.71 s · 13378018416B · 149350002a | — | — |
| sieve | 20.43 µs · 1048B · 2a | 20.43 µs · 1048B · 2a | 966.4 ns · 1048B · 2a | 21.1× | 1.041 µs · 8B · 1a | 371.8 ns · 0B · 0a | 141.2 µs · 122504B · 1611a | 51.14 µs · 18416B · 44a | 84.23 µs · 1872B · 25a | 77.15 µs · 5704B · 30a | — | 81.19 µs · 4368B · 795a |
| sort-stress | 504.1 µs · 5136B · 512a | 506.3 µs · 5136B · 512a | 12.91 µs · 5136B · 512a | 39× | — | 5.406 µs · 1024B · 2a | 1.319 ms · 270945B · 19965a | 848.7 µs · 36568B · 365a | 1.794 ms · 49104B · 3621a | 1.788 ms · 23448B · 2034a | — | 921.6 µs · 17473B · 1708a |
| spectral-norm | 545.1 µs · 648B · 6a | 461.1 µs · 648B · 6a | 8.464 µs · 648B · 6a | 64.4× | — | 7.094 µs · 576B · 3a | 2.122 ms · 571370B · 54822a | 1.093 ms · 234784B · 925a | 2.493 ms · 275337B · 33729a | 4.634 ms · 2457132B · 52718a | — | 4.357 ms · 2448018B · 74233a |
| string-build | 739.4 µs · 85408B · 4107a | 741.7 µs · 85408B · 4107a | 613.9 µs · 85408B · 4107a | 1.2× | — | 408.5 µs · 855890B · 5001a | 2.44 ms · 2211274B · 36101a | 2.334 ms · 1078110B · 13859a | 3.136 ms · 1147383B · 26883a | 2.499 ms · 1326245B · 24469a | — | 3.163 ms · 2372795B · 41432a |
| string-compare | 1.086 ms · 21136B · 1284a | 1.082 ms · 21136B · 1284a | 223.4 µs · 21136B · 1284a | 4.86× | — | 66.22 µs · 9632B · 1585a | 1.69 ms · 296282B · 20528a | 1.139 ms · 59712B · 3431a | 2.325 ms · 50744B · 3848a | 1.846 ms · 209521B · 5520a | — | 2.423 ms · 503614B · 31846a |
| struct-tree-walk | 287.4 µs · 768B · 8a | 287.8 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.67× | — | 1.864 ns · 0B · 0a | 656.4 µs · 458379B · 5114a | 1.381 ms · 818512B · 11253a | 1.09 ms · 558961B · 6149a | 3.554 ms · 2570671B · 34797a | — | 1.256 µs · 600B · 17a |
| tail-ping-pong | 29.5 µs · 0B · 0a | 29.38 µs · 0B · 0a | 480.4 ns · 0B · 0a | 61.4× | — | 891 ns · 0B · 0a | 110.1 µs · 106112B · 2001a | 36.28 µs · 21856B · 85a | 113.4 µs · 13984B · 1726a | 119.4 µs · 14280B · 1731a | — | 570.2 µs · 348831B · 9016a |
| tail-sum | 29.5 µs · 0B · 0a | 29.51 µs · 0B · 0a | 682.4 ns · 0B · 0a | 43.2× | — | 302.6 ns · 0B · 0a | 110.5 µs · 106112B · 2001a | 36.17 µs · 21856B · 85a | 114.4 µs · 13984B · 1726a | 119.9 µs · 14280B · 1731a | — | 84.37 µs · 4608B · 1017a |
| typed-array-sum | 7.34 µs · 0B · 0a | 7.295 µs · 0B · 0a | 258.6 ns · 0B · 0a | 28.4× | 266.8 ns · 8B · 1a | 1.869 ns · 0B · 0a | 45.4 µs · 94208B · 513a | 8.481 µs · 4000B · 15a | 27.44 µs · 2080B · 238a | 17.68 µs · 2496B · 246a | — | — |
| xorshift-i64 | 114.6 µs · 18424B · 2046a | 115.1 µs · 18424B · 2046a | 1.395 µs · 18424B · 2046a | 82.2× | — | 356.2 ns · 0B · 0a | 105.5 µs · 107608B · 2188a | — | 1.016 ms · 447255B · 12736a | 438.3 µs · 199938B · 7772a | — | 103.1 µs · 13545B · 1317a |

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
