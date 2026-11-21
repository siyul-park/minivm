# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.867 µs · 0B · 0a | 8.855 µs · 0B · 0a | 5.308 µs · 0B · 0a | 1.67× | — | 2.242 µs · 1024B · 128a | 35.7 µs · 96288B · 388a | 16.65 µs · 14375B · 256a | 67.51 µs · 78016B · 770a | 14.4 µs · 5712B · 266a | — | 30.93 µs · 2308B · 276a |
| binary-trees | 1.959 ms · 768B · 8a | 1.955 ms · 768B · 8a | 590.4 µs · 768B · 8a | 3.32× | — | 286.7 µs · 201940B · 8414a | 3.816 ms · 1468187B · 62877a | 11.4 ms · 6995695B · 92703a | 10.6 ms · 5198724B · 69052a | 27.81 ms · 19457706B · 280715a | — | 30.45 ms · 15128171B · 446106a |
| branch-tree | 1.326 µs · 0B · 0a | 19.97 ns · 0B · 0a | 85.22 ns · 0B · 0a | 15.6× | 627.5 ns · 16B · 1a | 160.4 ns · 0B · 0a | 44.33 µs · 95384B · 660a | 15.38 µs · 2464B · 9a | 26.82 µs · 1992B · 196a | 24.14 µs · 2168B · 203a | — | 20.53 µs · 1640B · 219a |
| closure-counter | 5.541 µs · 64B · 2a | 5.518 µs · 64B · 2a | 917 ns · 64B · 2a | 6.04× | — | 40.11 ns · 0B · 0a | 39 µs · 92272B · 261a | 12.86 µs · 151B · 3a | 19.28 µs · 1264B · 13a | 77.47 µs · 58312B · 659a | — | 92.78 µs · 35629B · 922a |
| coroutine-gen | 78.79 µs · 128B · 2a | 78.91 µs · 128B · 2a | 78.8 µs · 128B · 2a | 1× | — | 42.11 µs · 208B · 7a | — | 329.5 µs · 149935B · 649a | 585.8 µs · 330649B · 6719a | 196.8 µs · 91800B · 3646a | — | — |
| fannkuch | 837.6 µs · 34608B · 1442a | 819.2 µs · 34608B · 1442a | 222.4 µs · 34608B · 1442a | 3.77× | — | 35.23 µs · 17280B · 720a | 2.027 ms · 456579B · 25621a | 1.528 ms · 443136B · 1460a | 18.52 ms · 14005358B · 164946a | 3.295 ms · 1367680B · 16944a | — | 4.842 ms · 2501442B · 88439a |
| fnv1a64 | 124.9 µs · 16400B · 2049a | 124.7 µs · 16400B · 2049a | 1.162 µs · 16400B · 2049a | 107× | — | 902.9 ns · 0B · 0a | 160.9 µs · 120808B · 3838a | — | 1.476 ms · 608253B · 18475a | 393.5 µs · 162921B · 7429a | — | 157.1 µs · 17282B · 2074a |
| host-call | 466.9 µs · 41752B · 3080a | 481.2 µs · 41752B · 3080a | 481 µs · 41752B · 3080a | 0.971× | — | 3.313 µs · 0B · 0a | 175.3 µs · 139264B · 3073a | 133.1 µs · 22536B · 1079a | 822 µs · 158048B · 14585a | 107.2 µs · 14920B · 1799a | — | 601.8 µs · 340638B · 9231a |
| i64-wide-fib | 28.72 ms · 2913456B · 364178a | 21.13 ms · 1942304B · 242785a | 679.7 µs · 16B · 1a | 42.2× | — | 239.2 µs · 0B · 0a | 21.5 µs · 90520B · 52a | — | 155.7 ms · 64193288B · 1942305a | 340 ms · 253472688B · 4248840a | — | 132.3 ms · 98686730B · 2138709a |
| indirect-recursive-fib | 1.19 ms · 0B · 0a | 1.244 ms · 0B · 0a | 60.46 µs · 0B · 0a | 19.7× | 99.06 µs · 8B · 1a | 35.04 µs · 0B · 0a | 2.105 ms · 319346B · 28655a | 1.948 ms · 704B · 2a | 2.556 ms · 4680B · 39a | 10.89 ms · 10158198B · 109494a | — | 25.6 ms · 13061364B · 394062a |
| iterative-fib | 1.132 µs · 0B · 0a | 1.07 µs · 0B · 0a | 82.64 ns · 0B · 0a | 13.7× | 86.79 ns · 8B · 1a | 11.91 ns · 0B · 0a | 22.62 µs · 90592B · 61a | 1.149 µs · 160B · 0a | 4.308 µs · 368B · 20a | 6.075 µs · 2448B · 88a | — | 6.204 µs · 944B · 81a |
| mandelbrot | 290.5 µs · 0B · 0a | 248.7 µs · 0B · 0a | 11.26 µs · 0B · 0a | 25.8× | — | 6.325 µs · 0B · 0a | 950 µs · 292345B · 25008a | 480.9 µs · 169408B · 661a | 7.861 ms · 5558664B · 104191a | 1.508 ms · 324977B · 23643a | — | 1.566 ms · 365403B · 22513a |
| map-ops | 70.11 µs · 8816B · 3a | 70.41 µs · 8816B · 3a | 10.63 µs · 8816B · 3a | 6.6× | — | 10.75 µs · 4904B · 3a | — | 272.5 µs · 114327B · 198a | 382.9 µs · 66728B · 4078a | — | — | 1.173 ms · 501519B · 15813a |
| mat-mul | 375.3 µs · 6216B · 6a | 375.6 µs · 6216B · 6a | 8.421 µs · 6216B · 6a | 44.6× | — | 5.305 µs · 6144B · 3a | 1.367 ms · 431714B · 34871a | 668.9 µs · 143168B · 478a | 1.217 ms · 61304B · 537a | 1.448 ms · 90712B · 9350a | — | 935 µs · 36615B · 6036a |
| n-body | 614.4 µs · 504B · 14a | 581.7 µs · 504B · 14a | 20.01 µs · 504B · 14a | 30.7× | — | 7.344 µs · 0B · 0a | 1.514 ms · 386898B · 35892a | 1.213 ms · 258913B · 2002a | 3.402 ms · 658435B · 81545a | 2.449 ms · 382714B · 34975a | — | 1.768 ms · 445464B · 20669a |
| n-queens | 429.3 µs · 120B · 6a | 429.5 µs · 120B · 6a | 31.54 µs · 120B · 6a | 13.6× | — | 6.004 µs · 0B · 0a | 951.6 µs · 234865B · 17784a | 571.8 µs · 14423B · 63a | 1.097 ms · 7424B · 35a | 1.423 ms · 410345B · 5147a | — | 2.016 ms · 765999B · 33371a |
| naive-checksum | 253.4 µs · 0B · 0a | 142.7 µs · 0B · 0a | 8.732 µs · 0B · 0a | 29× | — | 2.759 µs · 0B · 0a | 620.9 µs · 235753B · 18206a | 471.2 µs · 174768B · 682a | 886.3 µs · 155640B · 19402a | 805.5 µs · 163817B · 20409a | — | 312.5 µs · 8977B · 2071a |
| permutation-flips | 129.7 µs · 7680B · 128a | 128.7 µs · 7680B · 128a | 17.07 µs · 7680B · 128a | 7.6× | — | 1.87 ns · 0B · 0a | 539.3 µs · 292858B · 9856a | 231.6 µs · 78808B · 451a | 521.6 µs · 122504B · 765a | 490.6 µs · 111976B · 2368a | — | 1.033 µs · 528B · 13a |
| recursive-fib-20 | 717.1 µs · 0B · 0a | 715.5 µs · 0B · 0a | 47.14 µs · 0B · 0a | 15.2× | 42.73 µs · 8B · 1a | 21.54 µs · 0B · 0a | 1.973 ms · 319345B · 28655a | 2.17 ms · 704B · 2a | 2.962 ms · 4680B · 39a | 11 ms · 9807915B · 109494a | — | 11.1 ms · 8302695B · 192848a |
| recursive-fib-35 | 973.9 ms · 0B · 0a | 974.3 ms · 0B · 0a | 63.95 ms · 0B · 0a | 15.2× | 58.21 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.623 s · 312797616B · 39088168a | 2.963 s · 971008B · 3793a | 4.035 s · 375360B · 46373a | 15.34 s · 13378017424B · 149349967a | — | — |
| sieve | 20.24 µs · 1048B · 2a | 20.36 µs · 1048B · 2a | 917.5 ns · 1048B · 2a | 22.1× | 1.036 µs · 8B · 1a | 350.8 ns · 0B · 0a | 134.9 µs · 122504B · 1611a | 50.17 µs · 18416B · 44a | 84.31 µs · 1872B · 25a | 76.33 µs · 5704B · 30a | — | 80.97 µs · 4368B · 795a |
| sort-stress | 504 µs · 5136B · 512a | 507.1 µs · 5136B · 512a | 12.81 µs · 5136B · 512a | 39.3× | — | 5.792 µs · 1024B · 2a | 1.276 ms · 270945B · 19965a | 854.4 µs · 36567B · 365a | 1.794 ms · 49104B · 3621a | 1.784 ms · 23448B · 2034a | — | 927.7 µs · 17473B · 1708a |
| spectral-norm | 532.4 µs · 648B · 6a | 458.8 µs · 648B · 6a | 8.368 µs · 648B · 6a | 63.6× | — | 7.032 µs · 576B · 3a | 2.084 ms · 571371B · 54822a | 1.08 ms · 234784B · 925a | 2.492 ms · 275338B · 33729a | 4.605 ms · 2457135B · 52718a | — | 4.26 ms · 2448018B · 74233a |
| string-build | 732.2 µs · 85408B · 4107a | 729.1 µs · 85408B · 4107a | 601.7 µs · 85408B · 4107a | 1.22× | — | 362.1 µs · 855889B · 5001a | 2.388 ms · 2211274B · 36101a | 2.274 ms · 1078111B · 13859a | 3.136 ms · 1147384B · 26883a | 2.443 ms · 1326243B · 24469a | — | 3.094 ms · 2372793B · 41432a |
| string-compare | 1.085 ms · 21136B · 1284a | 1.087 ms · 21136B · 1284a | 221 µs · 21136B · 1284a | 4.91× | — | 64.08 µs · 9632B · 1585a | 1.697 ms · 296281B · 20528a | 1.137 ms · 59712B · 3431a | 2.328 ms · 50744B · 3848a | 1.808 ms · 209521B · 5520a | — | 2.377 ms · 503610B · 31846a |
| struct-tree-walk | 282.3 µs · 768B · 8a | 281.9 µs · 768B · 8a | 108 µs · 768B · 8a | 2.61× | — | 1.894 ns · 0B · 0a | 641.3 µs · 458379B · 5114a | 1.35 ms · 818512B · 11253a | 1.066 ms · 558960B · 6149a | 3.432 ms · 2570667B · 34797a | — | 1.25 µs · 600B · 17a |
| tail-ping-pong | 29.46 µs · 0B · 0a | 29.39 µs · 0B · 0a | 483.6 ns · 0B · 0a | 60.9× | — | 891.2 ns · 0B · 0a | 106.9 µs · 106112B · 2001a | 35.5 µs · 21856B · 85a | 112.4 µs · 13984B · 1726a | 117.5 µs · 14280B · 1731a | — | 548.8 µs · 348835B · 9016a |
| tail-sum | 29.42 µs · 0B · 0a | 29.41 µs · 0B · 0a | 670.5 ns · 0B · 0a | 43.9× | — | 301.1 ns · 0B · 0a | 107.8 µs · 106112B · 2001a | 35.02 µs · 21856B · 85a | 112.2 µs · 13984B · 1726a | 117.1 µs · 14280B · 1731a | — | 84.49 µs · 4608B · 1017a |
| typed-array-sum | 7.381 µs · 0B · 0a | 7.383 µs · 0B · 0a | 260.3 ns · 0B · 0a | 28.4× | 266.4 ns · 8B · 1a | 1.86 ns · 0B · 0a | 43.64 µs · 94208B · 513a | 8.285 µs · 4000B · 15a | 27.5 µs · 2080B · 238a | 17.37 µs · 2496B · 246a | — | — |
| xorshift-i64 | 113.5 µs · 18424B · 2046a | 112.6 µs · 18424B · 2046a | 1.288 µs · 18424B · 2046a | 88.1× | — | 360.8 ns · 0B · 0a | 100.3 µs · 107608B · 2188a | — | 988.6 µs · 447248B · 12735a | 427.7 µs · 199937B · 7772a | — | 100.9 µs · 13545B · 1317a |

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
