# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.009 µs · 0B · 0a | 8.979 µs · 0B · 0a | 5.409 µs · 0B · 0a | 1.67× | — | 2.459 µs · 1024B · 128a | 44.13 µs · 96289B · 388a | 17.53 µs · 14376B · 256a | 70.53 µs · 78016B · 770a | 13.92 µs · 5712B · 266a | — | 31.23 µs · 2308B · 276a |
| binary-trees | 2.008 ms · 768B · 8a | 1.973 ms · 768B · 8a | 594.5 µs · 768B · 8a | 3.38× | — | 284.3 µs · 201936B · 8414a | 3.826 ms · 1468184B · 62877a | 10.61 ms · 6995691B · 92703a | 8.935 ms · 5198698B · 69052a | 27.66 ms · 19457612B · 280714a | — | 28.2 ms · 15128210B · 446106a |
| branch-tree | 1.339 µs · 0B · 0a | 19.54 ns · 0B · 0a | 85.24 ns · 0B · 0a | 15.7× | 624.7 ns · 16B · 1a | 161.1 ns · 0B · 0a | 46.85 µs · 95384B · 660a | 15.08 µs · 2464B · 9a | 26.44 µs · 1992B · 196a | 24.25 µs · 2168B · 203a | — | 20.84 µs · 1640B · 219a |
| closure-counter | 5.578 µs · 64B · 2a | 5.562 µs · 64B · 2a | 903.8 ns · 64B · 2a | 6.17× | — | 40.49 ns · 0B · 0a | 40.88 µs · 92272B · 261a | 13.17 µs · 151B · 3a | 18.97 µs · 1264B · 13a | 80.62 µs · 58312B · 659a | — | 89.08 µs · 35628B · 922a |
| coroutine-gen | 79.16 µs · 128B · 2a | 79.18 µs · 128B · 2a | 79.16 µs · 128B · 2a | 1× | — | 42.88 µs · 208B · 7a | — | 335.4 µs · 149936B · 649a | 600.8 µs · 330650B · 6719a | 199.1 µs · 91800B · 3646a | — | — |
| fannkuch | 828.7 µs · 34608B · 1442a | 814.2 µs · 34608B · 1442a | 220.7 µs · 34608B · 1442a | 3.75× | — | 35.44 µs · 17280B · 720a | 1.989 ms · 456578B · 25621a | 1.537 ms · 443136B · 1460a | 19.16 ms · 14005332B · 164946a | 3.33 ms · 1367678B · 16944a | — | 4.695 ms · 2501433B · 88439a |
| fnv1a64 | 123.7 µs · 16400B · 2049a | 123.8 µs · 16400B · 2049a | 1.155 µs · 16400B · 2049a | 107× | — | 902.4 ns · 0B · 0a | 161.5 µs · 120808B · 3838a | — | 1.424 ms · 608256B · 18475a | 389.7 µs · 162921B · 7429a | — | 154.4 µs · 17282B · 2074a |
| host-call | 466 µs · 41752B · 3080a | 484.6 µs · 41752B · 3080a | 482.9 µs · 41752B · 3080a | 0.965× | — | 3.312 µs · 0B · 0a | 181.8 µs · 139264B · 3073a | 127.6 µs · 22536B · 1079a | 775.6 µs · 158048B · 14585a | 106 µs · 14920B · 1799a | — | 553.4 µs · 340637B · 9231a |
| i64-wide-fib | 28.35 ms · 2913456B · 364178a | 21.02 ms · 1942304B · 242785a | 679.8 µs · 16B · 1a | 41.7× | — | 238.8 µs · 0B · 0a | 23.75 µs · 90520B · 52a | — | 155 ms · 64193424B · 1942306a | 346.5 ms · 253473104B · 4248843a | — | 123.4 ms · 98686602B · 2138708a |
| indirect-recursive-fib | 1.191 ms · 0B · 0a | 1.185 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.7× | 99.26 µs · 8B · 1a | 35.05 µs · 0B · 0a | 2.061 ms · 319346B · 28655a | 1.938 ms · 704B · 2a | 2.541 ms · 4680B · 39a | 11.59 ms · 10158181B · 109494a | — | 24.11 ms · 13061345B · 394061a |
| iterative-fib | 1.136 µs · 0B · 0a | 1.069 µs · 0B · 0a | 82.78 ns · 0B · 0a | 13.7× | 84.29 ns · 8B · 1a | 12.42 ns · 0B · 0a | 25.21 µs · 90592B · 61a | 1.144 µs · 160B · 0a | 4.223 µs · 368B · 20a | 5.965 µs · 2448B · 88a | — | 6.2 µs · 944B · 81a |
| mandelbrot | 293 µs · 0B · 0a | 248 µs · 0B · 0a | 11.21 µs · 0B · 0a | 26.1× | — | 6.326 µs · 0B · 0a | 934.2 µs · 292345B · 25008a | 485.4 µs · 169408B · 661a | 8.161 ms · 5558653B · 104191a | 1.474 ms · 324977B · 23643a | — | 1.522 ms · 365396B · 22513a |
| map-ops | 71.26 µs · 8816B · 3a | 71.19 µs · 8816B · 3a | 11.09 µs · 8816B · 3a | 6.43× | — | 11.12 µs · 4904B · 3a | — | 280.4 µs · 114327B · 198a | 379.3 µs · 66728B · 4078a | — | — | 1.109 ms · 501516B · 15813a |
| mat-mul | 375.1 µs · 6216B · 6a | 374 µs · 6216B · 6a | 8.976 µs · 6216B · 6a | 41.8× | — | 5.697 µs · 6144B · 3a | 1.34 ms · 431714B · 34871a | 669.9 µs · 143168B · 478a | 1.206 ms · 61304B · 537a | 1.46 ms · 90712B · 9350a | — | 917.1 µs · 36614B · 6036a |
| n-body | 613.7 µs · 504B · 14a | 585.5 µs · 504B · 14a | 19.11 µs · 504B · 14a | 32.1× | — | 7.356 µs · 0B · 0a | 1.479 ms · 386898B · 35892a | 1.235 ms · 258913B · 2002a | 3.336 ms · 658435B · 81545a | 2.442 ms · 382717B · 34975a | — | 1.747 ms · 445459B · 20669a |
| n-queens | 431.3 µs · 120B · 6a | 431.4 µs · 120B · 6a | 31.69 µs · 120B · 6a | 13.6× | — | 6.018 µs · 0B · 0a | 933.3 µs · 234865B · 17784a | 566.2 µs · 14423B · 63a | 1.102 ms · 7424B · 35a | 1.391 ms · 410345B · 5147a | — | 2.012 ms · 766002B · 33371a |
| naive-checksum | 252.6 µs · 0B · 0a | 142.6 µs · 0B · 0a | 8.774 µs · 0B · 0a | 28.8× | — | 2.758 µs · 0B · 0a | 601 µs · 235753B · 18206a | 491.1 µs · 174768B · 682a | 850.3 µs · 155640B · 19402a | 755.6 µs · 163816B · 20409a | — | 311.8 µs · 8977B · 2071a |
| permutation-flips | 128.9 µs · 7680B · 128a | 128.6 µs · 7680B · 128a | 17.13 µs · 7680B · 128a | 7.52× | — | 1.879 ns · 0B · 0a | 568 µs · 292857B · 9856a | 234.9 µs · 78808B · 451a | 521.8 µs · 122504B · 765a | 486.9 µs · 111976B · 2368a | — | 991.6 ns · 528B · 13a |
| recursive-fib-20 | 713.7 µs · 0B · 0a | 718.6 µs · 0B · 0a | 47.36 µs · 0B · 0a | 15.1× | 42.75 µs · 8B · 1a | 21.45 µs · 0B · 0a | 1.915 ms · 319345B · 28655a | 2.188 ms · 704B · 2a | 2.982 ms · 4680B · 39a | 11.81 ms · 9807912B · 109494a | — | 10.53 ms · 8302710B · 192848a |
| recursive-fib-35 | 970.5 ms · 0B · 0a | 989.7 ms · 0B · 0a | 63.92 ms · 0B · 0a | 15.2× | 58.27 ms · 8B · 1a | 29.06 ms · 0B · 0a | 2.555 s · 312797328B · 39088167a | 2.953 s · 971008B · 3793a | 4.06 s · 375376B · 46373a | 15.98 s · 13378019120B · 149349993a | — | — |
| sieve | 20.4 µs · 1048B · 2a | 20.46 µs · 1048B · 2a | 973.3 ns · 1048B · 2a | 21× | 1.023 µs · 8B · 1a | 372 ns · 0B · 0a | 139.4 µs · 122504B · 1611a | 50.79 µs · 18416B · 44a | 83.82 µs · 1872B · 25a | 76.85 µs · 5704B · 30a | — | 81.27 µs · 4368B · 795a |
| sort-stress | 506.5 µs · 5136B · 512a | 508.2 µs · 5136B · 512a | 12.74 µs · 5136B · 512a | 39.7× | — | 5.391 µs · 1024B · 2a | 1.269 ms · 270945B · 19965a | 846.8 µs · 36568B · 365a | 1.802 ms · 49104B · 3621a | 1.784 ms · 23448B · 2034a | — | 928.3 µs · 17475B · 1708a |
| spectral-norm | 544.5 µs · 648B · 6a | 461.5 µs · 648B · 6a | 8.419 µs · 648B · 6a | 64.7× | — | 7.056 µs · 576B · 3a | 2.035 ms · 571370B · 54822a | 1.082 ms · 234784B · 925a | 2.45 ms · 275337B · 33729a | 4.732 ms · 2457137B · 52718a | — | 4.117 ms · 2448012B · 74233a |
| string-build | 723.7 µs · 85408B · 4107a | 717.2 µs · 85408B · 4107a | 603.6 µs · 85408B · 4107a | 1.2× | — | 386.5 µs · 855890B · 5001a | 2.488 ms · 2211276B · 36101a | 2.385 ms · 1078111B · 13859a | 3.153 ms · 1147383B · 26883a | 2.493 ms · 1326243B · 24469a | — | 3.125 ms · 2372791B · 41432a |
| string-compare | 1.087 ms · 21136B · 1284a | 1.089 ms · 21136B · 1284a | 222.1 µs · 21136B · 1284a | 4.9× | — | 63.71 µs · 9632B · 1585a | 1.663 ms · 296282B · 20528a | 1.137 ms · 59712B · 3431a | 2.331 ms · 50744B · 3848a | 1.825 ms · 209521B · 5520a | — | 2.367 ms · 503612B · 31846a |
| struct-tree-walk | 284.9 µs · 768B · 8a | 284.6 µs · 768B · 8a | 108 µs · 768B · 8a | 2.64× | — | 1.898 ns · 0B · 0a | 659.1 µs · 458378B · 5114a | 1.395 ms · 818512B · 11253a | 1.087 ms · 558961B · 6149a | 3.52 ms · 2570662B · 34797a | — | 1.157 µs · 600B · 17a |
| tail-ping-pong | 29.48 µs · 0B · 0a | 29.41 µs · 0B · 0a | 478.4 ns · 0B · 0a | 61.6× | — | 890.7 ns · 0B · 0a | 116.6 µs · 106112B · 2001a | 36.1 µs · 21856B · 85a | 109 µs · 13984B · 1726a | 116.9 µs · 14280B · 1731a | — | 543.8 µs · 348835B · 9016a |
| tail-sum | 29.37 µs · 0B · 0a | 29.56 µs · 0B · 0a | 682.3 ns · 0B · 0a | 43× | — | 302.7 ns · 0B · 0a | 110.4 µs · 106112B · 2001a | 36.83 µs · 21856B · 85a | 108.7 µs · 13984B · 1726a | 117.3 µs · 14280B · 1731a | — | 86.27 µs · 4608B · 1017a |
| typed-array-sum | 7.321 µs · 0B · 0a | 7.444 µs · 0B · 0a | 267.8 ns · 0B · 0a | 27.3× | 264.6 ns · 8B · 1a | 1.893 ns · 0B · 0a | 48.62 µs · 94208B · 513a | 8.409 µs · 4000B · 15a | 27.49 µs · 2080B · 238a | 17.13 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.9 µs · 18424B · 2046a | 113.2 µs · 18424B · 2046a | 1.411 µs · 18424B · 2046a | 80× | — | 358.2 ns · 0B · 0a | 106.5 µs · 107608B · 2188a | — | 940.2 µs · 447252B · 12735a | 424 µs · 199937B · 7772a | — | 101.9 µs · 13545B · 1317a |

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
