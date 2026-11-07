# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.864 µs · 0B · 0a | 8.853 µs · 0B · 0a | 1× | — | 2.217 µs · 1024B · 128a | 38.87 µs · 96288B · 388a | 17.06 µs · 14376B · 256a | 69.7 µs · 78016B · 770a | 14.02 µs · 5712B · 266a | 9.959 µs · 20416B · 56a | 24.64 µs · 1744B · 148a |
| binary-trees | 1.94 ms · 768B · 8a | 1.222 ms · 768B · 8a | 1.59× | — | 306.4 µs · 201936B · 8414a | 3.922 ms · 1468185B · 62877a | 10.91 ms · 6995683B · 92703a | 9.615 ms · 5198707B · 69052a | 28.38 ms · 19457601B · 280714a | 2.14 ms · 120B · 0a | 29.04 ms · 15127296B · 446011a |
| branch-tree | 1.312 µs · 0B · 0a | 140.1 ns · 0B · 0a | 9.36× | 628.1 ns · 16B · 1a | 160.6 ns · 0B · 0a | 46.23 µs · 95385B · 660a | 15.19 µs · 2464B · 9a | 26.24 µs · 1992B · 196a | 24.04 µs · 2168B · 203a | 9.394 µs · 0B · 0a | 15.06 µs · 1252B · 122a |
| closure-counter | 5.504 µs · 64B · 2a | 948.9 ns · 64B · 2a | 5.8× | — | 39.87 ns · 0B · 0a | 40.93 µs · 92272B · 261a | 12.98 µs · 151B · 3a | 19.17 µs · 1264B · 13a | 78.67 µs · 58312B · 659a | 8.605 µs · 0B · 0a | 86.7 µs · 35049B · 793a |
| fannkuch | 833.7 µs · 34608B · 1442a | 276.2 µs · 34608B · 1442a | 3.02× | — | 35.12 µs · 17280B · 720a | 2.063 ms · 456578B · 25621a | 1.514 ms · 443136B · 1460a | 18.82 ms · 14005351B · 164946a | 3.344 ms · 1367681B · 16944a | 619.7 µs · 35B · 0a | 4.632 ms · 2491465B · 85959a |
| fnv1a64 | 123.2 µs · 16400B · 2049a | 1.196 µs · 16400B · 2049a | 103× | — | 902.7 ns · 0B · 0a | 160.3 µs · 120808B · 3838a | — | 1.49 ms · 608258B · 18475a | 392.9 µs · 162921B · 7429a | 136.5 µs · 8B · 0a | 102.5 µs · 9105B · 1050a |
| i64-wide-fib | 28.48 ms · 2913456B · 364178a | 861.1 µs · 16B · 1a | 33.1× | — | 239.4 µs · 0B · 0a | 23.11 µs · 90520B · 52a | — | 160.2 ms · 64193488B · 1942307a | 343.5 ms · 253472800B · 4248840a | 34.95 ms · 2388B · 6a | 127.2 ms · 98686560B · 2138708a |
| indirect-recursive-fib | 1.185 ms · 0B · 0a | 76.36 µs · 0B · 0a | 15.5× | 99.12 µs · 8B · 1a | 35.11 µs · 0B · 0a | 2.136 ms · 319346B · 28655a | 1.923 ms · 703B · 2a | 2.56 ms · 4680B · 39a | 11.23 ms · 10158184B · 109494a | 929.1 µs · 51B · 0a | 26.11 ms · 13061313B · 394061a |
| iterative-fib | 1.105 µs · 0B · 0a | 71.6 ns · 0B · 0a | 15.4× | 86.16 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.43 µs · 90592B · 61a | 1.16 µs · 160B · 0a | 4.27 µs · 368B · 20a | 6.096 µs · 2448B · 88a | 857.1 ns · 0B · 0a | 4.473 µs · 784B · 50a |
| mandelbrot | 292.1 µs · 0B · 0a | 11.1 µs · 0B · 0a | 26.3× | — | 6.345 µs · 0B · 0a | 955.7 µs · 292345B · 25008a | 487.9 µs · 169407B · 661a | 8.25 ms · 5558654B · 104191a | 1.526 ms · 324979B · 23643a | 366.4 µs · 21B · 0a | 1.276 ms · 324081B · 19107a |
| mat-mul | 370.9 µs · 6216B · 6a | 8.977 µs · 6216B · 6a | 41.3× | — | 5.8 µs · 6144B · 3a | 1.391 ms · 431715B · 34871a | 682.3 µs · 143168B · 478a | 1.228 ms · 61304B · 537a | 1.46 ms · 90712B · 9350a | 371.5 µs · 21B · 0a | 617.4 µs · 16874B · 1134a |
| n-body | 612.6 µs · 504B · 14a | 19.29 µs · 504B · 14a | 31.8× | — | 7.347 µs · 0B · 0a | 1.517 ms · 386898B · 35892a | 1.236 ms · 258913B · 2002a | 3.382 ms · 658436B · 81545a | 2.457 ms · 382714B · 34975a | 499.8 µs · 31B · 0a | 1.615 ms · 400502B · 18248a |
| n-queens | 428.9 µs · 120B · 6a | 32.84 µs · 120B · 6a | 13.1× | — | 6.025 µs · 0B · 0a | 963.6 µs · 234865B · 17784a | 567.1 µs · 14423B · 63a | 1.101 ms · 7424B · 35a | 1.466 ms · 410346B · 5147a | 283.7 µs · 16B · 0a | 1.783 ms · 749463B · 29235a |
| permutation-flips | 128.8 µs · 7680B · 128a | 22.83 µs · 7680B · 128a | 5.64× | — | 1.893 ns · 0B · 0a | 565.5 µs · 292858B · 9856a | 236.8 µs · 78808B · 451a | 534.3 µs · 122504B · 765a | 502.8 µs · 111976B · 2368a | 81.54 µs · 5B · 0a | 1.045 µs · 528B · 13a |
| recursive-fib-20 | 707.9 µs · 0B · 0a | 60.91 µs · 0B · 0a | 11.6× | 42.74 µs · 8B · 1a | 21.55 µs · 0B · 0a | 1.973 ms · 319345B · 28655a | 2.166 ms · 704B · 2a | 2.967 ms · 4680B · 39a | 11.73 ms · 9807917B · 109494a | 892.1 µs · 49B · 0a | 10.24 ms · 8302672B · 192847a |
| recursive-fib-35 | 966.8 ms · 0B · 0a | 82.87 ms · 0B · 0a | 11.7× | 58.22 ms · 8B · 1a | 29.14 ms · 0B · 0a | 2.635 s · 312797280B · 39088167a | 2.955 s · 971008B · 3793a | 4.035 s · 375360B · 46373a | 16.15 s · 13378013744B · 149349961a | 1.208 s · 19088B · 52a | — |
| sieve | 20.05 µs · 1048B · 2a | 968.3 ns · 1048B · 2a | 20.7× | 1.055 µs · 8B · 1a | 351 ns · 0B · 0a | 141.2 µs · 122504B · 1611a | 51.65 µs · 18416B · 44a | 84.36 µs · 1872B · 25a | 77.64 µs · 5704B · 30a | 18.94 µs · 1B · 0a | 40.8 µs · 1256B · 43a |
| sort-stress | 504 µs · 5136B · 512a | 12.66 µs · 5136B · 512a | 39.8× | — | 5.979 µs · 1024B · 2a | 1.285 ms · 270945B · 19965a | 847.5 µs · 36567B · 365a | 1.795 ms · 49104B · 3621a | 1.806 ms · 23448B · 2034a | 511.3 µs · 29B · 0a | 882.9 µs · 13123B · 935a |
| spectral-norm | 530.6 µs · 648B · 6a | 34.95 µs · 648B · 6a | 15.2× | — | 7.134 µs · 576B · 3a | 2.011 ms · 571371B · 54822a | 1.092 ms · 234784B · 925a | 2.502 ms · 275337B · 33729a | 4.706 ms · 2457134B · 52718a | 827.8 µs · 46B · 0a | 3.79 ms · 2443900B · 69364a |
| string-build | 734.1 µs · 85408B · 4107a | 598.3 µs · 85408B · 4107a | 1.23× | — | 474.6 µs · 855892B · 5001a | 2.58 ms · 2211273B · 36101a | 2.431 ms · 1078112B · 13859a | 3.19 ms · 1147384B · 26883a | 2.527 ms · 1326243B · 24469a | 551.9 µs · 31B · 0a | 2.974 ms · 2348538B · 38417a |
| struct-tree-walk | 281.1 µs · 768B · 8a | 183.7 µs · 768B · 8a | 1.53× | — | 1.891 ns · 0B · 0a | 671.6 µs · 458378B · 5114a | 1.381 ms · 818512B · 11253a | 1.107 ms · 558961B · 6149a | 3.585 ms · 2570663B · 34797a | 244.6 µs · 14B · 0a | 1.152 µs · 608B · 17a |
| tail-ping-pong | 29.34 µs · 0B · 0a | 510.5 ns · 0B · 0a | 57.5× | — | 891 ns · 0B · 0a | 110.7 µs · 106112B · 2001a | 36.74 µs · 21856B · 85a | 113.4 µs · 13984B · 1726a | 117.2 µs · 14280B · 1731a | 46.04 µs · 2B · 0a | 527.1 µs · 348835B · 9016a |
| tail-sum | 29.18 µs · 0B · 0a | 646.3 ns · 0B · 0a | 45.2× | — | 301.6 ns · 0B · 0a | 112.4 µs · 106112B · 2001a | 36.83 µs · 21856B · 85a | 112.9 µs · 13984B · 1726a | 117.1 µs · 14280B · 1731a | 46.32 µs · 2B · 0a | 30.06 µs · 584B · 16a |
| typed-array-sum | 7.195 µs · 0B · 0a | 229.5 ns · 0B · 0a | 31.4× | 268.5 ns · 8B · 1a | 1.902 ns · 0B · 0a | 49.42 µs · 94208B · 513a | 8.528 µs · 4000B · 15a | 27.58 µs · 2080B · 238a | 17.43 µs · 2496B · 246a | 6.286 µs · 0B · 0a | — |
| xorshift-i64 | 108.7 µs · 18424B · 2046a | 1.49 µs · 18424B · 2046a | 72.9× | — | 360.3 ns · 0B · 0a | 106.2 µs · 107608B · 2188a | — | 970.1 µs · 447252B · 12736a | 424.3 µs · 199938B · 7772a | 92.54 µs · 5B · 0a | 62.78 µs · 7361B · 547a |

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
