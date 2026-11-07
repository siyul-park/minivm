# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.885 µs · 0B · 0a | 8.881 µs · 0B · 0a | 1× | — | 2.221 µs · 1024B · 128a | 36.28 µs · 96289B · 388a | 16.94 µs · 14376B · 256a | 69.59 µs · 78016B · 770a | 14.12 µs · 5712B · 266a | 5.859 µs · 0B · 0a | 24.37 µs · 1744B · 148a |
| binary-trees | 1.97 ms · 768B · 8a | 1.231 ms · 768B · 8a | 1.6× | — | 286 µs · 201936B · 8414a | 3.869 ms · 1468183B · 62877a | 10.45 ms · 6995677B · 92703a | 8.817 ms · 5198699B · 69052a | 26.95 ms · 19457605B · 280714a | 2.131 ms · 122B · 0a | 30.3 ms · 15127300B · 446011a |
| branch-tree | 1.285 µs · 0B · 0a | 136.2 ns · 0B · 0a | 9.43× | 624.4 ns · 16B · 1a | 160.3 ns · 0B · 0a | 45.29 µs · 95384B · 660a | 15.25 µs · 2464B · 9a | 26.74 µs · 1992B · 196a | 24.61 µs · 2168B · 203a | 9.45 µs · 0B · 0a | 14.91 µs · 1252B · 122a |
| closure-counter | 5.553 µs · 64B · 2a | 944.3 ns · 64B · 2a | 5.88× | — | 39.84 ns · 0B · 0a | 41.97 µs · 92272B · 261a | 12.73 µs · 151B · 3a | 19.27 µs · 1264B · 13a | 77.15 µs · 58312B · 659a | 8.599 µs · 0B · 0a | 88.42 µs · 35049B · 793a |
| fannkuch | 831.1 µs · 34608B · 1442a | 275.2 µs · 34608B · 1442a | 3.02× | — | 34.92 µs · 17280B · 720a | 2.033 ms · 456579B · 25621a | 1.533 ms · 443135B · 1460a | 19.09 ms · 14005345B · 164946a | 3.372 ms · 1367678B · 16944a | 622 µs · 34B · 0a | 4.679 ms · 2491478B · 85959a |
| fnv1a64 | 122.8 µs · 16400B · 2049a | 1.204 µs · 16400B · 2049a | 102× | — | 903.6 ns · 0B · 0a | 160.9 µs · 120808B · 3838a | — | 1.453 ms · 608250B · 18475a | 397.4 µs · 162920B · 7429a | 138.7 µs · 8B · 0a | 102.5 µs · 9105B · 1050a |
| i64-wide-fib | 28.55 ms · 2913456B · 364178a | 860.4 µs · 16B · 1a | 33.2× | — | 239.1 µs · 0B · 0a | 23.49 µs · 90520B · 52a | — | 155.4 ms · 64193200B · 1942305a | 336.8 ms · 253472976B · 4248842a | 34.86 ms · 2448B · 6a | 125.1 ms · 98686581B · 2138708a |
| indirect-recursive-fib | 1.195 ms · 0B · 0a | 76.47 µs · 0B · 0a | 15.6× | 99.31 µs · 8B · 1a | 34.84 µs · 0B · 0a | 2.123 ms · 319347B · 28655a | 1.927 ms · 704B · 2a | 2.552 ms · 4680B · 39a | 11.25 ms · 10158189B · 109494a | 929.6 µs · 51B · 0a | 26.3 ms · 13061262B · 394061a |
| iterative-fib | 1.104 µs · 0B · 0a | 70.98 ns · 0B · 0a | 15.6× | 85.88 ns · 8B · 1a | 11.89 ns · 0B · 0a | 23.64 µs · 90592B · 61a | 1.149 µs · 160B · 0a | 4.275 µs · 368B · 20a | 6.111 µs · 2448B · 88a | 851.8 ns · 0B · 0a | 4.51 µs · 784B · 50a |
| mandelbrot | 291.7 µs · 0B · 0a | 11.1 µs · 0B · 0a | 26.3× | — | 6.347 µs · 0B · 0a | 955.2 µs · 292345B · 25008a | 478.5 µs · 169408B · 661a | 7.82 ms · 5558653B · 104191a | 1.515 ms · 324978B · 23643a | 368.7 µs · 20B · 0a | 1.317 ms · 324088B · 19107a |
| mat-mul | 369.4 µs · 6216B · 6a | 8.501 µs · 6216B · 6a | 43.5× | — | 5.391 µs · 6144B · 3a | 1.377 ms · 431714B · 34871a | 667.7 µs · 143168B · 478a | 1.2 ms · 61304B · 537a | 1.477 ms · 90712B · 9350a | 371.6 µs · 21B · 0a | 617.9 µs · 16875B · 1134a |
| n-body | 607 µs · 504B · 14a | 18.86 µs · 504B · 14a | 32.2× | — | 7.345 µs · 0B · 0a | 1.531 ms · 386898B · 35892a | 1.219 ms · 258913B · 2002a | 3.367 ms · 658435B · 81545a | 2.482 ms · 382714B · 34975a | 498.9 µs · 29B · 0a | 1.621 ms · 400504B · 18248a |
| n-queens | 429.6 µs · 120B · 6a | 33 µs · 120B · 6a | 13× | — | 6.004 µs · 0B · 0a | 974.1 µs · 234865B · 17784a | 568.7 µs · 14423B · 63a | 1.1 ms · 7424B · 35a | 1.433 ms · 410345B · 5147a | 283.9 µs · 16B · 0a | 1.751 ms · 749460B · 29235a |
| permutation-flips | 128.7 µs · 7680B · 128a | 22.74 µs · 7680B · 128a | 5.66× | — | 1.891 ns · 0B · 0a | 549.1 µs · 292857B · 9856a | 231.4 µs · 78808B · 451a | 522.9 µs · 122504B · 765a | 493.6 µs · 111976B · 2368a | 81.51 µs · 4B · 0a | 1.011 µs · 528B · 13a |
| recursive-fib-20 | 709.9 µs · 0B · 0a | 60.91 µs · 0B · 0a | 11.7× | 42.75 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.956 ms · 319345B · 28655a | 2.147 ms · 704B · 2a | 2.962 ms · 4680B · 39a | 11.35 ms · 9807908B · 109494a | 884.4 µs · 49B · 0a | 10.76 ms · 8302736B · 192848a |
| recursive-fib-35 | 965.8 ms · 0B · 0a | 82.82 ms · 0B · 0a | 11.7× | 58.24 ms · 8B · 1a | 29.13 ms · 0B · 0a | 2.667 s · 312797216B · 39088167a | 2.925 s · 971008B · 3793a | 4.027 s · 375360B · 46373a | 15.3 s · 13378014656B · 149349952a | 1.203 s · 19088B · 52a | — |
| sieve | 19.69 µs · 1048B · 2a | 910.3 ns · 1048B · 2a | 21.6× | 1.036 µs · 8B · 1a | 351.1 ns · 0B · 0a | 135.1 µs · 122504B · 1611a | 49.78 µs · 18416B · 44a | 84.19 µs · 1872B · 25a | 76.6 µs · 5704B · 30a | 18.94 µs · 1B · 0a | 40.2 µs · 1256B · 43a |
| sort-stress | 504.7 µs · 5136B · 512a | 12.47 µs · 5136B · 512a | 40.5× | — | 5.789 µs · 1024B · 2a | 1.306 ms · 270945B · 19965a | 842.1 µs · 36567B · 365a | 1.809 ms · 49104B · 3621a | 1.786 ms · 23448B · 2034a | 513.9 µs · 28B · 0a | 880.1 µs · 13121B · 935a |
| spectral-norm | 529.3 µs · 648B · 6a | 34.7 µs · 648B · 6a | 15.3× | — | 7.05 µs · 576B · 3a | 2.06 ms · 571372B · 54822a | 1.073 ms · 234784B · 925a | 2.52 ms · 275338B · 33729a | 4.689 ms · 2457137B · 52718a | 828.2 µs · 46B · 0a | 3.824 ms · 2443907B · 69364a |
| string-build | 736.8 µs · 85408B · 4107a | 562.4 µs · 85408B · 4107a | 1.31× | — | 360.4 µs · 855892B · 5001a | 2.35 ms · 2211275B · 36101a | 2.302 ms · 1078109B · 13859a | 3.112 ms · 1147385B · 26883a | 2.394 ms · 1326241B · 24469a | 553.1 µs · 30B · 0a | 2.913 ms · 2348564B · 38417a |
| struct-tree-walk | 282.3 µs · 768B · 8a | 182.8 µs · 768B · 8a | 1.54× | — | 1.889 ns · 0B · 0a | 626.3 µs · 458380B · 5114a | 1.335 ms · 818511B · 11253a | 1.068 ms · 558960B · 6149a | 3.432 ms · 2570664B · 34797a | 245.1 µs · 14B · 0a | 1.203 µs · 608B · 17a |
| tail-ping-pong | 29.37 µs · 0B · 0a | 518.3 ns · 0B · 0a | 56.7× | — | 890.6 ns · 0B · 0a | 104.8 µs · 106112B · 2001a | 35.33 µs · 21856B · 85a | 109.4 µs · 13984B · 1726a | 119.1 µs · 14280B · 1731a | 46.43 µs · 3B · 0a | 548.8 µs · 348836B · 9016a |
| tail-sum | 29.2 µs · 0B · 0a | 644.3 ns · 0B · 0a | 45.3× | — | 301.6 ns · 0B · 0a | 104.5 µs · 106112B · 2001a | 35.1 µs · 21856B · 85a | 109.6 µs · 13984B · 1726a | 118.7 µs · 14280B · 1731a | 46.57 µs · 3B · 0a | 30.26 µs · 584B · 16a |
| typed-array-sum | 7.333 µs · 0B · 0a | 229.2 ns · 0B · 0a | 32× | 265.9 ns · 8B · 1a | 1.9 ns · 0B · 0a | 43.77 µs · 94208B · 513a | 8.235 µs · 4000B · 15a | 27.28 µs · 2080B · 238a | 17.37 µs · 2496B · 246a | 6.212 µs · 0B · 0a | — |
| xorshift-i64 | 110.3 µs · 18424B · 2046a | 1.351 µs · 18424B · 2046a | 81.7× | — | 360.4 ns · 0B · 0a | 102.3 µs · 107608B · 2188a | — | 988.6 µs · 447247B · 12735a | 424 µs · 199937B · 7772a | 92.05 µs · 5B · 0a | 62.74 µs · 7360B · 547a |

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
