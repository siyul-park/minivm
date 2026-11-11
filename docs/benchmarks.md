# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.863 µs · 0B · 0a | 5.379 µs · 0B · 0a | 1.65× | — | 2.32 µs · 1024B · 128a | 37.05 µs · 96289B · 388a | 17.53 µs · 14376B · 256a | 69.22 µs · 78016B · 770a | 14.38 µs · 5712B · 266a | — | 30.95 µs · 2308B · 276a |
| binary-trees | 1.983 ms · 768B · 8a | 594.7 µs · 768B · 8a | 3.33× | — | 295.5 µs · 201936B · 8414a | 3.934 ms · 1468183B · 62877a | 10.54 ms · 6995667B · 92703a | 8.965 ms · 5198699B · 69052a | 27.78 ms · 19457616B · 280714a | — | 29.82 ms · 15128169B · 446106a |
| branch-tree | 1.297 µs · 0B · 0a | 151.2 ns · 0B · 0a | 8.58× | 626.5 ns · 16B · 1a | 160.5 ns · 0B · 0a | 46.44 µs · 95384B · 660a | 15.27 µs · 2463B · 9a | 26.95 µs · 1992B · 196a | 24.57 µs · 2168B · 203a | — | 21.08 µs · 1640B · 219a |
| closure-counter | 5.566 µs · 64B · 2a | 939.2 ns · 64B · 2a | 5.93× | — | 40.35 ns · 0B · 0a | 40.37 µs · 92272B · 261a | 13.48 µs · 151B · 3a | 19.14 µs · 1264B · 13a | 78.46 µs · 58312B · 659a | — | 93.7 µs · 35628B · 922a |
| coroutine-gen | 77.09 µs · 128B · 2a | 77.19 µs · 128B · 2a | 0.999× | — | 42.7 µs · 208B · 7a | — | 334.1 µs · 149936B · 649a | 598.3 µs · 330649B · 6719a | 202.2 µs · 91800B · 3646a | — | — |
| fannkuch | 843.4 µs · 34608B · 1442a | 231.2 µs · 34608B · 1442a | 3.65× | — | 35.46 µs · 17280B · 720a | 2.052 ms · 456578B · 25621a | 1.533 ms · 443136B · 1460a | 19.07 ms · 14005390B · 164946a | 3.363 ms · 1367680B · 16944a | — | 4.875 ms · 2501433B · 88439a |
| fnv1a64 | 126.6 µs · 16400B · 2049a | 1.161 µs · 16400B · 2049a | 109× | — | 902.7 ns · 0B · 0a | 162.7 µs · 120808B · 3838a | — | 1.466 ms · 608254B · 18475a | 396 µs · 162921B · 7429a | — | 157.7 µs · 17282B · 2074a |
| host-call | 746.6 µs · 196592B · 14073a | 756.7 µs · 196592B · 14073a | 0.987× | — | 3.313 µs · 0B · 0a | 181 µs · 139264B · 3073a | 132.4 µs · 22536B · 1079a | 822 µs · 158048B · 14585a | 105.4 µs · 14920B · 1799a | — | 594.4 µs · 340640B · 9231a |
| i64-wide-fib | 29.09 ms · 2913456B · 364178a | 679.9 µs · 16B · 1a | 42.8× | — | 238.7 µs · 0B · 0a | 22.08 µs · 90520B · 52a | — | 158.1 ms · 64193248B · 1942305a | 336.8 ms · 253472720B · 4248840a | — | 132.1 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 1.181 ms · 0B · 0a | 60.49 µs · 0B · 0a | 19.5× | 98.99 µs · 8B · 1a | 34.79 µs · 0B · 0a | 2.144 ms · 319346B · 28655a | 1.944 ms · 704B · 2a | 2.548 ms · 4680B · 39a | 11.15 ms · 10158186B · 109494a | — | 26.56 ms · 13061317B · 394061a |
| iterative-fib | 1.111 µs · 0B · 0a | 80.5 ns · 0B · 0a | 13.8× | 86.59 ns · 8B · 1a | 12.45 ns · 0B · 0a | 22.85 µs · 90592B · 61a | 1.2 µs · 160B · 0a | 4.277 µs · 368B · 20a | 6.293 µs · 2448B · 88a | — | 6.223 µs · 944B · 81a |
| mandelbrot | 294 µs · 0B · 0a | 10.9 µs · 0B · 0a | 27× | — | 6.324 µs · 0B · 0a | 979.7 µs · 292345B · 25008a | 488 µs · 169408B · 661a | 8.079 ms · 5558656B · 104191a | 1.512 ms · 324978B · 23643a | — | 1.584 ms · 365400B · 22513a |
| map-ops | 69.13 µs · 8816B · 3a | 69.53 µs · 8816B · 3a | 0.994× | — | 10.72 µs · 4904B · 3a | — | 274.1 µs · 114328B · 198a | 388.7 µs · 66728B · 4078a | — | — | 1.196 ms · 501525B · 15813a |
| mat-mul | 368.9 µs · 6216B · 6a | 8.47 µs · 6216B · 6a | 43.6× | — | 5.335 µs · 6144B · 3a | 1.366 ms · 431715B · 34871a | 666.7 µs · 143168B · 478a | 1.202 ms · 61304B · 537a | 1.464 ms · 90712B · 9350a | — | 933 µs · 36614B · 6036a |
| n-body | 611.3 µs · 504B · 14a | 18.91 µs · 504B · 14a | 32.3× | — | 7.355 µs · 0B · 0a | 1.516 ms · 386898B · 35892a | 1.224 ms · 258913B · 2002a | 3.328 ms · 658436B · 81545a | 2.498 ms · 382714B · 34975a | — | 1.785 ms · 445466B · 20669a |
| n-queens | 430.2 µs · 120B · 6a | 31.58 µs · 120B · 6a | 13.6× | — | 6.033 µs · 0B · 0a | 951 µs · 234865B · 17784a | 571.1 µs · 14423B · 63a | 1.093 ms · 7424B · 35a | 1.452 ms · 410346B · 5147a | — | 2.029 ms · 766010B · 33371a |
| permutation-flips | 128.9 µs · 7680B · 128a | 17.31 µs · 7680B · 128a | 7.45× | — | 1.875 ns · 0B · 0a | 552.6 µs · 292857B · 9856a | 233.1 µs · 78808B · 451a | 527.6 µs · 122504B · 765a | 495 µs · 111976B · 2368a | — | 1.093 µs · 528B · 13a |
| recursive-fib-20 | 713 µs · 0B · 0a | 47.17 µs · 0B · 0a | 15.1× | 42.75 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.968 ms · 319345B · 28655a | 2.164 ms · 704B · 2a | 2.963 ms · 4680B · 39a | 11.24 ms · 9807909B · 109494a | — | 11.69 ms · 8302737B · 192848a |
| recursive-fib-35 | 972.7 ms · 0B · 0a | 63.89 ms · 0B · 0a | 15.2× | 58.25 ms · 8B · 1a | 29.21 ms · 0B · 0a | 2.63 s · 312797152B · 39088167a | 3.001 s · 971008B · 3793a | 4.025 s · 375360B · 46373a | 15.37 s · 13378022064B · 149350011a | — | — |
| sieve | 20.03 µs · 1048B · 2a | 910.7 ns · 1048B · 2a | 22× | 1.034 µs · 8B · 1a | 371.8 ns · 0B · 0a | 136.7 µs · 122504B · 1611a | 50.62 µs · 18416B · 44a | 84.45 µs · 1872B · 25a | 77.45 µs · 5704B · 30a | — | 81.58 µs · 4368B · 795a |
| sort-stress | 507 µs · 5136B · 512a | 12.58 µs · 5136B · 512a | 40.3× | — | 5.282 µs · 1024B · 2a | 1.299 ms · 270945B · 19965a | 848.7 µs · 36568B · 365a | 1.796 ms · 49104B · 3621a | 1.78 ms · 23448B · 2034a | — | 925.7 µs · 17476B · 1708a |
| spectral-norm | 531.5 µs · 648B · 6a | 8.37 µs · 648B · 6a | 63.5× | — | 7.045 µs · 576B · 3a | 2.124 ms · 571371B · 54822a | 1.081 ms · 234784B · 925a | 2.514 ms · 275337B · 33729a | 4.666 ms · 2457134B · 52718a | — | 4.318 ms · 2448019B · 74233a |
| string-build | 745.5 µs · 85408B · 4107a | 616.4 µs · 85408B · 4107a | 1.21× | — | 355.2 µs · 855889B · 5001a | 2.397 ms · 2211274B · 36101a | 2.297 ms · 1078111B · 13859a | 3.127 ms · 1147385B · 26883a | 2.435 ms · 1326244B · 24469a | — | 3.133 ms · 2372800B · 41432a |
| string-compare | 1.096 ms · 21136B · 1284a | 239.9 µs · 21136B · 1284a | 4.57× | — | 64.74 µs · 9632B · 1585a | 1.68 ms · 296281B · 20528a | 1.143 ms · 59712B · 3431a | 2.33 ms · 50744B · 3848a | 1.814 ms · 209521B · 5520a | — | 2.435 ms · 503616B · 31846a |
| struct-tree-walk | 280.2 µs · 768B · 8a | 107.1 µs · 768B · 8a | 2.62× | — | 1.875 ns · 0B · 0a | 645.2 µs · 458378B · 5114a | 1.358 ms · 818512B · 11253a | 1.07 ms · 558960B · 6149a | 3.42 ms · 2570664B · 34797a | — | 1.28 µs · 600B · 17a |
| tail-ping-pong | 29.42 µs · 0B · 0a | 477 ns · 0B · 0a | 61.7× | — | 890.7 ns · 0B · 0a | 106.3 µs · 106112B · 2001a | 35.64 µs · 21856B · 85a | 113.7 µs · 13984B · 1726a | 118.8 µs · 14280B · 1731a | — | 559.8 µs · 348834B · 9016a |
| tail-sum | 29.64 µs · 0B · 0a | 674 ns · 0B · 0a | 44× | — | 302.6 ns · 0B · 0a | 105.4 µs · 106112B · 2001a | 35.07 µs · 21856B · 85a | 113 µs · 13984B · 1726a | 119.8 µs · 14280B · 1731a | — | 85.54 µs · 4608B · 1017a |
| typed-array-sum | 7.44 µs · 0B · 0a | 264.3 ns · 0B · 0a | 28.1× | 266.6 ns · 8B · 1a | 1.876 ns · 0B · 0a | 45.3 µs · 94208B · 513a | 8.299 µs · 4000B · 15a | 27.54 µs · 2080B · 238a | 17.65 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.1 µs · 18424B · 2046a | 1.354 µs · 18424B · 2046a | 82.8× | — | 356.1 ns · 0B · 0a | 101.8 µs · 107608B · 2188a | — | 983.3 µs · 447251B · 12735a | 426.7 µs · 199937B · 7772a | — | 104.4 µs · 13546B · 1317a |

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
