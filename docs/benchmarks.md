# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.845 µs · 0B · 0a | 8.856 µs · 0B · 0a | 0.999× | — | 2.235 µs · 1024B · 128a | 37.3 µs · 96289B · 388a | 16.92 µs · 14376B · 256a | 69.38 µs · 78016B · 770a | 14.33 µs · 5712B · 266a | 5.826 µs · 0B · 0a | 24.23 µs · 1744B · 148a |
| binary-trees | 1.992 ms · 768B · 8a | 1.235 ms · 768B · 8a | 1.61× | — | 304.1 µs · 201936B · 8414a | 4.022 ms · 1468189B · 62877a | 11.18 ms · 6995692B · 92703a | 9.097 ms · 5198701B · 69052a | 28.08 ms · 19457620B · 280714a | 2.133 ms · 120B · 0a | 31.26 ms · 15127296B · 446011a |
| branch-tree | 1.316 µs · 0B · 0a | 134.2 ns · 0B · 0a | 9.81× | 626.3 ns · 16B · 1a | 160.4 ns · 0B · 0a | 46.66 µs · 95384B · 660a | 15.23 µs · 2463B · 9a | 26.6 µs · 1992B · 196a | 24.19 µs · 2168B · 203a | 9.39 µs · 0B · 0a | 15.03 µs · 1252B · 122a |
| closure-counter | 5.513 µs · 64B · 2a | 943.5 ns · 64B · 2a | 5.84× | — | 39.88 ns · 0B · 0a | 41.8 µs · 92272B · 261a | 13.07 µs · 152B · 3a | 19.34 µs · 1264B · 13a | 77.74 µs · 58312B · 659a | 8.615 µs · 0B · 0a | 87.4 µs · 35048B · 793a |
| fannkuch | 833.9 µs · 34608B · 1442a | 274.6 µs · 34608B · 1442a | 3.04× | — | 35.43 µs · 17280B · 720a | 2.059 ms · 456579B · 25621a | 1.531 ms · 443136B · 1460a | 19.47 ms · 14005347B · 164946a | 3.352 ms · 1367678B · 16944a | 619.7 µs · 34B · 0a | 4.694 ms · 2491463B · 85959a |
| fnv1a64 | 125.3 µs · 16400B · 2049a | 1.207 µs · 16400B · 2049a | 104× | — | 904.6 ns · 0B · 0a | 162.4 µs · 120808B · 3838a | — | 1.477 ms · 608255B · 18475a | 394.7 µs · 162921B · 7429a | 137 µs · 8B · 0a | 103.5 µs · 9105B · 1050a |
| i64-wide-fib | 28.61 ms · 2913456B · 364178a | 860.3 µs · 16B · 1a | 33.3× | — | 239.3 µs · 0B · 0a | 23.25 µs · 90520B · 52a | — | 160.5 ms · 64193264B · 1942305a | 344.6 ms · 253472512B · 4248839a | 34.63 ms · 2388B · 6a | 128.2 ms · 98686576B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 76.38 µs · 0B · 0a | 15.5× | 98.96 µs · 8B · 1a | 35.15 µs · 0B · 0a | 2.112 ms · 319345B · 28655a | 1.92 ms · 703B · 2a | 2.561 ms · 4680B · 39a | 11.49 ms · 10158184B · 109494a | 927.5 µs · 55B · 0a | 25.85 ms · 13061333B · 394061a |
| iterative-fib | 1.106 µs · 0B · 0a | 70.45 ns · 0B · 0a | 15.7× | 87.1 ns · 8B · 1a | 11.9 ns · 0B · 0a | 24.79 µs · 90592B · 61a | 1.154 µs · 160B · 0a | 4.298 µs · 368B · 20a | 6.239 µs · 2448B · 88a | 873.8 ns · 0B · 0a | 4.529 µs · 784B · 50a |
| mandelbrot | 291.7 µs · 0B · 0a | 11.1 µs · 0B · 0a | 26.3× | — | 6.327 µs · 0B · 0a | 955 µs · 292345B · 25008a | 484 µs · 169408B · 661a | 8.166 ms · 5558646B · 104191a | 1.517 ms · 324978B · 23643a | 368.2 µs · 20B · 0a | 1.304 ms · 324084B · 19107a |
| mat-mul | 368.2 µs · 6216B · 6a | 8.685 µs · 6216B · 6a | 42.4× | — | 5.73 µs · 6144B · 3a | 1.386 ms · 431714B · 34871a | 672.8 µs · 143168B · 478a | 1.23 ms · 61304B · 537a | 1.463 ms · 90712B · 9350a | 371.8 µs · 21B · 0a | 622.1 µs · 16874B · 1134a |
| n-body | 609.2 µs · 504B · 14a | 19.1 µs · 504B · 14a | 31.9× | — | 7.345 µs · 0B · 0a | 1.535 ms · 386898B · 35892a | 1.219 ms · 258913B · 2002a | 3.346 ms · 658436B · 81545a | 2.448 ms · 382715B · 34975a | 499.4 µs · 28B · 0a | 1.638 ms · 400503B · 18248a |
| n-queens | 427.5 µs · 120B · 6a | 33 µs · 120B · 6a | 13× | — | 6.016 µs · 0B · 0a | 956 µs · 234865B · 17784a | 566.8 µs · 14423B · 63a | 1.099 ms · 7424B · 35a | 1.457 ms · 410346B · 5147a | 284.3 µs · 16B · 0a | 1.77 ms · 749460B · 29235a |
| permutation-flips | 128.6 µs · 7680B · 128a | 23.06 µs · 7680B · 128a | 5.58× | — | 1.889 ns · 0B · 0a | 554.5 µs · 292858B · 9856a | 232.9 µs · 78808B · 451a | 533.2 µs · 122504B · 765a | 495.6 µs · 111976B · 2368a | 81.52 µs · 4B · 0a | 1.03 µs · 528B · 13a |
| recursive-fib-20 | 709.6 µs · 0B · 0a | 60.89 µs · 0B · 0a | 11.7× | 42.75 µs · 8B · 1a | 21.5 µs · 0B · 0a | 1.995 ms · 319346B · 28655a | 2.173 ms · 704B · 2a | 2.965 ms · 4680B · 39a | 11.5 ms · 9807933B · 109494a | 911.1 µs · 49B · 0a | 10.71 ms · 8302683B · 192847a |
| recursive-fib-35 | 961.6 ms · 0B · 0a | 82.79 ms · 0B · 0a | 11.6× | 58.22 ms · 8B · 1a | 29.13 ms · 0B · 0a | 2.666 s · 312797808B · 39088171a | 2.956 s · 971008B · 3793a | 4.033 s · 375360B · 46373a | 15.65 s · 13378020416B · 149350012a | 1.212 s · 19088B · 52a | — |
| sieve | 19.93 µs · 1048B · 2a | 920.6 ns · 1048B · 2a | 21.6× | 1.049 µs · 8B · 1a | 351.1 ns · 0B · 0a | 139.9 µs · 122504B · 1611a | 50.78 µs · 18416B · 44a | 84.53 µs · 1872B · 25a | 77.46 µs · 5704B · 30a | 18.86 µs · 1B · 0a | 40.9 µs · 1256B · 43a |
| sort-stress | 503.2 µs · 5136B · 512a | 12.47 µs · 5136B · 512a | 40.4× | — | 5.917 µs · 1024B · 2a | 1.318 ms · 270945B · 19965a | 846 µs · 36567B · 365a | 1.796 ms · 49104B · 3621a | 1.803 ms · 23448B · 2034a | 514.9 µs · 28B · 0a | 879.2 µs · 13122B · 935a |
| spectral-norm | 528.7 µs · 648B · 6a | 34.82 µs · 648B · 6a | 15.2× | — | 7.094 µs · 576B · 3a | 2.106 ms · 571371B · 54822a | 1.09 ms · 234784B · 925a | 2.477 ms · 275337B · 33729a | 4.744 ms · 2457132B · 52718a | 826.6 µs · 46B · 0a | 3.929 ms · 2443897B · 69364a |
| string-build | 738 µs · 85408B · 4107a | 600.9 µs · 85408B · 4107a | 1.23× | — | 406 µs · 855890B · 5001a | 2.472 ms · 2211270B · 36101a | 2.354 ms · 1078110B · 13859a | 3.168 ms · 1147384B · 26883a | 2.452 ms · 1326242B · 24469a | 551.8 µs · 30B · 0a | 2.989 ms · 2348555B · 38417a |
| struct-tree-walk | 281.2 µs · 768B · 8a | 184.9 µs · 768B · 8a | 1.52× | — | 1.891 ns · 0B · 0a | 660.2 µs · 458380B · 5114a | 1.376 ms · 818512B · 11253a | 1.081 ms · 558960B · 6149a | 3.456 ms · 2570666B · 34797a | 244.9 µs · 13B · 0a | 1.207 µs · 608B · 17a |
| tail-ping-pong | 29.13 µs · 0B · 0a | 515 ns · 0B · 0a | 56.6× | — | 891.2 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 35.99 µs · 21856B · 85a | 113.7 µs · 13984B · 1726a | 118.2 µs · 14280B · 1731a | 46.38 µs · 2B · 0a | 550.8 µs · 348833B · 9016a |
| tail-sum | 29.48 µs · 0B · 0a | 646.4 ns · 0B · 0a | 45.6× | — | 302.1 ns · 0B · 0a | 108.3 µs · 106112B · 2001a | 35.97 µs · 21856B · 85a | 113.3 µs · 13984B · 1726a | 117.8 µs · 14280B · 1731a | 46.35 µs · 3B · 0a | 30.04 µs · 584B · 16a |
| typed-array-sum | 7.224 µs · 0B · 0a | 224.2 ns · 0B · 0a | 32.2× | 268.2 ns · 8B · 1a | 1.899 ns · 0B · 0a | 45.34 µs · 94208B · 513a | 8.351 µs · 4000B · 15a | 27.66 µs · 2080B · 238a | 17.63 µs · 2496B · 246a | 6.211 µs · 0B · 0a | — |
| xorshift-i64 | 110.9 µs · 18424B · 2046a | 1.387 µs · 18424B · 2046a | 80× | — | 360.2 ns · 0B · 0a | 103.8 µs · 107608B · 2188a | — | 999.8 µs · 447249B · 12735a | 429.6 µs · 199937B · 7772a | 92.02 µs · 5B · 0a | 63.37 µs · 7360B · 547a |

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
