# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.753 µs · 0B · 0a | 5.341 µs · 0B · 0a | 1.64× | — | 2.148 µs · 1024B · 128a | 37.42 µs · 96288B · 388a | 16.93 µs · 14376B · 256a | 73.14 µs · 78017B · 770a | 14.12 µs · 5712B · 266a | — | 30.71 µs · 2308B · 276a |
| binary-trees | 1.979 ms · 768B · 8a | 604.1 µs · 768B · 8a | 3.28× | — | 290.2 µs · 201936B · 8414a | 3.814 ms · 1468182B · 62877a | 10.58 ms · 6995682B · 92703a | 8.903 ms · 5198702B · 69052a | 27.83 ms · 19457609B · 280714a | — | 28.53 ms · 15128168B · 446106a |
| branch-tree | 1.317 µs · 0B · 0a | 146.2 ns · 0B · 0a | 9.01× | 626.5 ns · 16B · 1a | 160.3 ns · 0B · 0a | 46.3 µs · 95384B · 660a | 15.25 µs · 2463B · 9a | 26.29 µs · 1992B · 196a | 23.83 µs · 2168B · 203a | — | 20.58 µs · 1640B · 219a |
| closure-counter | 5.49 µs · 64B · 2a | 916.1 ns · 64B · 2a | 5.99× | — | 39.84 ns · 0B · 0a | 49.64 µs · 92272B · 261a | 12.69 µs · 151B · 3a | 19.23 µs · 1264B · 13a | 79.94 µs · 58312B · 659a | — | 89.07 µs · 35629B · 922a |
| fannkuch | 822.1 µs · 34608B · 1442a | 267.1 µs · 34608B · 1442a | 3.08× | — | 35.45 µs · 17280B · 720a | 2.052 ms · 456578B · 25621a | 1.573 ms · 443136B · 1460a | 19.39 ms · 14005350B · 164946a | 3.383 ms · 1367678B · 16944a | — | 4.683 ms · 2501442B · 88439a |
| fnv1a64 | 121.9 µs · 16400B · 2049a | 1.229 µs · 16400B · 2049a | 99.2× | — | 902.4 ns · 0B · 0a | 167 µs · 120808B · 3838a | — | 1.455 ms · 608256B · 18475a | 398.5 µs · 162921B · 7429a | — | 153 µs · 17282B · 2074a |
| i64-wide-fib | 28.45 ms · 2913456B · 364178a | 776.8 µs · 16B · 1a | 36.6× | — | 239.2 µs · 0B · 0a | 23.43 µs · 90520B · 52a | — | 160.2 ms · 64193224B · 1942305a | 346.9 ms · 253472576B · 4248840a | — | 129.2 ms · 98686672B · 2138708a |
| indirect-recursive-fib | 1.203 ms · 0B · 0a | 66.12 µs · 0B · 0a | 18.2× | 99.08 µs · 8B · 1a | 35.23 µs · 0B · 0a | 2.086 ms · 319345B · 28655a | 1.926 ms · 704B · 2a | 2.54 ms · 4680B · 39a | 11.57 ms · 10158185B · 109494a | — | 24.44 ms · 13061291B · 394061a |
| iterative-fib | 1.103 µs · 0B · 0a | 72.68 ns · 0B · 0a | 15.2× | 85.16 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.65 µs · 90592B · 61a | 1.161 µs · 160B · 0a | 4.212 µs · 368B · 20a | 6.057 µs · 2448B · 88a | — | 6.151 µs · 944B · 81a |
| mandelbrot | 287.4 µs · 0B · 0a | 11.03 µs · 0B · 0a | 26.1× | — | 6.323 µs · 0B · 0a | 936.1 µs · 292346B · 25008a | 484.8 µs · 169408B · 661a | 8.111 ms · 5558663B · 104191a | 1.468 ms · 324978B · 23643a | — | 1.517 ms · 365396B · 22513a |
| mat-mul | 366.5 µs · 6216B · 6a | 8.557 µs · 6216B · 6a | 42.8× | — | 5.448 µs · 6144B · 3a | 1.357 ms · 431715B · 34871a | 674.3 µs · 143168B · 478a | 1.206 ms · 61305B · 537a | 1.458 ms · 90712B · 9350a | — | 918.5 µs · 36612B · 6036a |
| n-body | 605.6 µs · 504B · 14a | 18.73 µs · 504B · 14a | 32.3× | — | 7.349 µs · 0B · 0a | 1.499 ms · 386898B · 35892a | 1.213 ms · 258913B · 2002a | 3.243 ms · 658435B · 81545a | 2.382 ms · 382714B · 34975a | — | 1.743 ms · 445472B · 20669a |
| n-queens | 428 µs · 120B · 6a | 31.8 µs · 120B · 6a | 13.5× | — | 6.011 µs · 0B · 0a | 945.2 µs · 234865B · 17784a | 572.7 µs · 14423B · 63a | 1.095 ms · 7424B · 35a | 1.405 ms · 410345B · 5147a | — | 2.032 ms · 766011B · 33371a |
| permutation-flips | 128.3 µs · 7680B · 128a | 17.47 µs · 7680B · 128a | 7.35× | — | 1.877 ns · 0B · 0a | 551.7 µs · 292857B · 9856a | 232.9 µs · 78808B · 451a | 524.3 µs · 122504B · 765a | 485.2 µs · 111976B · 2368a | — | 999.1 ns · 528B · 13a |
| recursive-fib-20 | 711 µs · 0B · 0a | 53.32 µs · 0B · 0a | 13.3× | 42.73 µs · 8B · 1a | 21.42 µs · 0B · 0a | 1.993 ms · 319345B · 28655a | 2.158 ms · 704B · 2a | 2.969 ms · 4680B · 39a | 11.36 ms · 9807913B · 109494a | — | 10.55 ms · 8302691B · 192848a |
| recursive-fib-35 | 969.5 ms · 0B · 0a | 72.48 ms · 0B · 0a | 13.4× | 58.2 ms · 8B · 1a | 29.24 ms · 0B · 0a | 2.648 s · 312797232B · 39088167a | 2.944 s · 971008B · 3793a | 4.032 s · 375360B · 46373a | 15.51 s · 13378016512B · 149349960a | — | — |
| sieve | 19.94 µs · 1048B · 2a | 951.6 ns · 1048B · 2a | 21× | 1.037 µs · 8B · 1a | 350.9 ns · 0B · 0a | 138 µs · 122504B · 1611a | 52.57 µs · 18416B · 44a | 83.96 µs · 1872B · 25a | 77.1 µs · 5704B · 30a | — | 80.59 µs · 4368B · 795a |
| sort-stress | 504.4 µs · 5136B · 512a | 12.52 µs · 5136B · 512a | 40.3× | — | 5.79 µs · 1024B · 2a | 1.273 ms · 270945B · 19965a | 852.7 µs · 36568B · 365a | 1.786 ms · 49104B · 3621a | 1.796 ms · 23448B · 2034a | — | 924.3 µs · 17473B · 1708a |
| spectral-norm | 533 µs · 648B · 6a | 33.46 µs · 648B · 6a | 15.9× | — | 7.045 µs · 576B · 3a | 2.041 ms · 571371B · 54822a | 1.079 ms · 234784B · 925a | 2.405 ms · 275338B · 33729a | 4.709 ms · 2457137B · 52718a | — | 4.077 ms · 2448044B · 74233a |
| string-build | 717.6 µs · 85408B · 4107a | 618.7 µs · 85408B · 4107a | 1.16× | — | 406.1 µs · 855890B · 5001a | 2.403 ms · 2211274B · 36101a | 2.322 ms · 1078111B · 13859a | 3.085 ms · 1147386B · 26883a | 2.443 ms · 1326245B · 24469a | — | 3.124 ms · 2372785B · 41432a |
| string-compare | 1.078 ms · 21136B · 1284a | 266.5 µs · 21136B · 1284a | 4.05× | — | 62.4 µs · 9632B · 1585a | 1.671 ms · 296281B · 20528a | 1.131 ms · 59712B · 3431a | 2.317 ms · 50745B · 3848a | 1.799 ms · 209522B · 5520a | — | 2.359 ms · 503609B · 31846a |
| struct-tree-walk | 288.7 µs · 768B · 8a | 108.9 µs · 768B · 8a | 2.65× | — | 1.878 ns · 0B · 0a | 659.9 µs · 458379B · 5114a | 1.364 ms · 818511B · 11253a | 1.094 ms · 558961B · 6149a | 3.57 ms · 2570662B · 34797a | — | 1.199 µs · 600B · 17a |
| tail-ping-pong | 29.21 µs · 0B · 0a | 511.7 ns · 0B · 0a | 57.1× | — | 890.6 ns · 0B · 0a | 107.5 µs · 106112B · 2001a | 35.51 µs · 21856B · 85a | 110.9 µs · 13984B · 1726a | 115.8 µs · 14280B · 1731a | — | 544.5 µs · 348835B · 9016a |
| tail-sum | 29.04 µs · 0B · 0a | 667.7 ns · 0B · 0a | 43.5× | — | 301.6 ns · 0B · 0a | 108.4 µs · 106112B · 2001a | 35.21 µs · 21856B · 85a | 111.4 µs · 13984B · 1726a | 117.1 µs · 14280B · 1731a | — | 84.13 µs · 4608B · 1017a |
| typed-array-sum | 7.21 µs · 0B · 0a | 238.5 ns · 0B · 0a | 30.2× | 267.8 ns · 8B · 1a | 1.89 ns · 0B · 0a | 45.85 µs · 94208B · 513a | 8.317 µs · 4000B · 15a | 27.25 µs · 2080B · 238a | 17.12 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.2 µs · 18424B · 2046a | 1.415 µs · 18424B · 2046a | 78.6× | — | 360.8 ns · 0B · 0a | 103 µs · 107608B · 2188a | — | 954.8 µs · 447248B · 12735a | 414.5 µs · 199937B · 7772a | — | 101 µs · 13545B · 1317a |

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
