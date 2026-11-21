# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.867 µs · 0B · 0a | 8.861 µs · 0B · 0a | 5.329 µs · 0B · 0a | 1.66× | — | 2.248 µs · 1024B · 128a | 36.61 µs · 96288B · 388a | 17.06 µs · 14376B · 256a | 69.79 µs · 78016B · 770a | 14.09 µs · 5712B · 266a | — | 31 µs · 2308B · 276a |
| binary-trees | 1.959 ms · 768B · 8a | 1.96 ms · 768B · 8a | 592.3 µs · 768B · 8a | 3.31× | — | 284.8 µs · 201936B · 8414a | 3.806 ms · 1468184B · 62877a | 10.41 ms · 6995679B · 92703a | 8.766 ms · 5198699B · 69052a | 27.11 ms · 19457627B · 280714a | — | 29.15 ms · 15128169B · 446106a |
| branch-tree | 1.328 µs · 0B · 0a | 20.11 ns · 0B · 0a | 87.21 ns · 0B · 0a | 15.2× | 626.8 ns · 16B · 1a | 160.4 ns · 0B · 0a | 44.8 µs · 95384B · 660a | 15.18 µs · 2464B · 9a | 26.49 µs · 1992B · 196a | 23.99 µs · 2168B · 203a | — | 20.72 µs · 1640B · 219a |
| closure-counter | 5.546 µs · 64B · 2a | 5.559 µs · 64B · 2a | 923.5 ns · 64B · 2a | 6.01× | — | 40.11 ns · 0B · 0a | 41.11 µs · 92272B · 261a | 12.9 µs · 151B · 3a | 19.11 µs · 1264B · 13a | 78.17 µs · 58312B · 659a | — | 92.38 µs · 35629B · 922a |
| coroutine-gen | 78.87 µs · 128B · 2a | 78.42 µs · 128B · 2a | 78.83 µs · 128B · 2a | 1× | — | 42.76 µs · 208B · 7a | — | 331.7 µs · 149936B · 649a | 594.2 µs · 330649B · 6719a | 200.6 µs · 91800B · 3646a | — | — |
| fannkuch | 833.4 µs · 34608B · 1442a | 814.5 µs · 34608B · 1442a | 223.7 µs · 34608B · 1442a | 3.73× | — | 35.3 µs · 17280B · 720a | 2 ms · 456578B · 25621a | 1.546 ms · 443135B · 1460a | 19.32 ms · 14005332B · 164946a | 3.336 ms · 1367677B · 16944a | — | 4.847 ms · 2501447B · 88439a |
| fnv1a64 | 124.5 µs · 16400B · 2049a | 124.6 µs · 16400B · 2049a | 1.161 µs · 16400B · 2049a | 107× | — | 903 ns · 0B · 0a | 161.2 µs · 120808B · 3838a | — | 1.443 ms · 608253B · 18475a | 395.2 µs · 162921B · 7429a | — | 155 µs · 17282B · 2074a |
| host-call | 472.3 µs · 41752B · 3080a | 480.3 µs · 41752B · 3080a | 479.1 µs · 41752B · 3080a | 0.986× | — | 3.313 µs · 0B · 0a | 178.4 µs · 139264B · 3073a | 132.5 µs · 22536B · 1079a | 804.6 µs · 158048B · 14585a | 106.8 µs · 14920B · 1799a | — | 596.9 µs · 340636B · 9231a |
| i64-wide-fib | 28.48 ms · 2913456B · 364178a | 20.81 ms · 1942304B · 242785a | 679.6 µs · 16B · 1a | 41.9× | — | 239.1 µs · 0B · 0a | 22.83 µs · 90520B · 52a | — | 155.7 ms · 64193224B · 1942305a | 343.2 ms · 253472944B · 4248841a | — | 133.4 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 1.184 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.6× | 98.72 µs · 8B · 1a | 34.98 µs · 0B · 0a | 2.072 ms · 319346B · 28655a | 1.956 ms · 703B · 2a | 2.549 ms · 4680B · 39a | 11.25 ms · 10158192B · 109494a | — | 24.36 ms · 13061301B · 394061a |
| iterative-fib | 1.132 µs · 0B · 0a | 1.07 µs · 0B · 0a | 82.91 ns · 0B · 0a | 13.7× | 85.48 ns · 8B · 1a | 11.9 ns · 0B · 0a | 23 µs · 90592B · 61a | 1.15 µs · 160B · 0a | 4.204 µs · 368B · 20a | 6.096 µs · 2448B · 88a | — | 6.201 µs · 944B · 81a |
| mandelbrot | 290.8 µs · 0B · 0a | 249.1 µs · 0B · 0a | 11.27 µs · 0B · 0a | 25.8× | — | 6.317 µs · 0B · 0a | 952.9 µs · 292345B · 25008a | 484.5 µs · 169408B · 661a | 7.96 ms · 5558649B · 104191a | 1.506 ms · 324978B · 23643a | — | 1.555 ms · 365408B · 22513a |
| map-ops | 70.18 µs · 8816B · 3a | 70.04 µs · 8816B · 3a | 10.65 µs · 8816B · 3a | 6.59× | — | 10.77 µs · 4904B · 3a | — | 273.2 µs · 114328B · 198a | 375.9 µs · 66728B · 4078a | — | — | 1.173 ms · 501524B · 15813a |
| mat-mul | 375.3 µs · 6216B · 6a | 376.3 µs · 6216B · 6a | 8.471 µs · 6216B · 6a | 44.3× | — | 5.299 µs · 6144B · 3a | 1.346 ms · 431714B · 34871a | 681.6 µs · 143168B · 478a | 1.22 ms · 61304B · 537a | 1.436 ms · 90712B · 9350a | — | 930.2 µs · 36616B · 6036a |
| n-body | 615.3 µs · 504B · 14a | 581.6 µs · 504B · 14a | 18.74 µs · 504B · 14a | 32.8× | — | 7.346 µs · 0B · 0a | 1.506 ms · 386898B · 35892a | 1.214 ms · 258913B · 2002a | 3.387 ms · 658435B · 81545a | 2.448 ms · 382715B · 34975a | — | 1.812 ms · 445469B · 20669a |
| n-queens | 429.1 µs · 120B · 6a | 429.1 µs · 120B · 6a | 31.59 µs · 120B · 6a | 13.6× | — | 5.997 µs · 0B · 0a | 946.7 µs · 234865B · 17784a | 574.5 µs · 14423B · 63a | 1.095 ms · 7424B · 35a | 1.419 ms · 410346B · 5147a | — | 2.016 ms · 766009B · 33371a |
| naive-checksum | 253.2 µs · 0B · 0a | 142 µs · 0B · 0a | 8.723 µs · 0B · 0a | 29× | — | 2.759 µs · 0B · 0a | 604.4 µs · 235753B · 18206a | 473.7 µs · 174768B · 682a | 846 µs · 155640B · 19402a | 760.1 µs · 163817B · 20409a | — | 312.9 µs · 8977B · 2071a |
| permutation-flips | 129 µs · 7680B · 128a | 128.2 µs · 7680B · 128a | 16.81 µs · 7680B · 128a | 7.67× | — | 1.869 ns · 0B · 0a | 548.6 µs · 292858B · 9856a | 233.2 µs · 78808B · 451a | 528.3 µs · 122504B · 765a | 481.4 µs · 111976B · 2368a | — | 997 ns · 528B · 13a |
| recursive-fib-20 | 716.9 µs · 0B · 0a | 718.2 µs · 0B · 0a | 47.62 µs · 0B · 0a | 15.1× | 42.75 µs · 8B · 1a | 21.48 µs · 0B · 0a | 1.916 ms · 319346B · 28655a | 2.17 ms · 704B · 2a | 2.964 ms · 4680B · 39a | 11.17 ms · 9807919B · 109494a | — | 10.8 ms · 8302682B · 192847a |
| recursive-fib-35 | 972 ms · 0B · 0a | 977.8 ms · 0B · 0a | 63.94 ms · 0B · 0a | 15.2× | 58.18 ms · 8B · 1a | 29.03 ms · 0B · 0a | 2.581 s · 312797488B · 39088169a | 3.002 s · 971008B · 3793a | 4.043 s · 375360B · 46373a | 15.27 s · 13378016256B · 149349953a | — | — |
| sieve | 20.36 µs · 1048B · 2a | 20.41 µs · 1048B · 2a | 923.8 ns · 1048B · 2a | 22× | 1.035 µs · 8B · 1a | 351.4 ns · 0B · 0a | 135.1 µs · 122504B · 1611a | 50.09 µs · 18416B · 44a | 84.29 µs · 1872B · 25a | 76.39 µs · 5704B · 30a | — | 81.19 µs · 4368B · 795a |
| sort-stress | 505.2 µs · 5136B · 512a | 509.1 µs · 5136B · 512a | 12.78 µs · 5136B · 512a | 39.5× | — | 5.774 µs · 1024B · 2a | 1.291 ms · 270945B · 19965a | 856.8 µs · 36567B · 365a | 1.799 ms · 49104B · 3621a | 1.782 ms · 23448B · 2034a | — | 926 µs · 17475B · 1708a |
| spectral-norm | 534.1 µs · 648B · 6a | 460.3 µs · 648B · 6a | 8.368 µs · 648B · 6a | 63.8× | — | 7.033 µs · 576B · 3a | 2.073 ms · 571371B · 54822a | 1.092 ms · 234784B · 925a | 2.493 ms · 275337B · 33729a | 4.6 ms · 2457133B · 52718a | — | 4.255 ms · 2448000B · 74233a |
| string-build | 732.2 µs · 85408B · 4107a | 725.8 µs · 85408B · 4107a | 609.3 µs · 85408B · 4107a | 1.2× | — | 348.2 µs · 855889B · 5001a | 2.353 ms · 2211273B · 36101a | 2.301 ms · 1078110B · 13859a | 3.073 ms · 1147385B · 26883a | 2.427 ms · 1326242B · 24469a | — | 3.077 ms · 2372798B · 41432a |
| string-compare | 1.086 ms · 21136B · 1284a | 1.12 ms · 21136B · 1284a | 220.2 µs · 21136B · 1284a | 4.93× | — | 64.28 µs · 9632B · 1585a | 1.691 ms · 296281B · 20528a | 1.145 ms · 59712B · 3431a | 2.33 ms · 50744B · 3848a | 1.827 ms · 209521B · 5520a | — | 2.391 ms · 503614B · 31846a |
| struct-tree-walk | 282.6 µs · 768B · 8a | 281.5 µs · 768B · 8a | 107.3 µs · 768B · 8a | 2.63× | — | 1.892 ns · 0B · 0a | 652.3 µs · 458379B · 5114a | 1.369 ms · 818511B · 11253a | 1.081 ms · 558960B · 6149a | 3.453 ms · 2570667B · 34797a | — | 1.222 µs · 600B · 17a |
| tail-ping-pong | 29.45 µs · 0B · 0a | 29.16 µs · 0B · 0a | 476.9 ns · 0B · 0a | 61.7× | — | 892 ns · 0B · 0a | 107.8 µs · 106112B · 2001a | 35.15 µs · 21855B · 85a | 111.4 µs · 13984B · 1726a | 115.6 µs · 14280B · 1731a | — | 562.4 µs · 348833B · 9016a |
| tail-sum | 29.28 µs · 0B · 0a | 29.42 µs · 0B · 0a | 697.2 ns · 0B · 0a | 42× | — | 301.2 ns · 0B · 0a | 107.3 µs · 106112B · 2001a | 35.12 µs · 21856B · 85a | 111.5 µs · 13984B · 1726a | 115.8 µs · 14280B · 1731a | — | 84.75 µs · 4608B · 1017a |
| typed-array-sum | 7.369 µs · 0B · 0a | 7.365 µs · 0B · 0a | 257.9 ns · 0B · 0a | 28.6× | 266.5 ns · 8B · 1a | 1.864 ns · 0B · 0a | 43.87 µs · 94208B · 513a | 8.203 µs · 4000B · 15a | 27.37 µs · 2080B · 238a | 17.36 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.6 µs · 18424B · 2046a | 110.8 µs · 18424B · 2046a | 1.31 µs · 18424B · 2046a | 85.2× | — | 361 ns · 0B · 0a | 101.2 µs · 107608B · 2188a | — | 995.1 µs · 447252B · 12735a | 421.3 µs · 199937B · 7772a | — | 101.1 µs · 13545B · 1317a |

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
