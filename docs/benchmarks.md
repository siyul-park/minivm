# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.508 µs · 0B · 0a | 5.456 µs · 0B · 0a | 1.74× | — | 2.529 µs · 1024B · 128a | 43.24 µs · 96288B · 388a | 17.5 µs · 14376B · 256a | 70.62 µs · 78016B · 770a | 14.61 µs · 5712B · 266a | — | 30.87 µs · 2308B · 276a |
| binary-trees | 1.988 ms · 768B · 8a | 595.4 µs · 768B · 8a | 3.34× | — | 292 µs · 201936B · 8414a | 3.869 ms · 1468183B · 62877a | 10.62 ms · 6995689B · 92703a | 9.063 ms · 5198703B · 69052a | 28.13 ms · 19457649B · 280714a | — | 30.19 ms · 15128176B · 446106a |
| branch-tree | 1.31 µs · 0B · 0a | 152.4 ns · 0B · 0a | 8.6× | 626.6 ns · 16B · 1a | 161 ns · 0B · 0a | 46.85 µs · 95385B · 660a | 15.31 µs · 2464B · 9a | 26.82 µs · 1992B · 196a | 24.14 µs · 2168B · 203a | — | 20.61 µs · 1640B · 219a |
| closure-counter | 5.511 µs · 64B · 2a | 949.6 ns · 64B · 2a | 5.8× | — | 40.36 ns · 0B · 0a | 40.16 µs · 92272B · 261a | 12.88 µs · 151B · 3a | 19.3 µs · 1264B · 13a | 81.96 µs · 58312B · 659a | — | 95.34 µs · 35628B · 922a |
| coroutine-gen | 77.6 µs · 128B · 2a | 77.41 µs · 128B · 2a | 1× | — | 42.78 µs · 208B · 7a | — | 329.5 µs · 149936B · 649a | 602 µs · 330649B · 6719a | 199.3 µs · 91800B · 3646a | — | — |
| fannkuch | 829.1 µs · 34608B · 1442a | 238.7 µs · 34608B · 1442a | 3.47× | — | 35.3 µs · 17280B · 720a | 2.065 ms · 456578B · 25621a | 1.523 ms · 443136B · 1460a | 19.21 ms · 14005345B · 164946a | 3.345 ms · 1367682B · 16944a | — | 4.918 ms · 2501433B · 88439a |
| fnv1a64 | 127.8 µs · 16400B · 2049a | 1.16 µs · 16400B · 2049a | 110× | — | 902.5 ns · 0B · 0a | 163.7 µs · 120808B · 3838a | — | 1.483 ms · 608255B · 18475a | 397.4 µs · 162921B · 7429a | — | 155.5 µs · 17283B · 2074a |
| host-call | 753.1 µs · 196592B · 14073a | 726.7 µs · 196592B · 14073a | 1.04× | — | 3.312 µs · 0B · 0a | 179 µs · 139264B · 3073a | 134 µs · 22536B · 1079a | 839.1 µs · 158048B · 14585a | 107.6 µs · 14920B · 1799a | — | 562.3 µs · 340637B · 9231a |
| i64-wide-fib | 28.68 ms · 2913456B · 364178a | 681.7 µs · 16B · 1a | 42.1× | — | 238.8 µs · 0B · 0a | 23.07 µs · 90520B · 52a | — | 159.7 ms · 64193296B · 1942305a | 346.2 ms · 253472848B · 4248841a | — | 132.4 ms · 98686592B · 2138708a |
| indirect-recursive-fib | 1.18 ms · 0B · 0a | 60.49 µs · 0B · 0a | 19.5× | 100 µs · 8B · 1a | 34.81 µs · 0B · 0a | 2.105 ms · 319346B · 28655a | 1.949 ms · 703B · 2a | 2.554 ms · 4680B · 39a | 11.59 ms · 10158182B · 109494a | — | 25.99 ms · 13061361B · 394062a |
| iterative-fib | 1.115 µs · 0B · 0a | 82.79 ns · 0B · 0a | 13.5× | 85.55 ns · 8B · 1a | 12.46 ns · 0B · 0a | 23.5 µs · 90592B · 61a | 1.197 µs · 159B · 0a | 4.246 µs · 368B · 20a | 6.162 µs · 2448B · 88a | — | 6.196 µs · 944B · 81a |
| mandelbrot | 289.6 µs · 0B · 0a | 10.93 µs · 0B · 0a | 26.5× | — | 6.32 µs · 0B · 0a | 972 µs · 292345B · 25008a | 483.9 µs · 169407B · 661a | 8.089 ms · 5558650B · 104191a | 1.514 ms · 324979B · 23643a | — | 1.592 ms · 365399B · 22513a |
| map-ops | 73.12 µs · 9584B · 5a | 73.43 µs · 9584B · 5a | 0.996× | — | 10.75 µs · 4904B · 3a | — | 273 µs · 114328B · 198a | 385 µs · 66728B · 4078a | — | — | 1.218 ms · 501524B · 15813a |
| mat-mul | 372.1 µs · 6216B · 6a | 8.437 µs · 6216B · 6a | 44.1× | — | 5.338 µs · 6144B · 3a | 1.352 ms · 431715B · 34871a | 667.6 µs · 143168B · 478a | 1.202 ms · 61304B · 537a | 1.457 ms · 90712B · 9350a | — | 928.8 µs · 36614B · 6036a |
| n-body | 608.3 µs · 504B · 14a | 18.83 µs · 504B · 14a | 32.3× | — | 7.357 µs · 0B · 0a | 1.552 ms · 386898B · 35892a | 1.231 ms · 258913B · 2002a | 3.319 ms · 658435B · 81545a | 2.44 ms · 382715B · 34975a | — | 1.763 ms · 445469B · 20669a |
| n-queens | 429.1 µs · 120B · 6a | 31.54 µs · 120B · 6a | 13.6× | — | 6.004 µs · 0B · 0a | 959.3 µs · 234865B · 17784a | 570.7 µs · 14424B · 63a | 1.093 ms · 7424B · 35a | 1.451 ms · 410345B · 5147a | — | 2.02 ms · 766014B · 33371a |
| permutation-flips | 129.3 µs · 7680B · 128a | 17.82 µs · 7680B · 128a | 7.25× | — | 1.874 ns · 0B · 0a | 558.4 µs · 292858B · 9856a | 236.7 µs · 78808B · 451a | 528.9 µs · 122504B · 765a | 494.2 µs · 111976B · 2368a | — | 1.068 µs · 528B · 13a |
| recursive-fib-20 | 713.8 µs · 0B · 0a | 47.73 µs · 0B · 0a | 15× | 42.74 µs · 8B · 1a | 21.46 µs · 0B · 0a | 1.974 ms · 319345B · 28655a | 2.194 ms · 703B · 2a | 2.955 ms · 4680B · 39a | 11.38 ms · 9807904B · 109494a | — | 11.26 ms · 8302747B · 192848a |
| recursive-fib-35 | 972.1 ms · 0B · 0a | 63.93 ms · 0B · 0a | 15.2× | 58.21 ms · 8B · 1a | 29.15 ms · 0B · 0a | 2.663 s · 312797280B · 39088167a | 2.957 s · 971008B · 3793a | 4.02 s · 375376B · 46373a | 15.64 s · 13378017360B · 149349965a | — | — |
| sieve | 20.75 µs · 1048B · 2a | 949.8 ns · 1048B · 2a | 21.8× | 1.026 µs · 8B · 1a | 372.4 ns · 0B · 0a | 137.7 µs · 122504B · 1611a | 50.47 µs · 18416B · 44a | 84.1 µs · 1872B · 25a | 76.75 µs · 5704B · 30a | — | 80.39 µs · 4368B · 795a |
| sort-stress | 504.4 µs · 5136B · 512a | 12.6 µs · 5136B · 512a | 40× | — | 5.298 µs · 1024B · 2a | 1.294 ms · 270946B · 19965a | 846.5 µs · 36568B · 365a | 1.789 ms · 49104B · 3621a | 1.776 ms · 23448B · 2034a | — | 922.8 µs · 17473B · 1708a |
| spectral-norm | 536.5 µs · 648B · 6a | 8.358 µs · 648B · 6a | 64.2× | — | 7.099 µs · 576B · 3a | 2.072 ms · 571371B · 54822a | 1.08 ms · 234783B · 925a | 2.464 ms · 275337B · 33729a | 4.595 ms · 2457131B · 52718a | — | 4.2 ms · 2448016B · 74233a |
| string-build | 726.1 µs · 85408B · 4107a | 495.8 µs · 85408B · 4107a | 1.46× | — | 368.3 µs · 855890B · 5001a | 2.418 ms · 2211276B · 36101a | 2.332 ms · 1078109B · 13859a | 3.182 ms · 1147384B · 26883a | 2.485 ms · 1326245B · 24469a | — | 3.135 ms · 2372778B · 41432a |
| string-compare | 1.082 ms · 21136B · 1284a | 240.7 µs · 21136B · 1284a | 4.49× | — | 65.76 µs · 9632B · 1585a | 1.671 ms · 296281B · 20528a | 1.144 ms · 59712B · 3431a | 2.332 ms · 50744B · 3848a | 1.852 ms · 209521B · 5520a | — | 2.422 ms · 503615B · 31846a |
| struct-tree-walk | 283.2 µs · 768B · 8a | 107.2 µs · 768B · 8a | 2.64× | — | 1.879 ns · 0B · 0a | 645.6 µs · 458379B · 5114a | 1.358 ms · 818513B · 11253a | 1.075 ms · 558961B · 6149a | 3.457 ms · 2570663B · 34797a | — | 1.248 µs · 600B · 17a |
| tail-ping-pong | 29.55 µs · 0B · 0a | 481.1 ns · 0B · 0a | 61.4× | — | 891 ns · 0B · 0a | 107.2 µs · 106112B · 2001a | 37.14 µs · 21856B · 85a | 113.4 µs · 13984B · 1726a | 117.7 µs · 14280B · 1731a | — | 566.1 µs · 348833B · 9016a |
| tail-sum | 29.49 µs · 0B · 0a | 678.9 ns · 0B · 0a | 43.4× | — | 302.6 ns · 0B · 0a | 106.8 µs · 106112B · 2001a | 35.69 µs · 21855B · 85a | 114.2 µs · 13984B · 1726a | 118.2 µs · 14280B · 1731a | — | 83.98 µs · 4608B · 1017a |
| typed-array-sum | 7.382 µs · 0B · 0a | 262.9 ns · 0B · 0a | 28.1× | 266.4 ns · 8B · 1a | 1.875 ns · 0B · 0a | 43.16 µs · 94208B · 513a | 8.298 µs · 3999B · 15a | 27.77 µs · 2080B · 238a | 17.44 µs · 2496B · 246a | — | — |
| xorshift-i64 | 115.4 µs · 18424B · 2046a | 1.324 µs · 18424B · 2046a | 87.1× | — | 356.1 ns · 0B · 0a | 101.6 µs · 107608B · 2188a | — | 1.008 ms · 447255B · 12736a | 436.1 µs · 199938B · 7772a | — | 102.3 µs · 13545B · 1317a |

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
