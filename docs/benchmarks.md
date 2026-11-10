# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.286 µs · 0B · 0a | 5.495 µs · 0B · 0a | 1.69× | — | 2.213 µs · 1024B · 128a | 37.41 µs · 96289B · 388a | 17.19 µs · 14376B · 256a | 70.14 µs · 78016B · 770a | 14.02 µs · 5712B · 266a | — | 30.87 µs · 2308B · 276a |
| binary-trees | 1.969 ms · 768B · 8a | 596.9 µs · 768B · 8a | 3.3× | — | 289 µs · 201936B · 8414a | 3.877 ms · 1468183B · 62877a | 11.02 ms · 6995703B · 92703a | 10.57 ms · 5198703B · 69052a | 29.08 ms · 19457649B · 280714a | — | 29.24 ms · 15128200B · 446106a |
| branch-tree | 1.323 µs · 0B · 0a | 139.9 ns · 0B · 0a | 9.46× | 625.7 ns · 16B · 1a | 160.4 ns · 0B · 0a | 46.77 µs · 95384B · 660a | 15.11 µs · 2464B · 9a | 26.39 µs · 1992B · 196a | 24.17 µs · 2168B · 203a | — | 20.78 µs · 1640B · 219a |
| closure-counter | 5.481 µs · 64B · 2a | 886.8 ns · 64B · 2a | 6.18× | — | 39.82 ns · 0B · 0a | 40.84 µs · 92272B · 261a | 12.85 µs · 151B · 3a | 19.09 µs · 1264B · 13a | 78.76 µs · 58312B · 659a | — | 92.46 µs · 35629B · 922a |
| fannkuch | 827.4 µs · 34608B · 1442a | 271.6 µs · 34608B · 1442a | 3.05× | — | 34.99 µs · 17280B · 720a | 2.031 ms · 456578B · 25621a | 1.54 ms · 443136B · 1460a | 19.1 ms · 14005335B · 164946a | 3.334 ms · 1367677B · 16944a | — | 4.88 ms · 2501446B · 88439a |
| fnv1a64 | 124.9 µs · 16400B · 2049a | 1.152 µs · 16400B · 2049a | 108× | — | 902.9 ns · 0B · 0a | 161.3 µs · 120808B · 3838a | — | 1.454 ms · 608257B · 18475a | 391.2 µs · 162921B · 7429a | — | 153.4 µs · 17282B · 2074a |
| i64-wide-fib | 28.32 ms · 2913456B · 364178a | 681.2 µs · 16B · 1a | 41.6× | — | 238.4 µs · 0B · 0a | 23 µs · 90520B · 52a | — | 159.1 ms · 64193320B · 1942306a | 345.9 ms · 253472448B · 4248839a | — | 132 ms · 98686608B · 2138708a |
| indirect-recursive-fib | 1.183 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.6× | 99.08 µs · 8B · 1a | 34.82 µs · 0B · 0a | 2.095 ms · 319346B · 28655a | 1.934 ms · 704B · 2a | 2.523 ms · 4680B · 39a | 11.4 ms · 10158187B · 109494a | — | 23.76 ms · 13061305B · 394061a |
| iterative-fib | 1.104 µs · 0B · 0a | 71.42 ns · 0B · 0a | 15.5× | 84.91 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.23 µs · 90592B · 61a | 1.158 µs · 160B · 0a | 4.25 µs · 368B · 20a | 6.177 µs · 2448B · 88a | — | 6.165 µs · 944B · 81a |
| mandelbrot | 291.7 µs · 0B · 0a | 10.93 µs · 0B · 0a | 26.7× | — | 6.326 µs · 0B · 0a | 964.4 µs · 292345B · 25008a | 474.1 µs · 169407B · 661a | 8.032 ms · 5558654B · 104191a | 1.489 ms · 324978B · 23643a | — | 1.528 ms · 365402B · 22513a |
| mat-mul | 368.5 µs · 6216B · 6a | 8.551 µs · 6216B · 6a | 43.1× | — | 5.424 µs · 6144B · 3a | 1.373 ms · 431715B · 34871a | 657 µs · 143168B · 478a | 1.21 ms · 61304B · 537a | 1.457 ms · 90712B · 9350a | — | 920.1 µs · 36615B · 6036a |
| n-body | 608.5 µs · 504B · 14a | 18.66 µs · 504B · 14a | 32.6× | — | 7.346 µs · 0B · 0a | 1.529 ms · 386898B · 35892a | 1.223 ms · 258913B · 2002a | 3.278 ms · 658435B · 81545a | 2.442 ms · 382714B · 34975a | — | 1.745 ms · 445462B · 20669a |
| n-queens | 431.5 µs · 120B · 6a | 31.5 µs · 120B · 6a | 13.7× | — | 6.025 µs · 0B · 0a | 951.4 µs · 234865B · 17784a | 568.8 µs · 14423B · 63a | 1.095 ms · 7424B · 35a | 1.419 ms · 410345B · 5147a | — | 2 ms · 766001B · 33371a |
| permutation-flips | 128.7 µs · 7680B · 128a | 18.17 µs · 7680B · 128a | 7.08× | — | 1.874 ns · 0B · 0a | 561.5 µs · 292857B · 9856a | 232.9 µs · 78808B · 451a | 529 µs · 122504B · 765a | 490.8 µs · 111976B · 2368a | — | 1.017 µs · 528B · 13a |
| recursive-fib-20 | 709.8 µs · 0B · 0a | 47.3 µs · 0B · 0a | 15× | 42.74 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.997 ms · 319346B · 28655a | 2.194 ms · 703B · 2a | 2.975 ms · 4680B · 39a | 11.69 ms · 9807914B · 109494a | — | 10.82 ms · 8302709B · 192848a |
| recursive-fib-35 | 977.9 ms · 0B · 0a | 63.88 ms · 0B · 0a | 15.3× | 58.21 ms · 8B · 1a | 29.06 ms · 0B · 0a | 2.651 s · 312797296B · 39088168a | 2.958 s · 971008B · 3793a | 4.05 s · 375360B · 46373a | 15.86 s · 13378017104B · 149349996a | — | — |
| sieve | 19.95 µs · 1048B · 2a | 948.6 ns · 1048B · 2a | 21× | 1.042 µs · 8B · 1a | 351 ns · 0B · 0a | 138.8 µs · 122504B · 1611a | 50.54 µs · 18416B · 44a | 83.99 µs · 1872B · 25a | 76.32 µs · 5704B · 30a | — | 81.73 µs · 4368B · 795a |
| sort-stress | 501.3 µs · 5136B · 512a | 12.66 µs · 5136B · 512a | 39.6× | — | 5.815 µs · 1024B · 2a | 1.293 ms · 270945B · 19965a | 852 µs · 36568B · 365a | 1.795 ms · 49104B · 3621a | 1.797 ms · 23448B · 2034a | — | 924.1 µs · 17474B · 1708a |
| spectral-norm | 535 µs · 648B · 6a | 8.695 µs · 648B · 6a | 61.5× | — | 7.033 µs · 576B · 3a | 2.055 ms · 571371B · 54822a | 1.072 ms · 234784B · 925a | 2.441 ms · 275337B · 33729a | 4.738 ms · 2457137B · 52718a | — | 4.154 ms · 2448025B · 74233a |
| string-build | 717.9 µs · 85408B · 4107a | 613.3 µs · 85408B · 4107a | 1.17× | — | 365.5 µs · 855890B · 5001a | 2.385 ms · 2211271B · 36101a | 2.3 ms · 1078109B · 13859a | 3.111 ms · 1147382B · 26883a | 2.442 ms · 1326242B · 24469a | — | 3.107 ms · 2372781B · 41432a |
| string-compare | 1.08 ms · 21136B · 1284a | 242.8 µs · 21136B · 1284a | 4.45× | — | 62.67 µs · 9632B · 1585a | 1.683 ms · 296282B · 20528a | 1.13 ms · 59711B · 3431a | 2.32 ms · 50744B · 3848a | 1.826 ms · 209521B · 5520a | — | 2.358 ms · 503609B · 31846a |
| struct-tree-walk | 283.6 µs · 768B · 8a | 108.2 µs · 768B · 8a | 2.62× | — | 1.875 ns · 0B · 0a | 649.2 µs · 458379B · 5114a | 1.37 ms · 818513B · 11253a | 1.083 ms · 558960B · 6149a | 3.512 ms · 2570667B · 34797a | — | 1.172 µs · 600B · 17a |
| tail-ping-pong | 29.11 µs · 0B · 0a | 467.8 ns · 0B · 0a | 62.2× | — | 891 ns · 0B · 0a | 107.2 µs · 106112B · 2001a | 35.11 µs · 21856B · 85a | 111.4 µs · 13984B · 1726a | 115.3 µs · 14280B · 1731a | — | 531.1 µs · 348833B · 9016a |
| tail-sum | 28.93 µs · 0B · 0a | 667.3 ns · 0B · 0a | 43.4× | — | 301.7 ns · 0B · 0a | 107.9 µs · 106112B · 2001a | 35.39 µs · 21856B · 85a | 111.2 µs · 13984B · 1726a | 115.3 µs · 14280B · 1731a | — | 85.02 µs · 4608B · 1017a |
| typed-array-sum | 7.254 µs · 0B · 0a | 261.3 ns · 0B · 0a | 27.8× | 266.8 ns · 8B · 1a | 1.887 ns · 0B · 0a | 45.71 µs · 94208B · 513a | 8.318 µs · 4000B · 15a | 27.29 µs · 2080B · 238a | 16.91 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111 µs · 18424B · 2046a | 1.452 µs · 18424B · 2046a | 76.4× | — | 361.5 ns · 0B · 0a | 103.2 µs · 107608B · 2188a | — | 954.7 µs · 447255B · 12736a | 412.8 µs · 199938B · 7772a | — | 100.9 µs · 13545B · 1317a |

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
