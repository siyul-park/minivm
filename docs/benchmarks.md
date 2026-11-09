# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.079 µs · 0B · 0a | 5.342 µs · 0B · 0a | 1.7× | — | 2.185 µs · 1024B · 128a | 39.25 µs · 96288B · 388a | 17.78 µs · 14376B · 256a | 77.95 µs · 78016B · 770a | 15.3 µs · 5712B · 266a | — | 30.96 µs · 2308B · 276a |
| binary-trees | 1.973 ms · 768B · 8a | 600.1 µs · 768B · 8a | 3.29× | — | 291.7 µs · 201936B · 8414a | 3.878 ms · 1468183B · 62877a | 10.81 ms · 6995678B · 92703a | 8.935 ms · 5198702B · 69052a | 28.87 ms · 19457601B · 280714a | — | 28.26 ms · 15128197B · 446106a |
| branch-tree | 1.302 µs · 0B · 0a | 138.8 ns · 0B · 0a | 9.38× | 627.2 ns · 16B · 1a | 160.5 ns · 0B · 0a | 50.62 µs · 95384B · 660a | 15.48 µs · 2464B · 9a | 26.45 µs · 1992B · 196a | 24.03 µs · 2168B · 203a | — | 20.54 µs · 1640B · 219a |
| closure-counter | 5.457 µs · 64B · 2a | 913.6 ns · 64B · 2a | 5.97× | — | 39.89 ns · 0B · 0a | 42.61 µs · 92273B · 261a | 12.85 µs · 151B · 3a | 19.11 µs · 1264B · 13a | 79.54 µs · 58312B · 659a | — | 89.7 µs · 35628B · 922a |
| fannkuch | 825.5 µs · 34608B · 1442a | 266 µs · 34608B · 1442a | 3.1× | — | 35.22 µs · 17280B · 720a | 1.98 ms · 456579B · 25621a | 1.568 ms · 443136B · 1460a | 19.88 ms · 14005352B · 164946a | 3.257 ms · 1367679B · 16944a | — | 4.732 ms · 2501422B · 88439a |
| fnv1a64 | 120.9 µs · 16400B · 2049a | 1.145 µs · 16400B · 2049a | 106× | — | 902.9 ns · 0B · 0a | 164.6 µs · 120808B · 3838a | — | 1.467 ms · 608251B · 18475a | 397.1 µs · 162921B · 7429a | — | 154.3 µs · 17282B · 2074a |
| i64-wide-fib | 28.07 ms · 2913456B · 364178a | 679.6 µs · 16B · 1a | 41.3× | — | 240 µs · 0B · 0a | 24.85 µs · 90520B · 52a | — | 153.9 ms · 64193392B · 1942306a | 356.8 ms · 253472512B · 4248839a | — | 122.6 ms · 98686640B · 2138708a |
| indirect-recursive-fib | 1.183 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.6× | 98.78 µs · 8B · 1a | 34.85 µs · 0B · 0a | 2.06 ms · 319346B · 28655a | 1.927 ms · 704B · 2a | 2.552 ms · 4680B · 39a | 11.95 ms · 10158189B · 109494a | — | 24.07 ms · 13061283B · 394061a |
| iterative-fib | 1.104 µs · 0B · 0a | 71.42 ns · 0B · 0a | 15.5× | 85.55 ns · 8B · 1a | 11.88 ns · 0B · 0a | 25.52 µs · 90592B · 61a | 1.163 µs · 160B · 0a | 4.253 µs · 368B · 20a | 6.119 µs · 2448B · 88a | — | 6.17 µs · 944B · 81a |
| mandelbrot | 290.6 µs · 0B · 0a | 10.94 µs · 0B · 0a | 26.6× | — | 6.321 µs · 0B · 0a | 962 µs · 292345B · 25008a | 497.3 µs · 169408B · 661a | 8.41 ms · 5558653B · 104191a | 1.523 ms · 324978B · 23643a | — | 1.537 ms · 365406B · 22513a |
| mat-mul | 369.1 µs · 6216B · 6a | 8.972 µs · 6216B · 6a | 41.1× | — | 5.96 µs · 6144B · 3a | 1.373 ms · 431714B · 34871a | 681.4 µs · 143168B · 478a | 1.215 ms · 61304B · 537a | 1.469 ms · 90712B · 9350a | — | 917.2 µs · 36615B · 6036a |
| n-body | 608.2 µs · 504B · 14a | 18.93 µs · 504B · 14a | 32.1× | — | 7.346 µs · 0B · 0a | 1.529 ms · 386899B · 35892a | 1.242 ms · 258914B · 2002a | 3.28 ms · 658435B · 81545a | 2.454 ms · 382714B · 34975a | — | 1.779 ms · 445457B · 20669a |
| n-queens | 429 µs · 120B · 6a | 31.47 µs · 120B · 6a | 13.6× | — | 6.009 µs · 0B · 0a | 956.5 µs · 234865B · 17784a | 573.5 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.442 ms · 410346B · 5147a | — | 2.019 ms · 765999B · 33371a |
| permutation-flips | 129 µs · 7680B · 128a | 18.01 µs · 7680B · 128a | 7.16× | — | 1.878 ns · 0B · 0a | 580.2 µs · 292857B · 9856a | 240.3 µs · 78808B · 451a | 542.5 µs · 122504B · 765a | 493.4 µs · 111976B · 2368a | — | 1.025 µs · 528B · 13a |
| recursive-fib-20 | 714.4 µs · 0B · 0a | 47.7 µs · 0B · 0a | 15× | 42.74 µs · 8B · 1a | 21.44 µs · 0B · 0a | 1.993 ms · 319345B · 28655a | 2.19 ms · 703B · 2a | 2.964 ms · 4680B · 39a | 11.94 ms · 9807916B · 109494a | — | 10.64 ms · 8302686B · 192847a |
| recursive-fib-35 | 972.5 ms · 0B · 0a | 63.91 ms · 0B · 0a | 15.2× | 58.18 ms · 8B · 1a | 29.09 ms · 0B · 0a | 2.662 s · 312797200B · 39088167a | 3.023 s · 971008B · 3793a | 4.026 s · 375360B · 46373a | 16.24 s · 13378012992B · 149349966a | — | — |
| sieve | 20.06 µs · 1048B · 2a | 989.9 ns · 1048B · 2a | 20.3× | 1.032 µs · 8B · 1a | 352 ns · 0B · 0a | 144.2 µs · 122504B · 1611a | 52.21 µs · 18416B · 44a | 84.31 µs · 1872B · 25a | 78.38 µs · 5704B · 30a | — | 80.92 µs · 4368B · 795a |
| sort-stress | 504.2 µs · 5136B · 512a | 12.79 µs · 5136B · 512a | 39.4× | — | 5.939 µs · 1024B · 2a | 1.298 ms · 270945B · 19965a | 873.2 µs · 36568B · 365a | 1.799 ms · 49104B · 3621a | 1.784 ms · 23448B · 2034a | — | 926.4 µs · 17473B · 1708a |
| spectral-norm | 531.3 µs · 648B · 6a | 8.424 µs · 648B · 6a | 63.1× | — | 7.093 µs · 576B · 3a | 2.063 ms · 571371B · 54822a | 1.101 ms · 234783B · 925a | 2.462 ms · 275338B · 33729a | 4.786 ms · 2457132B · 52718a | — | 4.149 ms · 2448010B · 74233a |
| string-build | 725.4 µs · 85408B · 4107a | 614.8 µs · 85408B · 4107a | 1.18× | — | 465.6 µs · 855889B · 5001a | 2.652 ms · 2211271B · 36101a | 2.377 ms · 1078110B · 13859a | 3.229 ms · 1147384B · 26883a | 2.583 ms · 1326242B · 24469a | — | 3.187 ms · 2372782B · 41432a |
| string-compare | 1.089 ms · 21136B · 1284a | 268 µs · 21136B · 1284a | 4.07× | — | 63.4 µs · 9632B · 1585a | 1.694 ms · 296281B · 20528a | 1.145 ms · 59712B · 3431a | 2.333 ms · 50744B · 3848a | 1.859 ms · 209521B · 5520a | — | 2.367 ms · 503620B · 31846a |
| struct-tree-walk | 282.9 µs · 768B · 8a | 108.4 µs · 768B · 8a | 2.61× | — | 1.877 ns · 0B · 0a | 679.8 µs · 458378B · 5114a | 1.401 ms · 818511B · 11253a | 1.119 ms · 558960B · 6149a | 3.639 ms · 2570668B · 34797a | — | 1.182 µs · 600B · 17a |
| tail-ping-pong | 28.99 µs · 0B · 0a | 472.4 ns · 0B · 0a | 61.4× | — | 890.5 ns · 0B · 0a | 111.2 µs · 106112B · 2001a | 36.96 µs · 21856B · 85a | 113.3 µs · 13984B · 1726a | 119.8 µs · 14280B · 1731a | — | 543.6 µs · 348835B · 9016a |
| tail-sum | 29.18 µs · 0B · 0a | 671.4 ns · 0B · 0a | 43.5× | — | 301.5 ns · 0B · 0a | 112.3 µs · 106112B · 2001a | 37.15 µs · 21856B · 85a | 112.9 µs · 13984B · 1726a | 119.6 µs · 14280B · 1731a | — | 83.79 µs · 4608B · 1017a |
| typed-array-sum | 7.319 µs · 0B · 0a | 266.1 ns · 0B · 0a | 27.5× | 268 ns · 8B · 1a | 1.89 ns · 0B · 0a | 48.59 µs · 94208B · 513a | 8.549 µs · 4000B · 15a | 27.51 µs · 2080B · 238a | 17.46 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.3 µs · 18424B · 2046a | 1.472 µs · 18424B · 2046a | 76.3× | — | 360.4 ns · 0B · 0a | 108.9 µs · 107608B · 2188a | — | 1.01 ms · 447249B · 12735a | 434.2 µs · 199938B · 7772a | — | 102.2 µs · 13546B · 1317a |

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
