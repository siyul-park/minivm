# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.223 µs · 0B · 0a | 5.471 µs · 0B · 0a | 1.69× | — | 2.216 µs · 1024B · 128a | 38.48 µs · 96288B · 388a | 18.14 µs · 14376B · 256a | 80.31 µs · 78016B · 770a | 14.92 µs · 5712B · 266a | — | 30.66 µs · 2308B · 276a |
| binary-trees | 1.978 ms · 768B · 8a | 599.5 µs · 768B · 8a | 3.3× | — | 287.7 µs · 201936B · 8414a | 3.929 ms · 1468183B · 62877a | 10.52 ms · 6995675B · 92703a | 8.814 ms · 5198708B · 69052a | 28.3 ms · 19457644B · 280714a | — | 28.7 ms · 15128189B · 446106a |
| branch-tree | 1.316 µs · 0B · 0a | 138.2 ns · 0B · 0a | 9.52× | 626 ns · 16B · 1a | 160.5 ns · 0B · 0a | 47.37 µs · 95384B · 660a | 15.07 µs · 2464B · 9a | 26.25 µs · 1992B · 196a | 24 µs · 2168B · 203a | — | 20.63 µs · 1640B · 219a |
| closure-counter | 5.46 µs · 64B · 2a | 897.1 ns · 64B · 2a | 6.09× | — | 39.83 ns · 0B · 0a | 40.39 µs · 92272B · 261a | 12.82 µs · 151B · 3a | 18.94 µs · 1264B · 13a | 76.6 µs · 58312B · 659a | — | 90.78 µs · 35629B · 922a |
| fannkuch | 821.9 µs · 34608B · 1442a | 267.3 µs · 34608B · 1442a | 3.07× | — | 34.92 µs · 17280B · 720a | 2.021 ms · 456579B · 25621a | 1.538 ms · 443136B · 1460a | 19.02 ms · 14005365B · 164946a | 3.353 ms · 1367676B · 16944a | — | 4.735 ms · 2501440B · 88439a |
| fnv1a64 | 120.7 µs · 16400B · 2049a | 1.151 µs · 16400B · 2049a | 105× | — | 902.8 ns · 0B · 0a | 160.8 µs · 120808B · 3838a | — | 1.457 ms · 608249B · 18475a | 389.3 µs · 162921B · 7429a | — | 153.8 µs · 17282B · 2074a |
| i64-wide-fib | 28.05 ms · 2913440B · 364178a | 680 µs · 16B · 1a | 41.3× | — | 239 µs · 0B · 0a | 23.21 µs · 90520B · 52a | — | 152.1 ms · 64193304B · 1942306a | 342 ms · 253472736B · 4248840a | — | 123.1 ms · 98686618B · 2138708a |
| indirect-recursive-fib | 1.18 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.5× | 98.78 µs · 8B · 1a | 34.98 µs · 0B · 0a | 2.059 ms · 319346B · 28655a | 1.933 ms · 704B · 2a | 2.517 ms · 4680B · 39a | 11.46 ms · 10158194B · 109494a | — | 23.94 ms · 13061315B · 394061a |
| iterative-fib | 1.106 µs · 0B · 0a | 69.49 ns · 0B · 0a | 15.9× | 84.77 ns · 8B · 1a | 11.89 ns · 0B · 0a | 23.43 µs · 90592B · 61a | 1.151 µs · 159B · 0a | 4.203 µs · 368B · 20a | 6.048 µs · 2448B · 88a | — | 6.138 µs · 944B · 81a |
| mandelbrot | 289.2 µs · 0B · 0a | 10.93 µs · 0B · 0a | 26.5× | — | 6.321 µs · 0B · 0a | 947.8 µs · 292345B · 25008a | 476.1 µs · 169408B · 661a | 8.036 ms · 5558656B · 104191a | 1.473 ms · 324978B · 23643a | — | 1.527 ms · 365397B · 22513a |
| mat-mul | 369.7 µs · 6216B · 6a | 8.596 µs · 6216B · 6a | 43× | — | 5.426 µs · 6144B · 3a | 1.364 ms · 431715B · 34871a | 665 µs · 143168B · 478a | 1.203 ms · 61304B · 537a | 1.454 ms · 90712B · 9350a | — | 922.3 µs · 36616B · 6036a |
| n-body | 605.8 µs · 504B · 14a | 18.77 µs · 504B · 14a | 32.3× | — | 7.352 µs · 0B · 0a | 1.519 ms · 386898B · 35892a | 1.204 ms · 258913B · 2002a | 3.226 ms · 658435B · 81545a | 2.411 ms · 382714B · 34975a | — | 1.758 ms · 445463B · 20669a |
| n-queens | 429.4 µs · 120B · 6a | 31.58 µs · 120B · 6a | 13.6× | — | 6.002 µs · 0B · 0a | 949.5 µs · 234865B · 17784a | 567.9 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.406 ms · 410345B · 5147a | — | 2.011 ms · 766004B · 33371a |
| permutation-flips | 128.5 µs · 7680B · 128a | 17.87 µs · 7680B · 128a | 7.19× | — | 1.874 ns · 0B · 0a | 559.4 µs · 292857B · 9856a | 233.3 µs · 78808B · 451a | 522.4 µs · 122504B · 765a | 490.2 µs · 111976B · 2368a | — | 998.8 ns · 528B · 13a |
| recursive-fib-20 | 721 µs · 0B · 0a | 47.13 µs · 0B · 0a | 15.3× | 42.73 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.97 ms · 319345B · 28655a | 2.17 ms · 704B · 2a | 2.972 ms · 4680B · 39a | 11.43 ms · 9807915B · 109494a | — | 10.47 ms · 8302696B · 192848a |
| recursive-fib-35 | 971.7 ms · 0B · 0a | 63.97 ms · 0B · 0a | 15.2× | 58.22 ms · 8B · 1a | 29 ms · 0B · 0a | 2.625 s · 312797200B · 39088167a | 3.005 s · 971008B · 3793a | 4.046 s · 375360B · 46373a | 15.53 s · 13378017152B · 149349958a | — | — |
| sieve | 19.87 µs · 1048B · 2a | 943.1 ns · 1048B · 2a | 21.1× | 1.033 µs · 8B · 1a | 350.9 ns · 0B · 0a | 136.8 µs · 122504B · 1611a | 50.56 µs · 18416B · 44a | 83.72 µs · 1872B · 25a | 76.21 µs · 5704B · 30a | — | 81.15 µs · 4368B · 795a |
| sort-stress | 499.5 µs · 5136B · 512a | 12.59 µs · 5136B · 512a | 39.7× | — | 5.795 µs · 1024B · 2a | 1.275 ms · 270945B · 19965a | 849.6 µs · 36568B · 365a | 1.794 ms · 49104B · 3621a | 1.785 ms · 23448B · 2034a | — | 924.3 µs · 17473B · 1708a |
| spectral-norm | 532.1 µs · 648B · 6a | 8.276 µs · 648B · 6a | 64.3× | — | 7.018 µs · 576B · 3a | 2 ms · 571371B · 54822a | 1.069 ms · 234784B · 925a | 2.434 ms · 275337B · 33729a | 4.576 ms · 2457135B · 52718a | — | 4.035 ms · 2448003B · 74233a |
| string-build | 713.7 µs · 85408B · 4107a | 613 µs · 85408B · 4107a | 1.16× | — | 370.9 µs · 855891B · 5001a | 2.408 ms · 2211275B · 36101a | 2.301 ms · 1078110B · 13859a | 3.119 ms · 1147384B · 26883a | 2.439 ms · 1326243B · 24469a | — | 3.049 ms · 2372784B · 41432a |
| string-compare | 1.077 ms · 21136B · 1284a | 242.8 µs · 21136B · 1284a | 4.44× | — | 62.26 µs · 9632B · 1585a | 1.666 ms · 296281B · 20528a | 1.128 ms · 59711B · 3431a | 2.324 ms · 50744B · 3848a | 1.827 ms · 209521B · 5520a | — | 2.353 ms · 503618B · 31846a |
| struct-tree-walk | 284.3 µs · 768B · 8a | 108.1 µs · 768B · 8a | 2.63× | — | 1.874 ns · 0B · 0a | 655.1 µs · 458379B · 5114a | 1.38 ms · 818513B · 11253a | 1.086 ms · 558961B · 6149a | 3.475 ms · 2570669B · 34797a | — | 1.161 µs · 600B · 17a |
| tail-ping-pong | 28.98 µs · 0B · 0a | 474.9 ns · 0B · 0a | 61× | — | 890.8 ns · 0B · 0a | 106.5 µs · 106112B · 2001a | 35.02 µs · 21856B · 85a | 110.9 µs · 13984B · 1726a | 115.4 µs · 14280B · 1731a | — | 534.6 µs · 348833B · 9016a |
| tail-sum | 28.84 µs · 0B · 0a | 665.8 ns · 0B · 0a | 43.3× | — | 302.5 ns · 0B · 0a | 109.7 µs · 106112B · 2001a | 35.85 µs · 21855B · 85a | 112.5 µs · 13984B · 1726a | 116.6 µs · 14280B · 1731a | — | 85.07 µs · 4608B · 1017a |
| typed-array-sum | 7.209 µs · 0B · 0a | 261.2 ns · 0B · 0a | 27.6× | 266.9 ns · 8B · 1a | 1.889 ns · 0B · 0a | 46.53 µs · 94208B · 513a | 8.38 µs · 4000B · 15a | 27.41 µs · 2080B · 238a | 17.06 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.8 µs · 18424B · 2046a | 1.493 µs · 18424B · 2046a | 75.5× | — | 362.2 ns · 0B · 0a | 105.7 µs · 107608B · 2188a | — | 991.4 µs · 447247B · 12735a | 430 µs · 199938B · 7772a | — | 101.1 µs · 13545B · 1317a |

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
