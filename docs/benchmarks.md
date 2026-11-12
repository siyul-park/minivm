# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.934 µs · 0B · 0a | 5.436 µs · 0B · 0a | 1.64× | — | 2.113 µs · 1024B · 128a | 39.47 µs · 96288B · 388a | 17.89 µs · 14376B · 256a | 71.89 µs · 78017B · 770a | 13.71 µs · 5712B · 266a | — | 30.8 µs · 2308B · 276a |
| binary-trees | 1.961 ms · 768B · 8a | 591.1 µs · 768B · 8a | 3.32× | — | 279.7 µs · 201936B · 8414a | 3.759 ms · 1468183B · 62877a | 10.35 ms · 6995672B · 92703a | 8.866 ms · 5198694B · 69052a | 29.27 ms · 19457610B · 280714a | — | 29.37 ms · 15128184B · 446106a |
| branch-tree | 1.326 µs · 0B · 0a | 153 ns · 0B · 0a | 8.67× | 625.1 ns · 16B · 1a | 160.5 ns · 0B · 0a | 46.71 µs · 95384B · 660a | 15.09 µs · 2464B · 9a | 26.13 µs · 1992B · 196a | 23.79 µs · 2168B · 203a | — | 20.56 µs · 1640B · 219a |
| closure-counter | 5.514 µs · 64B · 2a | 902.8 ns · 64B · 2a | 6.11× | — | 40.38 ns · 0B · 0a | 40.28 µs · 92272B · 261a | 13.03 µs · 151B · 3a | 18.98 µs · 1264B · 13a | 77.83 µs · 58312B · 659a | — | 87.38 µs · 35629B · 922a |
| coroutine-gen | 79.75 µs · 128B · 2a | 79.58 µs · 128B · 2a | 1× | — | 42.29 µs · 208B · 7a | — | 333.1 µs · 149936B · 649a | 596.5 µs · 330650B · 6719a | 197.1 µs · 91800B · 3646a | — | — |
| fannkuch | 831.2 µs · 34608B · 1442a | 220.9 µs · 34608B · 1442a | 3.76× | — | 35.41 µs · 17280B · 720a | 1.97 ms · 456578B · 25621a | 1.558 ms · 443135B · 1460a | 19.1 ms · 14005357B · 164946a | 3.308 ms · 1367677B · 16944a | — | 4.655 ms · 2501447B · 88439a |
| fnv1a64 | 123.2 µs · 16400B · 2049a | 1.15 µs · 16400B · 2049a | 107× | — | 902.3 ns · 0B · 0a | 165.8 µs · 120808B · 3838a | — | 1.475 ms · 608262B · 18475a | 372.1 µs · 162921B · 7429a | — | 153.7 µs · 17282B · 2074a |
| host-call | 692 µs · 196592B · 14073a | 676.8 µs · 196592B · 14073a | 1.02× | — | 3.311 µs · 0B · 0a | 180.2 µs · 139264B · 3073a | 128.6 µs · 22536B · 1079a | 751.6 µs · 158048B · 14585a | 106.6 µs · 14920B · 1799a | — | 549.1 µs · 340636B · 9231a |
| i64-wide-fib | 28.08 ms · 2913456B · 364178a | 681.1 µs · 16B · 1a | 41.2× | — | 239.3 µs · 0B · 0a | 23.04 µs · 90520B · 52a | — | 156.9 ms · 64193296B · 1942305a | 338.1 ms · 253472720B · 4248840a | — | 128.3 ms · 98686640B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 60.51 µs · 0B · 0a | 19.6× | 99.09 µs · 8B · 1a | 35.15 µs · 0B · 0a | 2.045 ms · 319345B · 28655a | 1.954 ms · 703B · 2a | 2.465 ms · 4680B · 39a | 11.16 ms · 10158185B · 109494a | — | 23.64 ms · 13061296B · 394061a |
| iterative-fib | 1.115 µs · 0B · 0a | 85.11 ns · 0B · 0a | 13.1× | 84.33 ns · 8B · 1a | 12.47 ns · 0B · 0a | 23.7 µs · 90592B · 61a | 1.159 µs · 159B · 0a | 4.22 µs · 368B · 20a | 5.896 µs · 2448B · 88a | — | 6.231 µs · 944B · 81a |
| mandelbrot | 293.3 µs · 0B · 0a | 10.95 µs · 0B · 0a | 26.8× | — | 6.344 µs · 0B · 0a | 910 µs · 292345B · 25008a | 483.8 µs · 169408B · 661a | 7.865 ms · 5558651B · 104191a | 1.455 ms · 324978B · 23643a | — | 1.529 ms · 365402B · 22513a |
| map-ops | 70.23 µs · 8816B · 3a | 11.23 µs · 8816B · 3a | 6.25× | — | 10.96 µs · 4904B · 3a | — | 280.8 µs · 114327B · 198a | 372.4 µs · 66728B · 4078a | — | — | 1.089 ms · 501521B · 15813a |
| mat-mul | 375 µs · 6216B · 6a | 8.815 µs · 6216B · 6a | 42.5× | — | 5.64 µs · 6144B · 3a | 1.298 ms · 431714B · 34871a | 674 µs · 143168B · 478a | 1.214 ms · 61304B · 537a | 1.455 ms · 90712B · 9350a | — | 921.7 µs · 36613B · 6036a |
| n-body | 610.7 µs · 504B · 14a | 18.8 µs · 504B · 14a | 32.5× | — | 7.359 µs · 0B · 0a | 1.444 ms · 386898B · 35892a | 1.195 ms · 258913B · 2002a | 3.2 ms · 658435B · 81545a | 2.37 ms · 382714B · 34975a | — | 1.739 ms · 445464B · 20669a |
| n-queens | 432.1 µs · 120B · 6a | 31.58 µs · 120B · 6a | 13.7× | — | 6.011 µs · 0B · 0a | 918 µs · 234865B · 17784a | 571.7 µs · 14423B · 63a | 1.09 ms · 7424B · 35a | 1.379 ms · 410345B · 5147a | — | 1.992 ms · 766004B · 33371a |
| permutation-flips | 127.7 µs · 7680B · 128a | 16.49 µs · 7680B · 128a | 7.74× | — | 1.875 ns · 0B · 0a | 543.8 µs · 292858B · 9856a | 230.6 µs · 78808B · 451a | 518.3 µs · 122504B · 765a | 482.2 µs · 111976B · 2368a | — | 988.9 ns · 528B · 13a |
| recursive-fib-20 | 720.9 µs · 0B · 0a | 47.43 µs · 0B · 0a | 15.2× | 42.77 µs · 8B · 1a | 21.53 µs · 0B · 0a | 1.898 ms · 319345B · 28655a | 2.225 ms · 704B · 2a | 2.953 ms · 4680B · 39a | 11.48 ms · 9807926B · 109494a | — | 10.55 ms · 8302658B · 192847a |
| recursive-fib-35 | 977.5 ms · 0B · 0a | 63.84 ms · 0B · 0a | 15.3× | 58.22 ms · 8B · 1a | 29.15 ms · 0B · 0a | 2.547 s · 312797344B · 39088168a | 2.973 s · 971008B · 3793a | 4.049 s · 375360B · 46373a | 15.44 s · 13378017904B · 149349960a | — | — |
| sieve | 20.07 µs · 1048B · 2a | 963.3 ns · 1048B · 2a | 20.8× | 1.032 µs · 8B · 1a | 372 ns · 0B · 0a | 138.1 µs · 122504B · 1611a | 51 µs · 18416B · 44a | 83.85 µs · 1872B · 25a | 77.29 µs · 5704B · 30a | — | 80.69 µs · 4368B · 795a |
| sort-stress | 504 µs · 5136B · 512a | 12.64 µs · 5136B · 512a | 39.9× | — | 5.306 µs · 1024B · 2a | 1.25 ms · 270945B · 19965a | 852.6 µs · 36567B · 365a | 1.787 ms · 49104B · 3621a | 1.771 ms · 23448B · 2034a | — | 925.9 µs · 17474B · 1708a |
| spectral-norm | 533.7 µs · 648B · 6a | 8.383 µs · 648B · 6a | 63.7× | — | 7.035 µs · 576B · 3a | 1.986 ms · 571371B · 54822a | 1.098 ms · 234784B · 925a | 2.419 ms · 275337B · 33729a | 4.567 ms · 2457136B · 52718a | — | 4.018 ms · 2448010B · 74233a |
| string-build | 723.3 µs · 85408B · 4107a | 592.4 µs · 85408B · 4107a | 1.22× | — | 369.4 µs · 855890B · 5001a | 2.397 ms · 2211274B · 36101a | 2.346 ms · 1078111B · 13859a | 3.118 ms · 1147384B · 26883a | 2.463 ms · 1326242B · 24469a | — | 3.069 ms · 2372787B · 41432a |
| string-compare | 1.081 ms · 21136B · 1284a | 236.8 µs · 21136B · 1284a | 4.57× | — | 62.44 µs · 9632B · 1585a | 1.651 ms · 296281B · 20528a | 1.132 ms · 59712B · 3431a | 2.333 ms · 50744B · 3848a | 1.828 ms · 209521B · 5520a | — | 2.356 ms · 503615B · 31846a |
| struct-tree-walk | 283.9 µs · 768B · 8a | 107.2 µs · 768B · 8a | 2.65× | — | 1.874 ns · 0B · 0a | 653.9 µs · 458379B · 5114a | 1.329 ms · 818512B · 11253a | 1.071 ms · 558961B · 6149a | 3.447 ms · 2570662B · 34797a | — | 1.152 µs · 600B · 17a |
| tail-ping-pong | 28.95 µs · 0B · 0a | 483.1 ns · 0B · 0a | 59.9× | — | 890.8 ns · 0B · 0a | 109 µs · 106112B · 2001a | 36.34 µs · 21856B · 85a | 111.5 µs · 13984B · 1726a | 117.4 µs · 14280B · 1731a | — | 522.2 µs · 348833B · 9016a |
| tail-sum | 28.89 µs · 0B · 0a | 685.3 ns · 0B · 0a | 42.2× | — | 302.6 ns · 0B · 0a | 109.6 µs · 106112B · 2001a | 35.82 µs · 21856B · 85a | 111 µs · 13984B · 1726a | 117.1 µs · 14280B · 1731a | — | 84 µs · 4608B · 1017a |
| typed-array-sum | 7.26 µs · 0B · 0a | 268 ns · 0B · 0a | 27.1× | 260.4 ns · 8B · 1a | 1.874 ns · 0B · 0a | 46.2 µs · 94208B · 513a | 9.063 µs · 3999B · 15a | 27.32 µs · 2080B · 238a | 17.27 µs · 2496B · 246a | — | — |
| xorshift-i64 | 108.5 µs · 18424B · 2046a | 1.455 µs · 18424B · 2046a | 74.5× | — | 356.7 ns · 0B · 0a | 104.3 µs · 107608B · 2188a | — | 923.5 µs · 447253B · 12736a | 406.5 µs · 199938B · 7772a | — | 101.8 µs · 13545B · 1317a |

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
