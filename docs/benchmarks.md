# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.891 µs · 0B · 0a | 8.894 µs · 0B · 0a | 5.386 µs · 0B · 0a | 1.65× | — | 2.126 µs · 1024B · 128a | 37.87 µs · 96288B · 388a | 17.02 µs · 14376B · 256a | 69.41 µs · 78017B · 770a | 13.81 µs · 5712B · 266a | — | 30.87 µs · 2308B · 276a |
| binary-trees | 1.954 ms · 768B · 8a | 1.943 ms · 768B · 8a | 590 µs · 768B · 8a | 3.31× | — | 287.6 µs · 201936B · 8414a | 4.156 ms · 1468191B · 62877a | 12.02 ms · 6995712B · 92703a | 9.607 ms · 5198717B · 69052a | 27.32 ms · 19457625B · 280714a | — | 27.98 ms · 15128177B · 446106a |
| branch-tree | 1.408 µs · 0B · 0a | 19.18 ns · 0B · 0a | 88.13 ns · 0B · 0a | 16× | 625.2 ns · 16B · 1a | 164.5 ns · 0B · 0a | 46.34 µs · 95384B · 660a | 15.23 µs · 2464B · 9a | 26.47 µs · 1992B · 196a | 24.2 µs · 2168B · 203a | — | 20.57 µs · 1640B · 219a |
| closure-counter | 5.557 µs · 64B · 2a | 5.514 µs · 64B · 2a | 927.6 ns · 64B · 2a | 5.99× | — | 40.44 ns · 0B · 0a | 42.27 µs · 92272B · 261a | 12.81 µs · 151B · 3a | 19.21 µs · 1264B · 13a | 80.19 µs · 58312B · 659a | — | 88.42 µs · 35629B · 922a |
| coroutine-gen | 79.37 µs · 128B · 2a | 78.99 µs · 128B · 2a | 79.28 µs · 128B · 2a | 1× | — | 42.8 µs · 208B · 7a | — | 327.4 µs · 149936B · 649a | 594 µs · 330650B · 6719a | 199.8 µs · 91800B · 3646a | — | — |
| fannkuch | 832.6 µs · 34608B · 1442a | 813.1 µs · 34608B · 1442a | 222.3 µs · 34608B · 1442a | 3.74× | — | 35.67 µs · 17280B · 720a | 1.986 ms · 456578B · 25621a | 1.542 ms · 443135B · 1460a | 19.16 ms · 14005340B · 164946a | 3.311 ms · 1367677B · 16944a | — | 4.656 ms · 2501427B · 88439a |
| fnv1a64 | 119.7 µs · 16400B · 2049a | 119.5 µs · 16400B · 2049a | 1.165 µs · 16400B · 2049a | 103× | — | 904.2 ns · 0B · 0a | 161.8 µs · 120808B · 3838a | — | 1.437 ms · 608256B · 18475a | 387.4 µs · 162921B · 7429a | — | 153.3 µs · 17282B · 2074a |
| host-call | 466.9 µs · 41752B · 3080a | 477.1 µs · 41752B · 3080a | 480.3 µs · 41752B · 3080a | 0.972× | — | 3.318 µs · 0B · 0a | 181.5 µs · 139264B · 3073a | 127.9 µs · 22536B · 1079a | 773.2 µs · 158048B · 14585a | 106 µs · 14920B · 1799a | — | 555.7 µs · 340638B · 9231a |
| i64-wide-fib | 27.85 ms · 2913440B · 364178a | 20.73 ms · 1942304B · 242785a | 679.9 µs · 16B · 1a | 41× | — | 239.1 µs · 0B · 0a | 23.42 µs · 90520B · 52a | — | 154.1 ms · 64193272B · 1942305a | 347.1 ms · 253472736B · 4248839a | — | 122.8 ms · 98686602B · 2138708a |
| indirect-recursive-fib | 1.191 ms · 0B · 0a | 1.187 ms · 0B · 0a | 60.52 µs · 0B · 0a | 19.7× | 98.94 µs · 8B · 1a | 35.48 µs · 0B · 0a | 2.059 ms · 319345B · 28655a | 1.952 ms · 703B · 2a | 2.545 ms · 4680B · 39a | 11.5 ms · 10158202B · 109494a | — | 24.24 ms · 13061340B · 394061a |
| iterative-fib | 1.131 µs · 0B · 0a | 1.072 µs · 0B · 0a | 84.23 ns · 0B · 0a | 13.4× | 86.86 ns · 8B · 1a | 12.5 ns · 0B · 0a | 26.51 µs · 90592B · 61a | 1.181 µs · 159B · 0a | 4.252 µs · 368B · 20a | 6.229 µs · 2448B · 88a | — | 6.113 µs · 944B · 81a |
| mandelbrot | 290 µs · 0B · 0a | 247.9 µs · 0B · 0a | 11.27 µs · 0B · 0a | 25.7× | — | 6.321 µs · 0B · 0a | 951.8 µs · 292345B · 25008a | 493.5 µs · 169408B · 661a | 8.177 ms · 5558652B · 104191a | 1.503 ms · 324978B · 23643a | — | 1.525 ms · 365400B · 22513a |
| map-ops | 70.65 µs · 8816B · 3a | 70.46 µs · 8816B · 3a | 10.91 µs · 8816B · 3a | 6.47× | — | 10.87 µs · 4904B · 3a | — | 277.9 µs · 114328B · 198a | 380.1 µs · 66728B · 4078a | — | — | 1.11 ms · 501524B · 15813a |
| mat-mul | 375.4 µs · 6216B · 6a | 377.2 µs · 6216B · 6a | 8.699 µs · 6216B · 6a | 43.2× | — | 5.641 µs · 6144B · 3a | 1.361 ms · 431714B · 34871a | 678.4 µs · 143168B · 478a | 1.204 ms · 61304B · 537a | 1.473 ms · 90712B · 9350a | — | 927.1 µs · 36615B · 6036a |
| n-body | 615.1 µs · 504B · 14a | 582 µs · 504B · 14a | 19.12 µs · 504B · 14a | 32.2× | — | 7.358 µs · 0B · 0a | 1.509 ms · 386898B · 35892a | 1.221 ms · 258913B · 2002a | 3.245 ms · 658435B · 81545a | 2.416 ms · 382714B · 34975a | — | 1.739 ms · 445471B · 20669a |
| n-queens | 429.6 µs · 120B · 6a | 429.6 µs · 120B · 6a | 31.35 µs · 120B · 6a | 13.7× | — | 6.013 µs · 0B · 0a | 934.6 µs · 234865B · 17784a | 573.6 µs · 14423B · 63a | 1.101 ms · 7424B · 35a | 1.425 ms · 410346B · 5147a | — | 2.003 ms · 766009B · 33371a |
| naive-checksum | 249.4 µs · 0B · 0a | 142.4 µs · 0B · 0a | 8.726 µs · 0B · 0a | 28.6× | — | 2.759 µs · 0B · 0a | 612.2 µs · 235753B · 18206a | 476.1 µs · 174768B · 682a | 859.5 µs · 155640B · 19402a | 775.9 µs · 163816B · 20409a | — | 310.3 µs · 8977B · 2071a |
| permutation-flips | 129 µs · 7680B · 128a | 128.3 µs · 7680B · 128a | 16.9 µs · 7680B · 128a | 7.63× | — | 1.865 ns · 0B · 0a | 565.2 µs · 292857B · 9856a | 234.3 µs · 78808B · 451a | 527 µs · 122504B · 765a | 486 µs · 111976B · 2368a | — | 995.8 ns · 528B · 13a |
| recursive-fib-20 | 710.9 µs · 0B · 0a | 725 µs · 0B · 0a | 47.34 µs · 0B · 0a | 15× | 42.74 µs · 8B · 1a | 21.51 µs · 0B · 0a | 1.919 ms · 319345B · 28655a | 2.177 ms · 704B · 2a | 2.958 ms · 4680B · 39a | 11.42 ms · 9807913B · 109494a | — | 10.53 ms · 8302692B · 192848a |
| recursive-fib-35 | 969 ms · 0B · 0a | 978 ms · 0B · 0a | 63.94 ms · 0B · 0a | 15.2× | 58.23 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.547 s · 312797568B · 39088169a | 2.943 s · 971008B · 3793a | 4.051 s · 375360B · 46373a | 15.49 s · 13378016800B · 149349962a | — | — |
| sieve | 20.31 µs · 1048B · 2a | 20.32 µs · 1048B · 2a | 932.6 ns · 1048B · 2a | 21.8× | 1.015 µs · 8B · 1a | 372 ns · 0B · 0a | 137.8 µs · 122504B · 1611a | 50.67 µs · 18416B · 44a | 84.72 µs · 1872B · 25a | 76.9 µs · 5704B · 30a | — | 81.44 µs · 4368B · 795a |
| sort-stress | 504.6 µs · 5136B · 512a | 508.8 µs · 5136B · 512a | 12.64 µs · 5136B · 512a | 39.9× | — | 5.324 µs · 1024B · 2a | 1.267 ms · 270945B · 19965a | 842.9 µs · 36567B · 365a | 1.781 ms · 49104B · 3621a | 1.789 ms · 23448B · 2034a | — | 929.4 µs · 17473B · 1708a |
| spectral-norm | 535.9 µs · 648B · 6a | 464.8 µs · 648B · 6a | 8.373 µs · 648B · 6a | 64× | — | 7.031 µs · 576B · 3a | 2.001 ms · 571370B · 54822a | 1.083 ms · 234784B · 925a | 2.441 ms · 275337B · 33729a | 4.665 ms · 2457134B · 52718a | — | 4.065 ms · 2448016B · 74233a |
| string-build | 721.3 µs · 85408B · 4107a | 713.2 µs · 85408B · 4107a | 489.8 µs · 85408B · 4107a | 1.47× | — | 368.8 µs · 855890B · 5001a | 2.377 ms · 2211275B · 36101a | 2.311 ms · 1078110B · 13859a | 3.111 ms · 1147384B · 26883a | 2.437 ms · 1326243B · 24469a | — | 3.069 ms · 2372791B · 41432a |
| string-compare | 1.084 ms · 21136B · 1284a | 1.085 ms · 21136B · 1284a | 220.4 µs · 21136B · 1284a | 4.92× | — | 63.84 µs · 9632B · 1585a | 1.646 ms · 296281B · 20528a | 1.128 ms · 59711B · 3431a | 2.34 ms · 50744B · 3848a | 1.775 ms · 209521B · 5520a | — | 2.371 ms · 503612B · 31846a |
| struct-tree-walk | 281.2 µs · 768B · 8a | 281.4 µs · 768B · 8a | 108.3 µs · 768B · 8a | 2.6× | — | 1.864 ns · 0B · 0a | 645.9 µs · 458379B · 5114a | 1.349 ms · 818512B · 11253a | 1.079 ms · 558960B · 6149a | 3.474 ms · 2570665B · 34797a | — | 1.176 µs · 600B · 17a |
| tail-ping-pong | 29.45 µs · 0B · 0a | 29.29 µs · 0B · 0a | 481.8 ns · 0B · 0a | 61.1× | — | 891.4 ns · 0B · 0a | 107.7 µs · 106112B · 2001a | 36.35 µs · 21855B · 85a | 108.9 µs · 13984B · 1726a | 116.6 µs · 14280B · 1731a | — | 541.6 µs · 348833B · 9016a |
| tail-sum | 29.41 µs · 0B · 0a | 29.4 µs · 0B · 0a | 686.6 ns · 0B · 0a | 42.8× | — | 302.6 ns · 0B · 0a | 107.3 µs · 106112B · 2001a | 37.57 µs · 21856B · 85a | 109 µs · 13984B · 1726a | 116.5 µs · 14280B · 1731a | — | 84.27 µs · 4608B · 1017a |
| typed-array-sum | 7.339 µs · 0B · 0a | 7.36 µs · 0B · 0a | 262.8 ns · 0B · 0a | 27.9× | 262.5 ns · 8B · 1a | 1.869 ns · 0B · 0a | 44.69 µs · 94208B · 513a | 8.342 µs · 4000B · 15a | 27.48 µs · 2080B · 238a | 17.25 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.9 µs · 18424B · 2046a | 110.6 µs · 18424B · 2046a | 1.353 µs · 18424B · 2046a | 82× | — | 357.1 ns · 0B · 0a | 104.2 µs · 107608B · 2188a | — | 963.5 µs · 447251B · 12735a | 416.9 µs · 199937B · 7772a | — | 101.2 µs · 13545B · 1317a |

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
