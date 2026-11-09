# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.755 µs · 0B · 0a | 5.336 µs · 0B · 0a | 1.64× | — | 2.239 µs · 1024B · 128a | 37.39 µs · 96288B · 388a | 16.89 µs · 14376B · 256a | 69.83 µs · 78016B · 770a | 13.98 µs · 5712B · 266a | — | 30.86 µs · 2308B · 276a |
| binary-trees | 1.954 ms · 768B · 8a | 599.9 µs · 768B · 8a | 3.26× | — | 283.2 µs · 201936B · 8414a | 3.812 ms · 1468183B · 62877a | 10.5 ms · 6995673B · 92703a | 8.78 ms · 5198698B · 69052a | 27.45 ms · 19458040B · 280714a | — | 29.35 ms · 15128230B · 446106a |
| branch-tree | 1.317 µs · 0B · 0a | 146.2 ns · 0B · 0a | 9.01× | 626.1 ns · 16B · 1a | 160.4 ns · 0B · 0a | 46.4 µs · 95384B · 660a | 15.24 µs · 2464B · 9a | 26.35 µs · 1992B · 196a | 23.73 µs · 2168B · 203a | — | 20.47 µs · 1640B · 219a |
| closure-counter | 5.494 µs · 64B · 2a | 908.9 ns · 64B · 2a | 6.04× | — | 39.82 ns · 0B · 0a | 41.13 µs · 92272B · 261a | 12.71 µs · 152B · 3a | 19.18 µs · 1264B · 13a | 77.21 µs · 58312B · 659a | — | 88.19 µs · 35628B · 922a |
| fannkuch | 819.8 µs · 34608B · 1442a | 265.8 µs · 34608B · 1442a | 3.08× | — | 35.02 µs · 17280B · 720a | 2.009 ms · 456578B · 25621a | 1.553 ms · 443135B · 1460a | 18.62 ms · 14005364B · 164946a | 3.266 ms · 1367680B · 16944a | — | 4.659 ms · 2501431B · 88439a |
| fnv1a64 | 121.3 µs · 16400B · 2049a | 1.232 µs · 16400B · 2049a | 98.4× | — | 904.4 ns · 0B · 0a | 162.2 µs · 120808B · 3838a | — | 1.399 ms · 608248B · 18475a | 376.4 µs · 162921B · 7429a | — | 153 µs · 17282B · 2074a |
| i64-wide-fib | 28.11 ms · 2913456B · 364178a | 776.4 µs · 16B · 1a | 36.2× | — | 239.2 µs · 0B · 0a | 22.31 µs · 90520B · 52a | — | 151.4 ms · 64193256B · 1942305a | 345.7 ms · 253472960B · 4248841a | — | 122.4 ms · 98686629B · 2138708a |
| indirect-recursive-fib | 1.183 ms · 0B · 0a | 66.1 µs · 0B · 0a | 17.9× | 98.97 µs · 8B · 1a | 35.1 µs · 0B · 0a | 2.058 ms · 319345B · 28655a | 1.933 ms · 704B · 2a | 2.541 ms · 4680B · 39a | 11.09 ms · 10158190B · 109494a | — | 23.96 ms · 13061347B · 394061a |
| iterative-fib | 1.102 µs · 0B · 0a | 72.84 ns · 0B · 0a | 15.1× | 85.2 ns · 8B · 1a | 11.9 ns · 0B · 0a | 23.89 µs · 90592B · 61a | 1.173 µs · 160B · 0a | 4.24 µs · 368B · 20a | 6.001 µs · 2448B · 88a | — | 6.134 µs · 944B · 81a |
| mandelbrot | 290.5 µs · 0B · 0a | 11.07 µs · 0B · 0a | 26.3× | — | 6.325 µs · 0B · 0a | 930.6 µs · 292345B · 25008a | 479.3 µs · 169408B · 661a | 7.875 ms · 5558648B · 104191a | 1.449 ms · 324978B · 23643a | — | 1.518 ms · 365399B · 22513a |
| mat-mul | 368.6 µs · 6216B · 6a | 8.644 µs · 6216B · 6a | 42.6× | — | 5.458 µs · 6144B · 3a | 1.333 ms · 431714B · 34871a | 698.3 µs · 143168B · 478a | 1.201 ms · 61304B · 537a | 1.459 ms · 90712B · 9350a | — | 917.4 µs · 36616B · 6036a |
| n-body | 605.6 µs · 504B · 14a | 18.52 µs · 504B · 14a | 32.7× | — | 7.343 µs · 0B · 0a | 1.461 ms · 386898B · 35892a | 1.21 ms · 258913B · 2002a | 3.198 ms · 658435B · 81545a | 2.378 ms · 382713B · 34975a | — | 1.744 ms · 445468B · 20669a |
| n-queens | 428.3 µs · 120B · 6a | 31.97 µs · 120B · 6a | 13.4× | — | 6.009 µs · 0B · 0a | 928.7 µs · 234865B · 17784a | 573 µs · 14423B · 63a | 1.095 ms · 7424B · 35a | 1.397 ms · 410346B · 5147a | — | 1.987 ms · 766006B · 33371a |
| permutation-flips | 127.5 µs · 7680B · 128a | 17.27 µs · 7680B · 128a | 7.38× | — | 1.876 ns · 0B · 0a | 557.7 µs · 292858B · 9856a | 234 µs · 78808B · 451a | 525.5 µs · 122504B · 765a | 481.9 µs · 111976B · 2368a | — | 983.1 ns · 528B · 13a |
| recursive-fib-20 | 712.5 µs · 0B · 0a | 53.37 µs · 0B · 0a | 13.3× | 42.76 µs · 8B · 1a | 21.47 µs · 0B · 0a | 1.974 ms · 319346B · 28655a | 2.165 ms · 704B · 2a | 2.968 ms · 4680B · 39a | 11.34 ms · 9807927B · 109494a | — | 11.31 ms · 8302738B · 192848a |
| recursive-fib-35 | 973 ms · 0B · 0a | 72.48 ms · 0B · 0a | 13.4× | 58.2 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.643 s · 312797216B · 39088167a | 2.945 s · 971008B · 3793a | 4.029 s · 375360B · 46373a | 15.48 s · 13378017584B · 149349961a | — | — |
| sieve | 19.89 µs · 1048B · 2a | 937.1 ns · 1048B · 2a | 21.2× | 1.029 µs · 8B · 1a | 350.9 ns · 0B · 0a | 138.5 µs · 122504B · 1611a | 50.5 µs · 18416B · 44a | 84.01 µs · 1872B · 25a | 77.06 µs · 5704B · 30a | — | 80.94 µs · 4368B · 795a |
| sort-stress | 503.1 µs · 5136B · 512a | 12.43 µs · 5136B · 512a | 40.5× | — | 5.789 µs · 1024B · 2a | 1.259 ms · 270945B · 19965a | 852.6 µs · 36568B · 365a | 1.789 ms · 49104B · 3621a | 1.795 ms · 23448B · 2034a | — | 924.8 µs · 17473B · 1708a |
| spectral-norm | 529.4 µs · 648B · 6a | 33.45 µs · 648B · 6a | 15.8× | — | 7.04 µs · 576B · 3a | 2.021 ms · 571371B · 54822a | 1.078 ms · 234784B · 925a | 2.444 ms · 275337B · 33729a | 4.671 ms · 2457134B · 52718a | — | 3.995 ms · 2447986B · 74233a |
| string-build | 718.8 µs · 85408B · 4107a | 603.5 µs · 85408B · 4107a | 1.19× | — | 367.7 µs · 855889B · 5001a | 2.396 ms · 2211276B · 36101a | 2.317 ms · 1078113B · 13859a | 3.151 ms · 1147384B · 26883a | 2.461 ms · 1326242B · 24469a | — | 3.097 ms · 2372780B · 41432a |
| string-compare | 1.084 ms · 21136B · 1284a | 265.7 µs · 21136B · 1284a | 4.08× | — | 62.5 µs · 9632B · 1585a | 1.659 ms · 296281B · 20528a | 1.139 ms · 59712B · 3431a | 2.338 ms · 50744B · 3848a | 1.797 ms · 209521B · 5520a | — | 2.37 ms · 503620B · 31846a |
| struct-tree-walk | 283 µs · 768B · 8a | 109.2 µs · 768B · 8a | 2.59× | — | 1.877 ns · 0B · 0a | 642.5 µs · 458379B · 5114a | 1.353 ms · 818514B · 11253a | 1.086 ms · 558960B · 6149a | 3.484 ms · 2570667B · 34797a | — | 1.155 µs · 600B · 17a |
| tail-ping-pong | 29.07 µs · 0B · 0a | 509.6 ns · 0B · 0a | 57× | — | 890.5 ns · 0B · 0a | 106.2 µs · 106112B · 2001a | 35.83 µs · 21856B · 85a | 111.6 µs · 13984B · 1726a | 115.6 µs · 14280B · 1731a | — | 535.5 µs · 348835B · 9016a |
| tail-sum | 29.14 µs · 0B · 0a | 663.5 ns · 0B · 0a | 43.9× | — | 301.3 ns · 0B · 0a | 108.5 µs · 106112B · 2001a | 35.53 µs · 21856B · 85a | 110.7 µs · 13984B · 1726a | 115.4 µs · 14280B · 1731a | — | 84.26 µs · 4608B · 1017a |
| typed-array-sum | 7.163 µs · 0B · 0a | 235.9 ns · 0B · 0a | 30.4× | 267.9 ns · 8B · 1a | 1.889 ns · 0B · 0a | 44.59 µs · 94208B · 513a | 8.357 µs · 4000B · 15a | 27.1 µs · 2080B · 238a | 17.01 µs · 2496B · 246a | — | — |
| xorshift-i64 | 109.9 µs · 18424B · 2046a | 1.407 µs · 18424B · 2046a | 78.1× | — | 360.2 ns · 0B · 0a | 102.4 µs · 107608B · 2188a | — | 970.1 µs · 447251B · 12735a | 415.3 µs · 199937B · 7772a | — | 101.3 µs · 13545B · 1317a |

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
