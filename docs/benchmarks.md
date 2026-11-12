# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.91 µs · 0B · 0a | 5.359 µs · 0B · 0a | 1.66× | — | 2.232 µs · 1024B · 128a | 37.18 µs · 96288B · 388a | 17.24 µs · 14376B · 256a | 69.31 µs · 78016B · 770a | 14.55 µs · 5712B · 266a | — | 30.8 µs · 2308B · 276a |
| binary-trees | 1.951 ms · 768B · 8a | 589.9 µs · 768B · 8a | 3.31× | — | 288.7 µs · 201936B · 8414a | 3.831 ms · 1468182B · 62877a | 10.53 ms · 6995687B · 92703a | 8.909 ms · 5198697B · 69052a | 27.84 ms · 19457632B · 280714a | — | 27.81 ms · 15128181B · 446106a |
| branch-tree | 1.358 µs · 0B · 0a | 159.8 ns · 0B · 0a | 8.5× | 627.1 ns · 16B · 1a | 160.5 ns · 0B · 0a | 45.7 µs · 95385B · 660a | 15.26 µs · 2463B · 9a | 26.51 µs · 1992B · 196a | 23.92 µs · 2168B · 203a | — | 20.72 µs · 1640B · 219a |
| closure-counter | 5.511 µs · 64B · 2a | 911.4 ns · 64B · 2a | 6.05× | — | 40.35 ns · 0B · 0a | 41.61 µs · 92272B · 261a | 13.07 µs · 152B · 3a | 18.98 µs · 1264B · 13a | 79.14 µs · 58312B · 659a | — | 90.26 µs · 35629B · 922a |
| coroutine-gen | 77.49 µs · 128B · 2a | 77.2 µs · 128B · 2a | 1× | — | 43.03 µs · 208B · 7a | — | 330.1 µs · 149936B · 649a | 599.8 µs · 330649B · 6719a | 200.8 µs · 91800B · 3646a | — | — |
| fannkuch | 833.8 µs · 34608B · 1442a | 224.6 µs · 34608B · 1442a | 3.71× | — | 35.61 µs · 17280B · 720a | 2.004 ms · 456578B · 25621a | 1.544 ms · 443135B · 1460a | 19.33 ms · 14005359B · 164946a | 3.344 ms · 1367678B · 16944a | — | 4.739 ms · 2501435B · 88439a |
| fnv1a64 | 119.9 µs · 16400B · 2049a | 1.153 µs · 16400B · 2049a | 104× | — | 902.6 ns · 0B · 0a | 163.9 µs · 120808B · 3838a | — | 1.448 ms · 608246B · 18475a | 389.8 µs · 162921B · 7429a | — | 154.4 µs · 17282B · 2074a |
| host-call | 724 µs · 196592B · 14073a | 702.9 µs · 196592B · 14073a | 1.03× | — | 3.311 µs · 0B · 0a | 181 µs · 139264B · 3073a | 131.7 µs · 22536B · 1079a | 807.7 µs · 158048B · 14585a | 105 µs · 14920B · 1799a | — | 578.8 µs · 340636B · 9231a |
| i64-wide-fib | 28.63 ms · 2913456B · 364178a | 680.1 µs · 16B · 1a | 42.1× | — | 239.5 µs · 0B · 0a | 23.67 µs · 90520B · 52a | — | 161.7 ms · 64193312B · 1942305a | 342.2 ms · 253472928B · 4248843a | — | 129.2 ms · 98686613B · 2138708a |
| indirect-recursive-fib | 1.191 ms · 0B · 0a | 60.46 µs · 0B · 0a | 19.7× | 99.07 µs · 8B · 1a | 34.76 µs · 0B · 0a | 2.101 ms · 319346B · 28655a | 1.941 ms · 704B · 2a | 2.571 ms · 4680B · 39a | 11.16 ms · 10158199B · 109494a | — | 24.25 ms · 13061316B · 394061a |
| iterative-fib | 1.112 µs · 0B · 0a | 78.26 ns · 0B · 0a | 14.2× | 85.13 ns · 8B · 1a | 12.45 ns · 0B · 0a | 24.11 µs · 90592B · 61a | 1.161 µs · 160B · 0a | 4.234 µs · 368B · 20a | 6.179 µs · 2448B · 88a | — | 6.163 µs · 944B · 81a |
| mandelbrot | 293.8 µs · 0B · 0a | 10.91 µs · 0B · 0a | 26.9× | — | 6.326 µs · 0B · 0a | 963.7 µs · 292345B · 25008a | 484 µs · 169408B · 661a | 8.039 ms · 5558657B · 104191a | 1.502 ms · 324978B · 23643a | — | 1.518 ms · 365399B · 22513a |
| map-ops | 70 µs · 8816B · 3a | 70.06 µs · 8816B · 3a | 0.999× | — | 10.8 µs · 4904B · 3a | — | 278.6 µs · 114327B · 198a | 379.8 µs · 66728B · 4078a | — | — | 1.113 ms · 501521B · 15813a |
| mat-mul | 368.9 µs · 6216B · 6a | 8.479 µs · 6216B · 6a | 43.5× | — | 5.402 µs · 6144B · 3a | 1.37 ms · 431714B · 34871a | 669 µs · 143168B · 478a | 1.2 ms · 61304B · 537a | 1.442 ms · 90712B · 9350a | — | 917.9 µs · 36613B · 6036a |
| n-body | 611.9 µs · 504B · 14a | 18.82 µs · 504B · 14a | 32.5× | — | 7.357 µs · 0B · 0a | 1.505 ms · 386898B · 35892a | 1.225 ms · 258913B · 2002a | 3.364 ms · 658435B · 81545a | 2.419 ms · 382714B · 34975a | — | 1.756 ms · 445463B · 20669a |
| n-queens | 432.4 µs · 120B · 6a | 31.63 µs · 120B · 6a | 13.7× | — | 6.019 µs · 0B · 0a | 957.8 µs · 234865B · 17784a | 576 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.454 ms · 410345B · 5147a | — | 2.008 ms · 766013B · 33371a |
| permutation-flips | 127.6 µs · 7680B · 128a | 17.53 µs · 7680B · 128a | 7.28× | — | 1.874 ns · 0B · 0a | 552.2 µs · 292858B · 9856a | 235.4 µs · 78808B · 451a | 525.8 µs · 122504B · 765a | 495.2 µs · 111976B · 2368a | — | 992.9 ns · 528B · 13a |
| recursive-fib-20 | 713.5 µs · 0B · 0a | 47.17 µs · 0B · 0a | 15.1× | 42.75 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.965 ms · 319346B · 28655a | 2.17 ms · 704B · 2a | 2.968 ms · 4680B · 39a | 11.27 ms · 9807908B · 109494a | — | 11.05 ms · 8302716B · 192848a |
| recursive-fib-35 | 968.2 ms · 0B · 0a | 64.1 ms · 0B · 0a | 15.1× | 58.17 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.584 s · 312797376B · 39088168a | 2.957 s · 971008B · 3793a | 4.029 s · 375360B · 46373a | 15.51 s · 13378018048B · 149349972a | — | — |
| sieve | 20.07 µs · 1048B · 2a | 944.2 ns · 1048B · 2a | 21.3× | 1.024 µs · 8B · 1a | 372 ns · 0B · 0a | 139 µs · 122504B · 1611a | 50.44 µs · 18415B · 44a | 84.18 µs · 1872B · 25a | 77.38 µs · 5704B · 30a | — | 81.51 µs · 4368B · 795a |
| sort-stress | 506.8 µs · 5136B · 512a | 12.58 µs · 5136B · 512a | 40.3× | — | 5.399 µs · 1024B · 2a | 1.282 ms · 270945B · 19965a | 844.8 µs · 36567B · 365a | 1.787 ms · 49104B · 3621a | 1.78 ms · 23448B · 2034a | — | 925.6 µs · 17474B · 1708a |
| spectral-norm | 531.1 µs · 648B · 6a | 8.332 µs · 648B · 6a | 63.7× | — | 7.029 µs · 576B · 3a | 2.058 ms · 571370B · 54822a | 1.088 ms · 234784B · 925a | 2.446 ms · 275337B · 33729a | 4.706 ms · 2457134B · 52718a | — | 4.214 ms · 2448017B · 74233a |
| string-build | 724.4 µs · 85408B · 4107a | 598 µs · 85408B · 4107a | 1.21× | — | 365.5 µs · 855891B · 5001a | 2.417 ms · 2211271B · 36101a | 2.308 ms · 1078110B · 13859a | 3.149 ms · 1147383B · 26883a | 2.468 ms · 1326247B · 24469a | — | 3.105 ms · 2372780B · 41432a |
| string-compare | 1.083 ms · 21136B · 1284a | 234.5 µs · 21136B · 1284a | 4.62× | — | 63.69 µs · 9632B · 1585a | 1.692 ms · 296281B · 20528a | 1.139 ms · 59712B · 3431a | 2.323 ms · 50744B · 3848a | 1.81 ms · 209522B · 5520a | — | 2.371 ms · 503615B · 31846a |
| struct-tree-walk | 280.1 µs · 768B · 8a | 107.4 µs · 768B · 8a | 2.61× | — | 1.875 ns · 0B · 0a | 652.4 µs · 458379B · 5114a | 1.37 ms · 818512B · 11253a | 1.079 ms · 558960B · 6149a | 3.519 ms · 2570665B · 34797a | — | 1.212 µs · 600B · 17a |
| tail-ping-pong | 28.86 µs · 0B · 0a | 480.2 ns · 0B · 0a | 60.1× | — | 891.1 ns · 0B · 0a | 108.8 µs · 106112B · 2001a | 35.62 µs · 21856B · 85a | 113 µs · 13984B · 1726a | 117.8 µs · 14280B · 1731a | — | 558.8 µs · 348833B · 9016a |
| tail-sum | 28.7 µs · 0B · 0a | 674 ns · 0B · 0a | 42.6× | — | 302.5 ns · 0B · 0a | 108.8 µs · 106112B · 2001a | 35.63 µs · 21856B · 85a | 113.2 µs · 13984B · 1726a | 117.4 µs · 14280B · 1731a | — | 84.51 µs · 4608B · 1017a |
| typed-array-sum | 7.259 µs · 0B · 0a | 262.3 ns · 0B · 0a | 27.7× | 264.8 ns · 8B · 1a | 1.876 ns · 0B · 0a | 44.12 µs · 94208B · 513a | 8.299 µs · 3999B · 15a | 27.37 µs · 2080B · 238a | 17.43 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.6 µs · 18424B · 2046a | 1.365 µs · 18424B · 2046a | 81.8× | — | 356.2 ns · 0B · 0a | 101.9 µs · 107608B · 2188a | — | 1.006 ms · 447250B · 12735a | 428.1 µs · 199938B · 7772a | — | 102.6 µs · 13546B · 1317a |

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
