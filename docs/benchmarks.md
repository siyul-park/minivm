# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.215 µs · 0B · 0a | 5.345 µs · 0B · 0a | 1.72× | — | 2.203 µs · 1024B · 128a | 36.84 µs · 96289B · 388a | 17.25 µs · 14375B · 256a | 68.41 µs · 78016B · 770a | 14.48 µs · 5712B · 266a | — | 31.17 µs · 2308B · 276a |
| binary-trees | 2.003 ms · 768B · 8a | 592.3 µs · 768B · 8a | 3.38× | — | 290 µs · 201936B · 8414a | 3.87 ms · 1468184B · 62877a | 10.36 ms · 6995672B · 92703a | 8.877 ms · 5198699B · 69052a | 27.33 ms · 19457632B · 280714a | — | 29.68 ms · 15128164B · 446106a |
| branch-tree | 1.3 µs · 0B · 0a | 152.6 ns · 0B · 0a | 8.52× | 627.7 ns · 16B · 1a | 160.4 ns · 0B · 0a | 55.65 µs · 95384B · 660a | 15.84 µs · 2464B · 9a | 26.9 µs · 1992B · 196a | 24.07 µs · 2168B · 203a | — | 20.47 µs · 1640B · 219a |
| closure-counter | 5.538 µs · 64B · 2a | 917.8 ns · 64B · 2a | 6.03× | — | 40.34 ns · 0B · 0a | 39.73 µs · 92272B · 261a | 13 µs · 151B · 3a | 19.25 µs · 1264B · 13a | 77.9 µs · 58312B · 659a | — | 90.88 µs · 35629B · 922a |
| coroutine-gen | 77.81 µs · 128B · 2a | 77.53 µs · 128B · 2a | 1× | — | 42.48 µs · 208B · 7a | — | 332 µs · 149936B · 649a | 596.9 µs · 330649B · 6719a | 204.6 µs · 91800B · 3646a | — | — |
| fannkuch | 828.3 µs · 34608B · 1442a | 229.3 µs · 34608B · 1442a | 3.61× | — | 35.22 µs · 17280B · 720a | 2.023 ms · 456579B · 25621a | 1.533 ms · 443135B · 1460a | 19 ms · 14005357B · 164946a | 3.34 ms · 1367678B · 16944a | — | 4.862 ms · 2501455B · 88439a |
| fnv1a64 | 124.9 µs · 16400B · 2049a | 1.155 µs · 16400B · 2049a | 108× | — | 902.5 ns · 0B · 0a | 160.9 µs · 120808B · 3838a | — | 1.49 ms · 608253B · 18475a | 396.1 µs · 162921B · 7429a | — | 153.7 µs · 17282B · 2074a |
| host-call | 710.2 µs · 196592B · 14073a | 739.4 µs · 196592B · 14073a | 0.96× | — | 3.312 µs · 0B · 0a | 178.2 µs · 139264B · 3073a | 131.4 µs · 22536B · 1079a | 797.8 µs · 158048B · 14585a | 109.1 µs · 14920B · 1799a | — | 592.6 µs · 340638B · 9231a |
| i64-wide-fib | 28.4 ms · 2913456B · 364178a | 682 µs · 16B · 1a | 41.6× | — | 239.4 µs · 0B · 0a | 23.1 µs · 90520B · 52a | — | 158.1 ms · 64193296B · 1942305a | 340.8 ms · 253472928B · 4248843a | — | 131.6 ms · 98686576B · 2138708a |
| indirect-recursive-fib | 1.188 ms · 0B · 0a | 60.49 µs · 0B · 0a | 19.6× | 100.3 µs · 8B · 1a | 35.05 µs · 0B · 0a | 2.108 ms · 319346B · 28655a | 1.944 ms · 704B · 2a | 2.545 ms · 4680B · 39a | 11.11 ms · 10158187B · 109494a | — | 24.91 ms · 13061313B · 394061a |
| iterative-fib | 1.115 µs · 0B · 0a | 84.23 ns · 0B · 0a | 13.2× | 85.51 ns · 8B · 1a | 12.46 ns · 0B · 0a | 24.8 µs · 90592B · 61a | 1.15 µs · 160B · 0a | 4.257 µs · 368B · 20a | 6.091 µs · 2448B · 88a | — | 6.194 µs · 944B · 81a |
| mandelbrot | 290.5 µs · 0B · 0a | 10.91 µs · 0B · 0a | 26.6× | — | 6.318 µs · 0B · 0a | 960.2 µs · 292345B · 25008a | 484.7 µs · 169408B · 661a | 8.236 ms · 5558650B · 104191a | 1.498 ms · 324979B · 23643a | — | 1.533 ms · 365396B · 22513a |
| map-ops | 74.08 µs · 9584B · 5a | 73.89 µs · 9584B · 5a | 1× | — | 11.04 µs · 4904B · 3a | — | 277.3 µs · 114327B · 198a | 385.6 µs · 66728B · 4078a | — | — | 1.153 ms · 501517B · 15813a |
| mat-mul | 374.9 µs · 6216B · 6a | 8.628 µs · 6216B · 6a | 43.5× | — | 5.575 µs · 6144B · 3a | 1.373 ms · 431714B · 34871a | 672.9 µs · 143168B · 478a | 1.208 ms · 61304B · 537a | 1.45 ms · 90712B · 9350a | — | 920.2 µs · 36612B · 6036a |
| n-body | 611.3 µs · 504B · 14a | 18.83 µs · 504B · 14a | 32.5× | — | 7.362 µs · 0B · 0a | 1.496 ms · 386899B · 35892a | 1.211 ms · 258913B · 2002a | 3.364 ms · 658435B · 81545a | 2.468 ms · 382714B · 34975a | — | 1.782 ms · 445465B · 20669a |
| n-queens | 434.3 µs · 120B · 6a | 31.7 µs · 120B · 6a | 13.7× | — | 6.035 µs · 0B · 0a | 966.4 µs · 234865B · 17784a | 572.4 µs · 14423B · 63a | 1.099 ms · 7424B · 35a | 1.438 ms · 410345B · 5147a | — | 2.037 ms · 766013B · 33371a |
| permutation-flips | 129.4 µs · 7680B · 128a | 17.5 µs · 7680B · 128a | 7.4× | — | 1.875 ns · 0B · 0a | 557.8 µs · 292858B · 9856a | 236.7 µs · 78808B · 451a | 530.4 µs · 122504B · 765a | 495.4 µs · 111976B · 2368a | — | 1.038 µs · 528B · 13a |
| recursive-fib-20 | 725.2 µs · 0B · 0a | 47.33 µs · 0B · 0a | 15.3× | 42.73 µs · 8B · 1a | 21.51 µs · 0B · 0a | 1.974 ms · 319346B · 28655a | 2.169 ms · 704B · 2a | 2.952 ms · 4680B · 39a | 11.24 ms · 9807923B · 109494a | — | 10.94 ms · 8302717B · 192848a |
| recursive-fib-35 | 975 ms · 0B · 0a | 64.19 ms · 0B · 0a | 15.2× | 58.26 ms · 8B · 1a | 29.18 ms · 0B · 0a | 2.612 s · 312797392B · 39088167a | 2.957 s · 971008B · 3793a | 4.022 s · 375360B · 46373a | 15.14 s · 13378018000B · 149349974a | — | — |
| sieve | 19.99 µs · 1048B · 2a | 909.7 ns · 1048B · 2a | 22× | 1.027 µs · 8B · 1a | 371.9 ns · 0B · 0a | 135.6 µs · 122505B · 1611a | 49.67 µs · 18416B · 44a | 84.35 µs · 1872B · 25a | 76.54 µs · 5704B · 30a | — | 80.53 µs · 4368B · 795a |
| sort-stress | 506 µs · 5136B · 512a | 12.62 µs · 5136B · 512a | 40.1× | — | 5.314 µs · 1024B · 2a | 1.289 ms · 270945B · 19965a | 847.3 µs · 36568B · 365a | 1.785 ms · 49104B · 3621a | 1.775 ms · 23448B · 2034a | — | 922.4 µs · 17473B · 1708a |
| spectral-norm | 536.7 µs · 648B · 6a | 8.368 µs · 648B · 6a | 64.1× | — | 7.043 µs · 576B · 3a | 2.071 ms · 571371B · 54822a | 1.076 ms · 234784B · 925a | 2.487 ms · 275337B · 33729a | 4.631 ms · 2457136B · 52718a | — | 4.24 ms · 2448027B · 74233a |
| string-build | 747.6 µs · 85408B · 4107a | 606.3 µs · 85408B · 4107a | 1.23× | — | 349.5 µs · 855890B · 5001a | 2.36 ms · 2211272B · 36101a | 2.334 ms · 1078111B · 13859a | 3.118 ms · 1147385B · 26883a | 2.454 ms · 1326242B · 24469a | — | 3.095 ms · 2372775B · 41432a |
| string-compare | 1.084 ms · 21136B · 1284a | 241 µs · 21136B · 1284a | 4.5× | — | 65.27 µs · 9632B · 1585a | 1.688 ms · 296281B · 20528a | 1.129 ms · 59712B · 3431a | 2.332 ms · 50744B · 3848a | 1.842 ms · 209521B · 5520a | — | 2.372 ms · 503606B · 31846a |
| struct-tree-walk | 283.4 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.63× | — | 1.879 ns · 0B · 0a | 640.8 µs · 458380B · 5114a | 1.332 ms · 818511B · 11253a | 1.077 ms · 558960B · 6149a | 3.401 ms · 2570667B · 34797a | — | 1.216 µs · 600B · 17a |
| tail-ping-pong | 29.1 µs · 0B · 0a | 479.8 ns · 0B · 0a | 60.7× | — | 891 ns · 0B · 0a | 106.1 µs · 106112B · 2001a | 35.13 µs · 21855B · 85a | 113.4 µs · 13984B · 1726a | 117 µs · 14280B · 1731a | — | 559.4 µs · 348834B · 9016a |
| tail-sum | 28.97 µs · 0B · 0a | 676.4 ns · 0B · 0a | 42.8× | — | 302.6 ns · 0B · 0a | 107 µs · 106112B · 2001a | 35.49 µs · 21856B · 85a | 113.4 µs · 13984B · 1726a | 117.2 µs · 14280B · 1731a | — | 84.18 µs · 4608B · 1017a |
| typed-array-sum | 7.332 µs · 0B · 0a | 259.6 ns · 0B · 0a | 28.2× | 266 ns · 8B · 1a | 1.876 ns · 0B · 0a | 42.8 µs · 94208B · 513a | 8.221 µs · 3999B · 15a | 27.5 µs · 2080B · 238a | 17.57 µs · 2496B · 246a | — | — |
| xorshift-i64 | 115.1 µs · 18424B · 2046a | 1.349 µs · 18424B · 2046a | 85.3× | — | 356.1 ns · 0B · 0a | 102 µs · 107608B · 2188a | — | 986.7 µs · 447252B · 12735a | 421.2 µs · 199937B · 7772a | — | 103.3 µs · 13545B · 1317a |

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
