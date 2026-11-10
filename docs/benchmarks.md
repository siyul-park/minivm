# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.255 µs · 0B · 0a | 5.338 µs · 0B · 0a | 1.73× | — | 2.149 µs · 1024B · 128a | 39.24 µs · 96288B · 388a | 17.49 µs · 14376B · 256a | 70.92 µs · 78016B · 770a | 14.16 µs · 5712B · 266a | — | 30.4 µs · 2308B · 276a |
| binary-trees | 2 ms · 768B · 8a | 586.8 µs · 768B · 8a | 3.41× | — | 290.9 µs · 201936B · 8414a | 3.801 ms · 1468183B · 62877a | 10.64 ms · 6995673B · 92703a | 9.061 ms · 5198698B · 69052a | 28.18 ms · 19457620B · 280714a | — | 29.24 ms · 15128174B · 446106a |
| branch-tree | 1.343 µs · 0B · 0a | 151.5 ns · 0B · 0a | 8.86× | 626.7 ns · 16B · 1a | 160.8 ns · 0B · 0a | 47.15 µs · 95384B · 660a | 15.18 µs · 2464B · 9a | 26.56 µs · 1992B · 196a | 23.79 µs · 2168B · 203a | — | 20.58 µs · 1640B · 219a |
| closure-counter | 5.535 µs · 64B · 2a | 908.4 ns · 64B · 2a | 6.09× | — | 40.36 ns · 0B · 0a | 42.09 µs · 92273B · 261a | 12.95 µs · 151B · 3a | 19.23 µs · 1264B · 13a | 80.69 µs · 58312B · 659a | — | 88.31 µs · 35629B · 922a |
| coroutine-gen | 77.82 µs · 128B · 2a | 77.53 µs · 128B · 2a | 1× | — | 42.87 µs · 208B · 7a | — | 337.1 µs · 149936B · 649a | 600.7 µs · 330649B · 6719a | 201.2 µs · 91800B · 3646a | — | — |
| fannkuch | 831.5 µs · 34608B · 1442a | 223.7 µs · 34608B · 1442a | 3.72× | — | 35.38 µs · 17280B · 720a | 1.97 ms · 456579B · 25621a | 1.556 ms · 443136B · 1460a | 19.82 ms · 14005336B · 164946a | 3.306 ms · 1367678B · 16944a | — | 4.706 ms · 2501444B · 88439a |
| fnv1a64 | 121.6 µs · 16400B · 2049a | 1.153 µs · 16400B · 2049a | 105× | — | 902.6 ns · 0B · 0a | 163.7 µs · 120808B · 3838a | — | 1.46 ms · 608253B · 18475a | 389.3 µs · 162921B · 7429a | — | 153.4 µs · 17282B · 2074a |
| host-call | 704.4 µs · 196592B · 14073a | 703.4 µs · 196592B · 14073a | 1× | — | 3.314 µs · 0B · 0a | 180.4 µs · 139264B · 3073a | 129 µs · 22536B · 1079a | 776.7 µs · 158048B · 14585a | 107 µs · 14920B · 1799a | — | 550.9 µs · 340641B · 9231a |
| i64-wide-fib | 27.99 ms · 2913456B · 364178a | 679 µs · 16B · 1a | 41.2× | — | 240 µs · 0B · 0a | 24.38 µs · 90520B · 52a | — | 152.8 ms · 64193296B · 1942305a | 351.7 ms · 253472720B · 4248840a | — | 124.8 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 1.183 ms · 0B · 0a | 60.77 µs · 0B · 0a | 19.5× | 98.77 µs · 8B · 1a | 34.85 µs · 0B · 0a | 2.065 ms · 319346B · 28655a | 1.984 ms · 704B · 2a | 2.545 ms · 4680B · 39a | 11.5 ms · 10158184B · 109494a | — | 24.23 ms · 13061316B · 394061a |
| iterative-fib | 1.12 µs · 0B · 0a | 82.37 ns · 0B · 0a | 13.6× | 85.43 ns · 8B · 1a | 12.46 ns · 0B · 0a | 25.88 µs · 90592B · 61a | 1.153 µs · 160B · 0a | 4.238 µs · 368B · 20a | 6.068 µs · 2448B · 88a | — | 6.207 µs · 944B · 81a |
| mandelbrot | 291.6 µs · 0B · 0a | 10.92 µs · 0B · 0a | 26.7× | — | 6.324 µs · 0B · 0a | 935.5 µs · 292345B · 25008a | 493 µs · 169407B · 661a | 8.327 ms · 5558654B · 104191a | 1.464 ms · 324980B · 23643a | — | 1.537 ms · 365397B · 22513a |
| map-ops | 75.2 µs · 9584B · 5a | 75.17 µs · 9584B · 5a | 1× | — | 11.28 µs · 4904B · 3a | — | 285 µs · 114328B · 198a | 374.5 µs · 66728B · 4078a | — | — | 1.116 ms · 501524B · 15813a |
| mat-mul | 375 µs · 6216B · 6a | 9.055 µs · 6216B · 6a | 41.4× | — | 5.747 µs · 6144B · 3a | 1.349 ms · 431715B · 34871a | 679 µs · 143168B · 478a | 1.21 ms · 61304B · 537a | 1.442 ms · 90712B · 9350a | — | 924.4 µs · 36615B · 6036a |
| n-body | 605.3 µs · 504B · 14a | 19.14 µs · 504B · 14a | 31.6× | — | 7.36 µs · 0B · 0a | 1.476 ms · 386898B · 35892a | 1.221 ms · 258913B · 2002a | 3.238 ms · 658435B · 81545a | 2.388 ms · 382716B · 34975a | — | 1.77 ms · 445460B · 20669a |
| n-queens | 428.3 µs · 120B · 6a | 31.66 µs · 120B · 6a | 13.5× | — | 6.027 µs · 0B · 0a | 929 µs · 234865B · 17784a | 576.1 µs · 14423B · 63a | 1.1 ms · 7424B · 35a | 1.394 ms · 410345B · 5147a | — | 2.031 ms · 766007B · 33371a |
| permutation-flips | 129 µs · 7680B · 128a | 17.05 µs · 7680B · 128a | 7.57× | — | 1.874 ns · 0B · 0a | 560.3 µs · 292857B · 9856a | 237 µs · 78808B · 451a | 523.2 µs · 122504B · 765a | 483.8 µs · 111976B · 2368a | — | 1.014 µs · 528B · 13a |
| recursive-fib-20 | 716.8 µs · 0B · 0a | 47.54 µs · 0B · 0a | 15.1× | 42.74 µs · 8B · 1a | 21.48 µs · 0B · 0a | 1.923 ms · 319345B · 28655a | 2.222 ms · 704B · 2a | 2.96 ms · 4680B · 39a | 11.66 ms · 9807914B · 109494a | — | 10.62 ms · 8302688B · 192848a |
| recursive-fib-35 | 973 ms · 0B · 0a | 64.48 ms · 0B · 0a | 15.1× | 58.25 ms · 8B · 1a | 29.1 ms · 0B · 0a | 2.582 s · 312797136B · 39088167a | 2.999 s · 971008B · 3793a | 4.027 s · 375360B · 46373a | 15.94 s · 13378015936B · 149349979a | — | — |
| sieve | 20.08 µs · 1048B · 2a | 990.5 ns · 1048B · 2a | 20.3× | 1.016 µs · 8B · 1a | 371.9 ns · 0B · 0a | 142.4 µs · 122504B · 1611a | 51.15 µs · 18416B · 44a | 84.41 µs · 1872B · 25a | 77.01 µs · 5704B · 30a | — | 80.32 µs · 4368B · 795a |
| sort-stress | 508.7 µs · 5136B · 512a | 12.68 µs · 5136B · 512a | 40.1× | — | 5.377 µs · 1024B · 2a | 1.258 ms · 270946B · 19965a | 850.4 µs · 36567B · 365a | 1.789 ms · 49104B · 3621a | 1.782 ms · 23448B · 2034a | — | 924 µs · 17473B · 1708a |
| spectral-norm | 538.7 µs · 648B · 6a | 8.39 µs · 648B · 6a | 64.2× | — | 7.061 µs · 576B · 3a | 2.007 ms · 571371B · 54822a | 1.081 ms · 234784B · 925a | 2.434 ms · 275337B · 33729a | 4.627 ms · 2457133B · 52718a | — | 4.068 ms · 2448022B · 74233a |
| string-build | 721.6 µs · 85408B · 4107a | 493.4 µs · 85408B · 4107a | 1.46× | — | 417.1 µs · 855890B · 5001a | 2.531 ms · 2211272B · 36101a | 2.352 ms · 1078110B · 13859a | 3.142 ms · 1147386B · 26883a | 2.533 ms · 1326244B · 24469a | — | 3.147 ms · 2372785B · 41432a |
| string-compare | 1.09 ms · 21136B · 1284a | 235.7 µs · 21136B · 1284a | 4.62× | — | 63.06 µs · 9632B · 1585a | 1.656 ms · 296282B · 20528a | 1.129 ms · 59711B · 3431a | 2.342 ms · 50744B · 3848a | 1.83 ms · 209520B · 5520a | — | 2.369 ms · 503614B · 31846a |
| struct-tree-walk | 284.4 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.64× | — | 1.878 ns · 0B · 0a | 663.9 µs · 458379B · 5114a | 1.374 ms · 818511B · 11253a | 1.072 ms · 558961B · 6149a | 3.53 ms · 2570661B · 34797a | — | 1.174 µs · 600B · 17a |
| tail-ping-pong | 28.94 µs · 0B · 0a | 480 ns · 0B · 0a | 60.3× | — | 890.8 ns · 0B · 0a | 110.9 µs · 106112B · 2001a | 36.7 µs · 21856B · 85a | 112.7 µs · 13984B · 1726a | 115.6 µs · 14280B · 1731a | — | 535.6 µs · 348834B · 9016a |
| tail-sum | 28.79 µs · 0B · 0a | 685.2 ns · 0B · 0a | 42× | — | 302.6 ns · 0B · 0a | 124.6 µs · 106112B · 2001a | 37.65 µs · 21856B · 85a | 112.5 µs · 13984B · 1726a | 115.9 µs · 14280B · 1731a | — | 84.21 µs · 4608B · 1017a |
| typed-array-sum | 7.313 µs · 0B · 0a | 260.6 ns · 0B · 0a | 28.1× | 266.8 ns · 8B · 1a | 1.875 ns · 0B · 0a | 49.49 µs · 94208B · 513a | 8.409 µs · 4000B · 15a | 27.33 µs · 2080B · 238a | 17.2 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.2 µs · 18424B · 2046a | 1.511 µs · 18424B · 2046a | 74.3× | — | 357.2 ns · 0B · 0a | 105.6 µs · 107608B · 2188a | — | 947.4 µs · 447250B · 12735a | 417.2 µs · 199937B · 7772a | — | 102.1 µs · 13545B · 1317a |

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
