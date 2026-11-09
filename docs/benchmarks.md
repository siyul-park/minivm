# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.928 µs · 0B · 0a | 5.442 µs · 0B · 0a | 1.64× | — | 2.066 µs · 1024B · 128a | 37.73 µs · 96288B · 388a | 16.71 µs · 14376B · 256a | 70.34 µs · 78017B · 770a | 13.81 µs · 5712B · 266a | — | 31.02 µs · 2308B · 276a |
| binary-trees | 1.977 ms · 768B · 8a | 595.4 µs · 768B · 8a | 3.32× | — | 284.3 µs · 201936B · 8414a | 3.865 ms · 1468185B · 62877a | 10.97 ms · 6995662B · 92703a | 8.94 ms · 5198698B · 69052a | 27.98 ms · 19457601B · 280714a | — | 28.03 ms · 15128166B · 446106a |
| branch-tree | 1.316 µs · 0B · 0a | 139.6 ns · 0B · 0a | 9.43× | 625 ns · 16B · 1a | 160.6 ns · 0B · 0a | 45.95 µs · 95384B · 660a | 15.15 µs · 2464B · 9a | 26.33 µs · 1992B · 196a | 24.1 µs · 2168B · 203a | — | 20.92 µs · 1640B · 219a |
| closure-counter | 5.498 µs · 64B · 2a | 899.8 ns · 64B · 2a | 6.11× | — | 39.89 ns · 0B · 0a | 42.15 µs · 92272B · 261a | 12.66 µs · 151B · 3a | 19.26 µs · 1264B · 13a | 78.09 µs · 58312B · 659a | — | 88.8 µs · 35629B · 922a |
| fannkuch | 823.5 µs · 34608B · 1442a | 264.4 µs · 34608B · 1442a | 3.12× | — | 35.12 µs · 17280B · 720a | 2.033 ms · 456578B · 25621a | 1.56 ms · 443136B · 1460a | 19.07 ms · 14005343B · 164946a | 3.235 ms · 1367678B · 16944a | — | 4.653 ms · 2501429B · 88439a |
| fnv1a64 | 120.6 µs · 16400B · 2049a | 1.16 µs · 16400B · 2049a | 104× | — | 904.6 ns · 0B · 0a | 161.9 µs · 120808B · 3838a | — | 1.34 ms · 608252B · 18475a | 369.2 µs · 162921B · 7429a | — | 152.6 µs · 17282B · 2074a |
| i64-wide-fib | 28.11 ms · 2913456B · 364178a | 681.1 µs · 16B · 1a | 41.3× | — | 239.5 µs · 0B · 0a | 22.79 µs · 90520B · 52a | — | 157 ms · 64193352B · 1942306a | 353.5 ms · 253472816B · 4248841a | — | 124 ms · 98686586B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 60.46 µs · 0B · 0a | 19.6× | 99.06 µs · 8B · 1a | 35.43 µs · 0B · 0a | 2.113 ms · 319345B · 28655a | 1.918 ms · 703B · 2a | 2.583 ms · 4680B · 39a | 11.54 ms · 10158186B · 109494a | — | 24.09 ms · 13061340B · 394061a |
| iterative-fib | 1.103 µs · 0B · 0a | 74.15 ns · 0B · 0a | 14.9× | 84.21 ns · 8B · 1a | 11.9 ns · 0B · 0a | 25.5 µs · 90592B · 61a | 1.178 µs · 159B · 0a | 4.254 µs · 368B · 20a | 5.914 µs · 2448B · 88a | — | 6.145 µs · 944B · 81a |
| mandelbrot | 291.1 µs · 0B · 0a | 10.84 µs · 0B · 0a | 26.9× | — | 6.322 µs · 0B · 0a | 917.8 µs · 292345B · 25008a | 475.4 µs · 169408B · 661a | 8.034 ms · 5558654B · 104191a | 1.443 ms · 324979B · 23643a | — | 1.514 ms · 365400B · 22513a |
| mat-mul | 368 µs · 6216B · 6a | 8.756 µs · 6216B · 6a | 42× | — | 5.597 µs · 6144B · 3a | 1.346 ms · 431715B · 34871a | 658.9 µs · 143169B · 478a | 1.217 ms · 61304B · 537a | 1.44 ms · 90712B · 9350a | — | 926.1 µs · 36614B · 6036a |
| n-body | 609.6 µs · 504B · 14a | 18.55 µs · 504B · 14a | 32.9× | — | 7.347 µs · 0B · 0a | 1.494 ms · 386899B · 35892a | 1.208 ms · 258913B · 2002a | 3.232 ms · 658435B · 81545a | 2.361 ms · 382714B · 34975a | — | 1.739 ms · 445465B · 20669a |
| n-queens | 428.9 µs · 120B · 6a | 31.62 µs · 120B · 6a | 13.6× | — | 6.023 µs · 0B · 0a | 947.8 µs · 234865B · 17784a | 567.8 µs · 14423B · 63a | 1.11 ms · 7424B · 35a | 1.383 ms · 410346B · 5147a | — | 2.003 ms · 766004B · 33371a |
| permutation-flips | 127.9 µs · 7680B · 128a | 17.14 µs · 7680B · 128a | 7.46× | — | 1.875 ns · 0B · 0a | 553.8 µs · 292858B · 9856a | 230.6 µs · 78808B · 451a | 524.2 µs · 122504B · 765a | 479 µs · 111976B · 2368a | — | 979.3 ns · 528B · 13a |
| recursive-fib-20 | 710 µs · 0B · 0a | 47.38 µs · 0B · 0a | 15× | 42.76 µs · 8B · 1a | 21.45 µs · 0B · 0a | 1.992 ms · 319346B · 28655a | 2.149 ms · 704B · 2a | 3.005 ms · 4680B · 39a | 11.52 ms · 9807916B · 109494a | — | 10.5 ms · 8302694B · 192848a |
| recursive-fib-35 | 969.2 ms · 0B · 0a | 63.93 ms · 0B · 0a | 15.2× | 58.21 ms · 8B · 1a | 29.2 ms · 0B · 0a | 2.674 s · 312797216B · 39088167a | 3 s · 971008B · 3793a | 4.092 s · 375360B · 46373a | 15.55 s · 13378017024B · 149349973a | — | — |
| sieve | 19.96 µs · 1048B · 2a | 951 ns · 1048B · 2a | 21× | 1.02 µs · 8B · 1a | 350.6 ns · 0B · 0a | 148.1 µs · 122504B · 1611a | 51.02 µs · 18416B · 44a | 84.27 µs · 1872B · 25a | 75.85 µs · 5704B · 30a | — | 80.96 µs · 4368B · 795a |
| sort-stress | 503.8 µs · 5136B · 512a | 12.68 µs · 5136B · 512a | 39.7× | — | 5.784 µs · 1024B · 2a | 1.291 ms · 270945B · 19965a | 853.4 µs · 36568B · 365a | 1.802 ms · 49104B · 3621a | 1.775 ms · 23448B · 2034a | — | 920.4 µs · 17473B · 1708a |
| spectral-norm | 533.1 µs · 648B · 6a | 8.352 µs · 648B · 6a | 63.8× | — | 7.03 µs · 576B · 3a | 2.062 ms · 571372B · 54822a | 1.076 ms · 234784B · 925a | 2.449 ms · 275337B · 33729a | 4.659 ms · 2457132B · 52718a | — | 4.115 ms · 2448013B · 74233a |
| string-build | 716.1 µs · 85408B · 4107a | 608.6 µs · 85408B · 4107a | 1.18× | — | 366.3 µs · 855891B · 5001a | 2.401 ms · 2211277B · 36101a | 2.323 ms · 1078112B · 13859a | 3.114 ms · 1147385B · 26883a | 2.414 ms · 1326242B · 24469a | — | 3.082 ms · 2372780B · 41432a |
| string-compare | 1.094 ms · 21136B · 1284a | 241.7 µs · 21136B · 1284a | 4.52× | — | 62.38 µs · 9632B · 1585a | 1.664 ms · 296282B · 20528a | 1.126 ms · 59712B · 3431a | 2.327 ms · 50744B · 3848a | 1.789 ms · 209521B · 5520a | — | 2.354 ms · 503608B · 31846a |
| struct-tree-walk | 288.7 µs · 768B · 8a | 107.9 µs · 768B · 8a | 2.68× | — | 1.874 ns · 0B · 0a | 659.5 µs · 458379B · 5114a | 1.352 ms · 818512B · 11253a | 1.092 ms · 558961B · 6149a | 3.529 ms · 2570671B · 34797a | — | 1.165 µs · 600B · 17a |
| tail-ping-pong | 29 µs · 0B · 0a | 472.4 ns · 0B · 0a | 61.4× | — | 891.1 ns · 0B · 0a | 107.4 µs · 106112B · 2001a | 35.34 µs · 21856B · 85a | 110.2 µs · 13984B · 1726a | 113.1 µs · 14280B · 1731a | — | 531.8 µs · 348829B · 9016a |
| tail-sum | 28.99 µs · 0B · 0a | 681.1 ns · 0B · 0a | 42.6× | — | 301.6 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 35.57 µs · 21856B · 85a | 110.7 µs · 13984B · 1726a | 113.1 µs · 14280B · 1731a | — | 84.86 µs · 4608B · 1017a |
| typed-array-sum | 7.116 µs · 0B · 0a | 268.2 ns · 0B · 0a | 26.5× | 266.4 ns · 8B · 1a | 1.887 ns · 0B · 0a | 45.52 µs · 94208B · 513a | 8.24 µs · 4000B · 15a | 27.15 µs · 2080B · 238a | 16.86 µs · 2496B · 246a | — | — |
| xorshift-i64 | 109.1 µs · 18424B · 2046a | 1.409 µs · 18424B · 2046a | 77.5× | — | 360.2 ns · 0B · 0a | 102.1 µs · 107608B · 2188a | — | 957.7 µs · 447255B · 12736a | 416.1 µs · 199937B · 7772a | — | 100.7 µs · 13545B · 1317a |

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
