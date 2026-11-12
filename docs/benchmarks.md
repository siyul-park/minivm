# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.865 µs · 0B · 0a | 5.304 µs · 0B · 0a | 1.67× | — | 2.225 µs · 1024B · 128a | 37.73 µs · 96288B · 388a | 17.64 µs · 14376B · 256a | 70.16 µs · 78016B · 770a | 14.51 µs · 5712B · 266a | — | 31.03 µs · 2308B · 276a |
| binary-trees | 1.984 ms · 768B · 8a | 592.5 µs · 768B · 8a | 3.35× | — | 297.8 µs · 201936B · 8414a | 3.902 ms · 1468185B · 62877a | 10.49 ms · 6995676B · 92703a | 9.02 ms · 5198701B · 69052a | 27.59 ms · 19457606B · 280714a | — | 30.79 ms · 15128177B · 446106a |
| branch-tree | 1.338 µs · 0B · 0a | 158 ns · 0B · 0a | 8.47× | 625.4 ns · 16B · 1a | 161.7 ns · 0B · 0a | 47.61 µs · 95385B · 660a | 15.13 µs · 2464B · 9a | 26.55 µs · 1992B · 196a | 24.58 µs · 2168B · 203a | — | 20.76 µs · 1640B · 219a |
| closure-counter | 5.543 µs · 64B · 2a | 951.5 ns · 64B · 2a | 5.83× | — | 40.39 ns · 0B · 0a | 40.27 µs · 92272B · 261a | 13.17 µs · 151B · 3a | 19.04 µs · 1264B · 13a | 78.18 µs · 58312B · 659a | — | 94.99 µs · 35629B · 922a |
| coroutine-gen | 77.72 µs · 128B · 2a | 77.23 µs · 128B · 2a | 1.01× | — | 41.56 µs · 208B · 7a | — | 327.5 µs · 149936B · 649a | 606.5 µs · 330650B · 6719a | 202.5 µs · 91800B · 3646a | — | — |
| fannkuch | 827.7 µs · 34608B · 1442a | 227.4 µs · 34608B · 1442a | 3.64× | — | 35.63 µs · 17280B · 720a | 2.037 ms · 456579B · 25621a | 1.531 ms · 443137B · 1460a | 19.34 ms · 14005345B · 164946a | 3.417 ms · 1367677B · 16944a | — | 4.876 ms · 2501438B · 88439a |
| fnv1a64 | 126.9 µs · 16400B · 2049a | 1.163 µs · 16400B · 2049a | 109× | — | 903.8 ns · 0B · 0a | 163.7 µs · 120808B · 3838a | — | 1.507 ms · 608253B · 18475a | 403.5 µs · 162921B · 7429a | — | 155.4 µs · 17282B · 2074a |
| host-call | 748.7 µs · 196592B · 14073a | 752.8 µs · 196592B · 14073a | 0.995× | — | 3.312 µs · 0B · 0a | 180.3 µs · 139264B · 3073a | 131.9 µs · 22536B · 1079a | 812.4 µs · 158048B · 14585a | 109 µs · 14920B · 1799a | — | 600.5 µs · 340638B · 9231a |
| i64-wide-fib | 28.54 ms · 2913456B · 364178a | 682.2 µs · 16B · 1a | 41.8× | — | 239 µs · 0B · 0a | 23.38 µs · 90520B · 52a | — | 159.5 ms · 64193392B · 1942306a | 347.7 ms · 253472880B · 4248841a | — | 134.5 ms · 98686597B · 2138708a |
| indirect-recursive-fib | 1.179 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.5× | 98.91 µs · 8B · 1a | 34.78 µs · 0B · 0a | 2.102 ms · 319345B · 28655a | 1.944 ms · 704B · 2a | 2.481 ms · 4680B · 39a | 11.48 ms · 10158214B · 109494a | — | 25.88 ms · 13061341B · 394061a |
| iterative-fib | 1.113 µs · 0B · 0a | 86.21 ns · 0B · 0a | 12.9× | 85.27 ns · 8B · 1a | 12.37 ns · 0B · 0a | 25.44 µs · 90592B · 61a | 1.15 µs · 160B · 0a | 4.266 µs · 368B · 20a | 6.093 µs · 2448B · 88a | — | 6.195 µs · 944B · 81a |
| mandelbrot | 293.1 µs · 0B · 0a | 10.89 µs · 0B · 0a | 26.9× | — | 6.345 µs · 0B · 0a | 965.6 µs · 292346B · 25008a | 484.1 µs · 169408B · 661a | 8.121 ms · 5558655B · 104191a | 1.531 ms · 324978B · 23643a | — | 1.571 ms · 365401B · 22513a |
| map-ops | 70.59 µs · 8816B · 3a | 11.35 µs · 8816B · 3a | 6.22× | — | 11.09 µs · 4904B · 3a | — | 277.1 µs · 114328B · 198a | 389.4 µs · 66728B · 4078a | — | — | 1.199 ms · 501517B · 15813a |
| mat-mul | 371.8 µs · 6216B · 6a | 8.745 µs · 6216B · 6a | 42.5× | — | 5.711 µs · 6144B · 3a | 1.38 ms · 431714B · 34871a | 673.8 µs · 143168B · 478a | 1.219 ms · 61304B · 537a | 1.473 ms · 90712B · 9350a | — | 927.5 µs · 36613B · 6036a |
| n-body | 609.8 µs · 504B · 14a | 19 µs · 504B · 14a | 32.1× | — | 7.36 µs · 0B · 0a | 1.517 ms · 386898B · 35892a | 1.223 ms · 258913B · 2002a | 3.391 ms · 658436B · 81545a | 2.489 ms · 382714B · 34975a | — | 1.782 ms · 445466B · 20669a |
| n-queens | 434.5 µs · 120B · 6a | 31.61 µs · 120B · 6a | 13.7× | — | 6.025 µs · 0B · 0a | 974.4 µs · 234865B · 17784a | 573.5 µs · 14423B · 63a | 1.104 ms · 7424B · 35a | 1.45 ms · 410346B · 5147a | — | 2.064 ms · 766007B · 33371a |
| permutation-flips | 129.9 µs · 7680B · 128a | 17.41 µs · 7680B · 128a | 7.46× | — | 1.877 ns · 0B · 0a | 570.7 µs · 292858B · 9856a | 236.5 µs · 78808B · 451a | 532.6 µs · 122504B · 765a | 502.3 µs · 111976B · 2368a | — | 1.049 µs · 528B · 13a |
| recursive-fib-20 | 719.1 µs · 0B · 0a | 47.13 µs · 0B · 0a | 15.3× | 42.72 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.974 ms · 319346B · 28655a | 2.213 ms · 704B · 2a | 2.978 ms · 4680B · 39a | 11.53 ms · 9807918B · 109494a | — | 11.36 ms · 8302723B · 192848a |
| recursive-fib-35 | 982.1 ms · 0B · 0a | 63.88 ms · 0B · 0a | 15.4× | 58.22 ms · 8B · 1a | 29.15 ms · 0B · 0a | 2.63 s · 312797632B · 39088170a | 3.019 s · 971008B · 3793a | 4.058 s · 375360B · 46373a | 15.82 s · 13378016768B · 149349992a | — | — |
| sieve | 20.05 µs · 1048B · 2a | 953 ns · 1048B · 2a | 21× | 1.035 µs · 8B · 1a | 371.8 ns · 0B · 0a | 140 µs · 122504B · 1611a | 51.18 µs · 18416B · 44a | 83.62 µs · 1872B · 25a | 77.59 µs · 5704B · 30a | — | 81.26 µs · 4368B · 795a |
| sort-stress | 507.5 µs · 5136B · 512a | 12.72 µs · 5136B · 512a | 39.9× | — | 5.466 µs · 1024B · 2a | 1.314 ms · 270945B · 19965a | 854.9 µs · 36568B · 365a | 1.797 ms · 49104B · 3621a | 1.79 ms · 23448B · 2034a | — | 927 µs · 17473B · 1708a |
| spectral-norm | 539.8 µs · 648B · 6a | 8.391 µs · 648B · 6a | 64.3× | — | 7.096 µs · 576B · 3a | 2.079 ms · 571370B · 54822a | 1.094 ms · 234784B · 925a | 2.515 ms · 275337B · 33729a | 4.768 ms · 2457139B · 52718a | — | 4.321 ms · 2448011B · 74233a |
| string-build | 738 µs · 85408B · 4107a | 616.4 µs · 85408B · 4107a | 1.2× | — | 422.6 µs · 855890B · 5001a | 2.532 ms · 2211273B · 36101a | 2.394 ms · 1078110B · 13859a | 3.194 ms · 1147383B · 26883a | 2.528 ms · 1326242B · 24469a | — | 3.18 ms · 2372791B · 41432a |
| string-compare | 1.087 ms · 21136B · 1284a | 239.7 µs · 21136B · 1284a | 4.54× | — | 64.91 µs · 9632B · 1585a | 1.703 ms · 296281B · 20528a | 1.157 ms · 59712B · 3431a | 2.363 ms · 50744B · 3848a | 1.89 ms · 209521B · 5520a | — | 2.479 ms · 503616B · 31846a |
| struct-tree-walk | 282.4 µs · 768B · 8a | 108.4 µs · 768B · 8a | 2.6× | — | 1.877 ns · 0B · 0a | 659.4 µs · 458379B · 5114a | 1.39 ms · 818512B · 11253a | 1.123 ms · 558961B · 6149a | 3.537 ms · 2570666B · 34797a | — | 1.246 µs · 600B · 17a |
| tail-ping-pong | 28.87 µs · 0B · 0a | 487 ns · 0B · 0a | 59.3× | — | 890.6 ns · 0B · 0a | 110.5 µs · 106112B · 2001a | 35.84 µs · 21856B · 85a | 113.7 µs · 13984B · 1726a | 120.3 µs · 14280B · 1731a | — | 578.2 µs · 348833B · 9016a |
| tail-sum | 28.97 µs · 0B · 0a | 687 ns · 0B · 0a | 42.2× | — | 302.5 ns · 0B · 0a | 109.7 µs · 106112B · 2001a | 36.03 µs · 21856B · 85a | 113.4 µs · 13984B · 1726a | 119.6 µs · 14280B · 1731a | — | 84.33 µs · 4608B · 1017a |
| typed-array-sum | 7.335 µs · 0B · 0a | 268.6 ns · 0B · 0a | 27.3× | 262.7 ns · 8B · 1a | 1.877 ns · 0B · 0a | 46.23 µs · 94208B · 513a | 8.298 µs · 4000B · 15a | 27.59 µs · 2080B · 238a | 17.49 µs · 2496B · 246a | — | — |
| xorshift-i64 | 113.9 µs · 18424B · 2046a | 1.414 µs · 18424B · 2046a | 80.5× | — | 356.1 ns · 0B · 0a | 103 µs · 107608B · 2188a | — | 1.018 ms · 447250B · 12735a | 440.2 µs · 199938B · 7772a | — | 102.5 µs · 13545B · 1317a |

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
