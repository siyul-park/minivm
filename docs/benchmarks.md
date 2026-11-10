# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.297 µs · 0B · 0a | 5.59 µs · 0B · 0a | 1.66× | — | 2.239 µs · 1024B · 128a | 37.84 µs · 96288B · 388a | 16.83 µs · 14376B · 256a | 69.61 µs · 78016B · 770a | 14.25 µs · 5712B · 266a | — | 30.87 µs · 2308B · 276a |
| binary-trees | 2.009 ms · 768B · 8a | 601.5 µs · 768B · 8a | 3.34× | — | 321.9 µs · 201937B · 8414a | 3.949 ms · 1468184B · 62877a | 10.56 ms · 6995672B · 92703a | 8.971 ms · 5198706B · 69052a | 27.43 ms · 19457649B · 280714a | — | 29 ms · 15128172B · 446106a |
| branch-tree | 1.335 µs · 0B · 0a | 142.1 ns · 0B · 0a | 9.39× | 625.9 ns · 16B · 1a | 160.4 ns · 0B · 0a | 46.18 µs · 95384B · 660a | 15.16 µs · 2463B · 9a | 26.18 µs · 1992B · 196a | 24.02 µs · 2168B · 203a | — | 20.5 µs · 1640B · 219a |
| closure-counter | 5.501 µs · 64B · 2a | 891.6 ns · 64B · 2a | 6.17× | — | 39.81 ns · 0B · 0a | 42.4 µs · 92272B · 261a | 12.76 µs · 151B · 3a | 19.2 µs · 1264B · 13a | 80.09 µs · 58312B · 659a | — | 88.38 µs · 35628B · 922a |
| fannkuch | 828.1 µs · 34608B · 1442a | 267.6 µs · 34608B · 1442a | 3.1× | — | 34.88 µs · 17280B · 720a | 2.037 ms · 456579B · 25621a | 1.546 ms · 443136B · 1460a | 19.5 ms · 14005356B · 164946a | 3.34 ms · 1367676B · 16944a | — | 4.717 ms · 2501434B · 88439a |
| fnv1a64 | 119.4 µs · 16400B · 2049a | 1.147 µs · 16400B · 2049a | 104× | — | 902.9 ns · 0B · 0a | 162.1 µs · 120809B · 3838a | — | 1.463 ms · 608254B · 18475a | 390.9 µs · 162921B · 7429a | — | 153.8 µs · 17282B · 2074a |
| i64-wide-fib | 27.91 ms · 2913456B · 364178a | 679.8 µs · 16B · 1a | 41.1× | — | 239.2 µs · 0B · 0a | 23.66 µs · 90520B · 52a | — | 162 ms · 64193368B · 1942306a | 351.9 ms · 253472944B · 4248842a | — | 131.3 ms · 98686618B · 2138708a |
| indirect-recursive-fib | 1.189 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.7× | 100.1 µs · 8B · 1a | 35.02 µs · 0B · 0a | 2.073 ms · 319345B · 28655a | 1.93 ms · 704B · 2a | 2.521 ms · 4680B · 39a | 11.58 ms · 10158186B · 109494a | — | 24.04 ms · 13061283B · 394061a |
| iterative-fib | 1.108 µs · 0B · 0a | 70.18 ns · 0B · 0a | 15.8× | 86.07 ns · 8B · 1a | 11.89 ns · 0B · 0a | 25.09 µs · 90592B · 61a | 1.163 µs · 160B · 0a | 4.229 µs · 368B · 20a | 6.122 µs · 2448B · 88a | — | 6.18 µs · 944B · 81a |
| mandelbrot | 289.8 µs · 0B · 0a | 10.9 µs · 0B · 0a | 26.6× | — | 6.326 µs · 0B · 0a | 941.4 µs · 292345B · 25008a | 494.4 µs · 169408B · 661a | 8.199 ms · 5558650B · 104191a | 1.515 ms · 324978B · 23643a | — | 1.524 ms · 365400B · 22513a |
| mat-mul | 370.4 µs · 6216B · 6a | 8.781 µs · 6216B · 6a | 42.2× | — | 5.835 µs · 6144B · 3a | 1.383 ms · 431714B · 34871a | 694.4 µs · 143168B · 478a | 1.209 ms · 61304B · 537a | 1.453 ms · 90712B · 9350a | — | 921.6 µs · 36615B · 6036a |
| n-body | 604.8 µs · 504B · 14a | 18.91 µs · 504B · 14a | 32× | — | 7.347 µs · 0B · 0a | 1.519 ms · 386898B · 35892a | 1.231 ms · 258913B · 2002a | 3.259 ms · 658435B · 81545a | 2.452 ms · 382714B · 34975a | — | 1.758 ms · 445468B · 20669a |
| n-queens | 430.2 µs · 120B · 6a | 31.55 µs · 120B · 6a | 13.6× | — | 6.005 µs · 0B · 0a | 964.4 µs · 234865B · 17784a | 572.8 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.425 ms · 410345B · 5147a | — | 2.001 ms · 766009B · 33371a |
| permutation-flips | 130 µs · 7680B · 128a | 18.02 µs · 7680B · 128a | 7.21× | — | 1.877 ns · 0B · 0a | 573.1 µs · 292857B · 9856a | 237.5 µs · 78808B · 451a | 535.2 µs · 122504B · 765a | 494.3 µs · 111976B · 2368a | — | 1.001 µs · 528B · 13a |
| recursive-fib-20 | 718.9 µs · 0B · 0a | 47.15 µs · 0B · 0a | 15.2× | 42.74 µs · 8B · 1a | 21.54 µs · 0B · 0a | 1.987 ms · 319345B · 28655a | 2.242 ms · 703B · 2a | 2.953 ms · 4680B · 39a | 11.32 ms · 9807912B · 109494a | — | 10.62 ms · 8302674B · 192847a |
| recursive-fib-35 | 967.9 ms · 0B · 0a | 63.93 ms · 0B · 0a | 15.1× | 58.21 ms · 8B · 1a | 29.14 ms · 0B · 0a | 2.661 s · 312797280B · 39088167a | 2.959 s · 971008B · 3793a | 4.008 s · 375360B · 46373a | 15.65 s · 13378014944B · 149349974a | — | — |
| sieve | 20.05 µs · 1048B · 2a | 956.5 ns · 1048B · 2a | 21× | 1.04 µs · 8B · 1a | 350.7 ns · 0B · 0a | 139.5 µs · 122504B · 1611a | 52.76 µs · 18416B · 44a | 84.13 µs · 1872B · 25a | 77.71 µs · 5704B · 30a | — | 80.77 µs · 4368B · 795a |
| sort-stress | 500.8 µs · 5136B · 512a | 12.67 µs · 5136B · 512a | 39.5× | — | 5.923 µs · 1024B · 2a | 1.304 ms · 270945B · 19965a | 857.8 µs · 36568B · 365a | 1.788 ms · 49104B · 3621a | 1.795 ms · 23448B · 2034a | — | 933.1 µs · 17474B · 1708a |
| spectral-norm | 536.3 µs · 648B · 6a | 8.395 µs · 648B · 6a | 63.9× | — | 7.093 µs · 576B · 3a | 2.079 ms · 571371B · 54822a | 1.128 ms · 234783B · 925a | 2.473 ms · 275337B · 33729a | 4.75 ms · 2457134B · 52718a | — | 4.18 ms · 2447995B · 74233a |
| string-build | 722.7 µs · 85408B · 4107a | 626.2 µs · 85408B · 4107a | 1.15× | — | 404 µs · 855889B · 5001a | 2.502 ms · 2211275B · 36101a | 2.342 ms · 1078110B · 13859a | 3.197 ms · 1147384B · 26883a | 2.508 ms · 1326240B · 24469a | — | 3.152 ms · 2372785B · 41432a |
| string-compare | 1.087 ms · 21136B · 1284a | 245.1 µs · 21136B · 1284a | 4.43× | — | 63.6 µs · 9632B · 1585a | 1.693 ms · 296281B · 20528a | 1.142 ms · 59712B · 3431a | 2.335 ms · 50744B · 3848a | 1.86 ms · 209521B · 5520a | — | 2.375 ms · 503611B · 31846a |
| struct-tree-walk | 284 µs · 768B · 8a | 108.2 µs · 768B · 8a | 2.63× | — | 1.882 ns · 0B · 0a | 653.7 µs · 458379B · 5114a | 1.388 ms · 818512B · 11253a | 1.097 ms · 558960B · 6149a | 3.531 ms · 2570662B · 34797a | — | 1.196 µs · 600B · 17a |
| tail-ping-pong | 28.86 µs · 0B · 0a | 475.8 ns · 0B · 0a | 60.7× | — | 891 ns · 0B · 0a | 109.5 µs · 106112B · 2001a | 37.03 µs · 21856B · 85a | 111.5 µs · 13984B · 1726a | 117.8 µs · 14280B · 1731a | — | 556.2 µs · 348830B · 9016a |
| tail-sum | 29.22 µs · 0B · 0a | 664.2 ns · 0B · 0a | 44× | — | 301.7 ns · 0B · 0a | 109.5 µs · 106112B · 2001a | 36.04 µs · 21856B · 85a | 110.5 µs · 13984B · 1726a | 117.2 µs · 14280B · 1731a | — | 84.12 µs · 4608B · 1017a |
| typed-array-sum | 7.242 µs · 0B · 0a | 257.6 ns · 0B · 0a | 28.1× | 268.1 ns · 8B · 1a | 1.89 ns · 0B · 0a | 45.1 µs · 94208B · 513a | 8.403 µs · 4000B · 15a | 26.99 µs · 2080B · 238a | 17.4 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.9 µs · 18424B · 2046a | 1.414 µs · 18424B · 2046a | 78.4× | — | 360.1 ns · 0B · 0a | 104.6 µs · 107608B · 2188a | — | 1.006 ms · 447251B · 12735a | 425.7 µs · 199937B · 7772a | — | 102 µs · 13545B · 1317a |

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
