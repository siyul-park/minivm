# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.766 µs · 0B · 0a | 5.333 µs · 0B · 0a | 1.64× | — | 2.271 µs · 1024B · 128a | 41.73 µs · 96288B · 388a | 18.5 µs · 14376B · 256a | 68.77 µs · 78016B · 770a | 14.18 µs · 5712B · 266a | — | 30.88 µs · 2308B · 276a |
| binary-trees | 1.952 ms · 768B · 8a | 609.4 µs · 768B · 8a | 3.2× | — | 289.2 µs · 201936B · 8414a | 3.897 ms · 1468185B · 62877a | 10.53 ms · 6995673B · 92703a | 8.852 ms · 5198695B · 69052a | 27.53 ms · 19457592B · 280714a | — | 30.71 ms · 15128164B · 446106a |
| branch-tree | 1.287 µs · 0B · 0a | 141 ns · 0B · 0a | 9.13× | 626.4 ns · 16B · 1a | 160.4 ns · 0B · 0a | 45.72 µs · 95385B · 660a | 15.21 µs · 2464B · 9a | 26.58 µs · 1992B · 196a | 24.05 µs · 2168B · 203a | — | 20.49 µs · 1640B · 219a |
| closure-counter | 5.474 µs · 64B · 2a | 912 ns · 64B · 2a | 6× | — | 40.07 ns · 0B · 0a | 42.73 µs · 92272B · 261a | 12.66 µs · 151B · 3a | 19.34 µs · 1264B · 13a | 79.25 µs · 58312B · 659a | — | 90.34 µs · 35629B · 922a |
| fannkuch | 825.6 µs · 34608B · 1442a | 270.6 µs · 34608B · 1442a | 3.05× | — | 35.04 µs · 17280B · 720a | 2.061 ms · 456579B · 25621a | 1.54 ms · 443136B · 1460a | 19.13 ms · 14005355B · 164946a | 3.346 ms · 1367677B · 16944a | — | 4.873 ms · 2501448B · 88439a |
| fnv1a64 | 124.1 µs · 16400B · 2049a | 1.234 µs · 16400B · 2049a | 101× | — | 902.7 ns · 0B · 0a | 175.4 µs · 120808B · 3838a | — | 1.517 ms · 608250B · 18475a | 397.4 µs · 162921B · 7429a | — | 153.8 µs · 17282B · 2074a |
| i64-wide-fib | 29.07 ms · 2913456B · 364178a | 776.9 µs · 16B · 1a | 37.4× | — | 238.6 µs · 0B · 0a | 22.9 µs · 90520B · 52a | — | 158.2 ms · 64193352B · 1942306a | 343.6 ms · 253472816B · 4248841a | — | 130.6 ms · 98686634B · 2138708a |
| indirect-recursive-fib | 1.185 ms · 0B · 0a | 66.12 µs · 0B · 0a | 17.9× | 100.3 µs · 8B · 1a | 34.76 µs · 0B · 0a | 2.128 ms · 319346B · 28655a | 1.931 ms · 704B · 2a | 2.542 ms · 4680B · 39a | 11.34 ms · 10158177B · 109494a | — | 25.56 ms · 13061337B · 394061a |
| iterative-fib | 1.102 µs · 0B · 0a | 72.62 ns · 0B · 0a | 15.2× | 86.58 ns · 8B · 1a | 11.9 ns · 0B · 0a | 22.91 µs · 90592B · 61a | 1.147 µs · 160B · 0a | 4.256 µs · 368B · 20a | 6.095 µs · 2448B · 88a | — | 6.118 µs · 944B · 81a |
| mandelbrot | 290.7 µs · 0B · 0a | 11.08 µs · 0B · 0a | 26.2× | — | 6.341 µs · 0B · 0a | 968.1 µs · 292346B · 25008a | 479 µs · 169408B · 661a | 7.99 ms · 5558651B · 104191a | 1.498 ms · 324978B · 23643a | — | 1.529 ms · 365403B · 22513a |
| mat-mul | 368.4 µs · 6216B · 6a | 8.437 µs · 6216B · 6a | 43.7× | — | 5.402 µs · 6144B · 3a | 1.377 ms · 431714B · 34871a | 668.5 µs · 143168B · 478a | 1.205 ms · 61304B · 537a | 1.46 ms · 90712B · 9350a | — | 918.1 µs · 36614B · 6036a |
| n-body | 609.9 µs · 504B · 14a | 18.05 µs · 504B · 14a | 33.8× | — | 7.346 µs · 0B · 0a | 1.545 ms · 386898B · 35892a | 1.212 ms · 258913B · 2002a | 3.39 ms · 658435B · 81545a | 2.443 ms · 382714B · 34975a | — | 1.745 ms · 445466B · 20669a |
| n-queens | 428 µs · 120B · 6a | 31.94 µs · 120B · 6a | 13.4× | — | 6.019 µs · 0B · 0a | 972.3 µs · 234865B · 17784a | 570.8 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.421 ms · 410345B · 5147a | — | 2.012 ms · 766004B · 33371a |
| permutation-flips | 127.7 µs · 7680B · 128a | 17.85 µs · 7680B · 128a | 7.15× | — | 1.876 ns · 0B · 0a | 555 µs · 292857B · 9856a | 233.9 µs · 78808B · 451a | 523.9 µs · 122504B · 765a | 496.3 µs · 111976B · 2368a | — | 1.051 µs · 528B · 13a |
| recursive-fib-20 | 721.2 µs · 0B · 0a | 53.33 µs · 0B · 0a | 13.5× | 42.74 µs · 8B · 1a | 21.49 µs · 0B · 0a | 2.009 ms · 319345B · 28655a | 2.159 ms · 704B · 2a | 2.965 ms · 4680B · 39a | 11.15 ms · 9807925B · 109494a | — | 10.89 ms · 8302682B · 192847a |
| recursive-fib-35 | 968.7 ms · 0B · 0a | 72.45 ms · 0B · 0a | 13.4× | 58.21 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.701 s · 312797264B · 39088167a | 2.941 s · 971008B · 3793a | 4.033 s · 375360B · 46373a | 15.49 s · 13378018464B · 149349991a | — | — |
| sieve | 19.92 µs · 1048B · 2a | 951.6 ns · 1048B · 2a | 20.9× | 1.035 µs · 8B · 1a | 350.9 ns · 0B · 0a | 135.7 µs · 122504B · 1611a | 51.96 µs · 18416B · 44a | 83.99 µs · 1872B · 25a | 77.1 µs · 5704B · 30a | — | 80.69 µs · 4368B · 795a |
| sort-stress | 505.1 µs · 5136B · 512a | 12.47 µs · 5136B · 512a | 40.5× | — | 5.786 µs · 1024B · 2a | 1.294 ms · 270945B · 19965a | 854.2 µs · 36568B · 365a | 1.794 ms · 49104B · 3621a | 1.79 ms · 23448B · 2034a | — | 924.1 µs · 17473B · 1708a |
| spectral-norm | 532 µs · 648B · 6a | 33.33 µs · 648B · 6a | 16× | — | 7.045 µs · 576B · 3a | 2.12 ms · 571370B · 54822a | 1.073 ms · 234784B · 925a | 2.458 ms · 275338B · 33729a | 4.612 ms · 2457131B · 52718a | — | 4.31 ms · 2448016B · 74233a |
| string-build | 728.5 µs · 85408B · 4107a | 625.3 µs · 85408B · 4107a | 1.16× | — | 365.3 µs · 855893B · 5001a | 2.367 ms · 2211276B · 36101a | 2.282 ms · 1078109B · 13859a | 3.103 ms · 1147384B · 26883a | 2.449 ms · 1326242B · 24469a | — | 3.108 ms · 2372804B · 41432a |
| string-compare | 1.083 ms · 21136B · 1284a | 271.9 µs · 21136B · 1284a | 3.98× | — | 63.62 µs · 9632B · 1585a | 1.693 ms · 296281B · 20528a | 1.14 ms · 59712B · 3431a | 2.329 ms · 50744B · 3848a | 1.817 ms · 209521B · 5520a | — | 2.418 ms · 503623B · 31846a |
| struct-tree-walk | 282.6 µs · 768B · 8a | 109 µs · 768B · 8a | 2.59× | — | 1.877 ns · 0B · 0a | 641.1 µs · 458379B · 5114a | 1.36 ms · 818513B · 11253a | 1.077 ms · 558960B · 6149a | 3.471 ms · 2570669B · 34797a | — | 1.232 µs · 600B · 17a |
| tail-ping-pong | 29.27 µs · 0B · 0a | 513.4 ns · 0B · 0a | 57× | — | 890.6 ns · 0B · 0a | 106.4 µs · 106112B · 2001a | 36.93 µs · 21856B · 85a | 113.8 µs · 13984B · 1726a | 117.8 µs · 14280B · 1731a | — | 558 µs · 348834B · 9016a |
| tail-sum | 29.27 µs · 0B · 0a | 665.2 ns · 0B · 0a | 44× | — | 302.1 ns · 0B · 0a | 106.3 µs · 106112B · 2001a | 35.38 µs · 21856B · 85a | 114.3 µs · 13984B · 1726a | 118.4 µs · 14280B · 1731a | — | 84.38 µs · 4608B · 1017a |
| typed-array-sum | 7.23 µs · 0B · 0a | 237.8 ns · 0B · 0a | 30.4× | 267.4 ns · 8B · 1a | 1.889 ns · 0B · 0a | 45.76 µs · 94208B · 513a | 8.32 µs · 4000B · 15a | 27.52 µs · 2080B · 238a | 17.5 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.2 µs · 18424B · 2046a | 1.408 µs · 18424B · 2046a | 79.7× | — | 360.2 ns · 0B · 0a | 102.3 µs · 107608B · 2188a | — | 999.6 µs · 447252B · 12735a | 425.2 µs · 199938B · 7772a | — | 101.7 µs · 13546B · 1317a |

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
