# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.047 µs · 0B · 0a | 5.511 µs · 0B · 0a | 1.64× | — | 2.23 µs · 1024B · 128a | 37.79 µs · 96289B · 388a | 17.25 µs · 14376B · 256a | 69.94 µs · 78016B · 770a | 14.25 µs · 5712B · 266a | — | 30.84 µs · 2308B · 276a |
| binary-trees | 1.979 ms · 768B · 8a | 594.6 µs · 768B · 8a | 3.33× | — | 291.4 µs · 201936B · 8414a | 3.938 ms · 1468185B · 62877a | 10.47 ms · 6995673B · 92703a | 8.921 ms · 5198698B · 69052a | 27.45 ms · 19457609B · 280714a | — | 28.54 ms · 15128165B · 446106a |
| branch-tree | 1.277 µs · 0B · 0a | 136.5 ns · 0B · 0a | 9.36× | 626.3 ns · 16B · 1a | 160.6 ns · 0B · 0a | 52.22 µs · 95384B · 660a | 15.73 µs · 2464B · 9a | 26.71 µs · 1992B · 196a | 23.71 µs · 2168B · 203a | — | 20.48 µs · 1640B · 219a |
| closure-counter | 5.479 µs · 64B · 2a | 902 ns · 64B · 2a | 6.07× | — | 39.81 ns · 0B · 0a | 42.14 µs · 92272B · 261a | 12.71 µs · 151B · 3a | 19.11 µs · 1264B · 13a | 78.15 µs · 58312B · 659a | — | 90.03 µs · 35628B · 922a |
| fannkuch | 822.6 µs · 34608B · 1442a | 239.4 µs · 34608B · 1442a | 3.44× | — | 35.36 µs · 17280B · 720a | 2.034 ms · 456579B · 25621a | 1.557 ms · 443136B · 1460a | 19.54 ms · 14005346B · 164946a | 3.358 ms · 1367678B · 16944a | — | 4.794 ms · 2501425B · 88439a |
| fnv1a64 | 121.4 µs · 16400B · 2049a | 1.148 µs · 16400B · 2049a | 106× | — | 902.7 ns · 0B · 0a | 163.1 µs · 120808B · 3838a | — | 1.489 ms · 608252B · 18475a | 392.4 µs · 162921B · 7429a | — | 153.4 µs · 17282B · 2074a |
| i64-wide-fib | 28.02 ms · 2913456B · 364178a | 679.5 µs · 16B · 1a | 41.2× | — | 239.5 µs · 0B · 0a | 23.32 µs · 90520B · 52a | — | 155.7 ms · 64193192B · 1942305a | 345.6 ms · 253472944B · 4248842a | — | 131.6 ms · 98686586B · 2138708a |
| indirect-recursive-fib | 1.188 ms · 0B · 0a | 60.49 µs · 0B · 0a | 19.6× | 98.97 µs · 8B · 1a | 35.3 µs · 0B · 0a | 2.086 ms · 319345B · 28655a | 1.939 ms · 704B · 2a | 2.569 ms · 4680B · 39a | 11.48 ms · 10158190B · 109494a | — | 24.14 ms · 13061282B · 394061a |
| iterative-fib | 1.105 µs · 0B · 0a | 70.39 ns · 0B · 0a | 15.7× | 85.7 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.2 µs · 90592B · 61a | 1.164 µs · 160B · 0a | 4.273 µs · 368B · 20a | 6.104 µs · 2448B · 88a | — | 6.161 µs · 944B · 81a |
| mandelbrot | 291 µs · 0B · 0a | 10.89 µs · 0B · 0a | 26.7× | — | 6.318 µs · 0B · 0a | 957 µs · 292345B · 25008a | 489 µs · 169408B · 661a | 8.174 ms · 5558646B · 104191a | 1.506 ms · 324979B · 23643a | — | 1.531 ms · 365402B · 22513a |
| mat-mul | 375.3 µs · 6216B · 6a | 8.666 µs · 6216B · 6a | 43.3× | — | 5.766 µs · 6144B · 3a | 1.379 ms · 431714B · 34871a | 683.6 µs · 143168B · 478a | 1.225 ms · 61304B · 537a | 1.467 ms · 90712B · 9350a | — | 923.8 µs · 36615B · 6036a |
| n-body | 604.3 µs · 504B · 14a | 18.77 µs · 504B · 14a | 32.2× | — | 7.346 µs · 0B · 0a | 1.504 ms · 386899B · 35892a | 1.241 ms · 258913B · 2002a | 3.288 ms · 658435B · 81545a | 2.452 ms · 382714B · 34975a | — | 1.746 ms · 445463B · 20669a |
| n-queens | 428.3 µs · 120B · 6a | 31.36 µs · 120B · 6a | 13.7× | — | 6.006 µs · 0B · 0a | 953.2 µs · 234865B · 17784a | 570.8 µs · 14423B · 63a | 1.099 ms · 7424B · 35a | 1.433 ms · 410346B · 5147a | — | 1.996 ms · 766006B · 33371a |
| permutation-flips | 129.6 µs · 7680B · 128a | 17.92 µs · 7680B · 128a | 7.23× | — | 1.889 ns · 0B · 0a | 580.4 µs · 292858B · 9856a | 239.4 µs · 78808B · 451a | 533.5 µs · 122504B · 765a | 490.7 µs · 111976B · 2368a | — | 1.008 µs · 528B · 13a |
| recursive-fib-20 | 713.5 µs · 0B · 0a | 47.14 µs · 0B · 0a | 15.1× | 42.73 µs · 8B · 1a | 21.49 µs · 0B · 0a | 2.009 ms · 319345B · 28655a | 2.153 ms · 704B · 2a | 2.96 ms · 4680B · 39a | 11.39 ms · 9807929B · 109494a | — | 10.66 ms · 8302722B · 192848a |
| recursive-fib-35 | 978.6 ms · 0B · 0a | 64.08 ms · 0B · 0a | 15.3× | 58.19 ms · 8B · 1a | 29.02 ms · 0B · 0a | 2.679 s · 312797232B · 39088167a | 2.946 s · 971008B · 3793a | 4.027 s · 375360B · 46373a | 15.87 s · 13378019792B · 149350015a | — | — |
| sieve | 19.89 µs · 1048B · 2a | 938 ns · 1048B · 2a | 21.2× | 1.031 µs · 8B · 1a | 351.3 ns · 0B · 0a | 139.2 µs · 122504B · 1611a | 50.94 µs · 18416B · 44a | 84.6 µs · 1872B · 25a | 77.63 µs · 5704B · 30a | — | 80.81 µs · 4368B · 795a |
| sort-stress | 501.4 µs · 5136B · 512a | 12.72 µs · 5136B · 512a | 39.4× | — | 5.859 µs · 1024B · 2a | 1.292 ms · 270946B · 19965a | 856.5 µs · 36567B · 365a | 1.79 ms · 49104B · 3621a | 1.796 ms · 23448B · 2034a | — | 923.7 µs · 17473B · 1708a |
| spectral-norm | 535 µs · 648B · 6a | 8.37 µs · 648B · 6a | 63.9× | — | 7.065 µs · 576B · 3a | 2.048 ms · 571371B · 54822a | 1.094 ms · 234784B · 925a | 2.453 ms · 275337B · 33729a | 4.742 ms · 2457134B · 52718a | — | 4.147 ms · 2448005B · 74233a |
| string-build | 727.4 µs · 85408B · 4107a | 628.6 µs · 85408B · 4107a | 1.16× | — | 396.6 µs · 855890B · 5001a | 2.533 ms · 2211274B · 36101a | 2.367 ms · 1078112B · 13859a | 3.159 ms · 1147381B · 26883a | 2.529 ms · 1326244B · 24469a | — | 3.119 ms · 2372781B · 41432a |
| string-compare | 1.088 ms · 21136B · 1284a | 245.6 µs · 21136B · 1284a | 4.43× | — | 63.36 µs · 9632B · 1585a | 1.707 ms · 296281B · 20528a | 1.137 ms · 59712B · 3431a | 2.326 ms · 50744B · 3848a | 1.839 ms · 209521B · 5520a | — | 2.36 ms · 503612B · 31846a |
| struct-tree-walk | 284.1 µs · 768B · 8a | 107.9 µs · 768B · 8a | 2.63× | — | 1.877 ns · 0B · 0a | 660.6 µs · 458378B · 5114a | 1.369 ms · 818511B · 11253a | 1.083 ms · 558961B · 6149a | 3.513 ms · 2570663B · 34797a | — | 1.181 µs · 600B · 17a |
| tail-ping-pong | 29.12 µs · 0B · 0a | 474.8 ns · 0B · 0a | 61.3× | — | 892.2 ns · 0B · 0a | 109.9 µs · 106112B · 2001a | 35.73 µs · 21855B · 85a | 112.9 µs · 13984B · 1726a | 115.9 µs · 14280B · 1731a | — | 552.8 µs · 348833B · 9016a |
| tail-sum | 29.14 µs · 0B · 0a | 671.1 ns · 0B · 0a | 43.4× | — | 301.5 ns · 0B · 0a | 110.4 µs · 106112B · 2001a | 36.64 µs · 21856B · 85a | 113.4 µs · 13984B · 1726a | 115.6 µs · 14280B · 1731a | — | 84.31 µs · 4608B · 1017a |
| typed-array-sum | 7.203 µs · 0B · 0a | 265.2 ns · 0B · 0a | 27.2× | 267.6 ns · 8B · 1a | 1.878 ns · 0B · 0a | 47.25 µs · 94208B · 513a | 8.469 µs · 4000B · 15a | 27.4 µs · 2080B · 238a | 17.51 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.2 µs · 18424B · 2046a | 1.441 µs · 18424B · 2046a | 77.2× | — | 360.2 ns · 0B · 0a | 105.3 µs · 107608B · 2188a | — | 989.8 µs · 447250B · 12735a | 437 µs · 199937B · 7772a | — | 101.5 µs · 13546B · 1317a |

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
