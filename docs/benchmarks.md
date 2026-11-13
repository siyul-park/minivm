# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.863 µs · 0B · 0a | 5.336 µs · 0B · 0a | 1.66× | — | 2.23 µs · 1024B · 128a | 36.66 µs · 96289B · 388a | 17.1 µs · 14376B · 256a | 69.35 µs · 78016B · 770a | 14.45 µs · 5712B · 266a | — | 31.26 µs · 2308B · 276a |
| binary-trees | 1.947 ms · 768B · 8a | 591.5 µs · 768B · 8a | 3.29× | — | 285.2 µs · 201936B · 8414a | 3.875 ms · 1468184B · 62877a | 11.21 ms · 6995695B · 92703a | 10.09 ms · 5198716B · 69052a | 28.73 ms · 19457708B · 280715a | — | 30.09 ms · 15128195B · 446106a |
| branch-tree | 1.32 µs · 0B · 0a | 153.4 ns · 0B · 0a | 8.6× | 625.3 ns · 16B · 1a | 160.5 ns · 0B · 0a | 45.12 µs · 95384B · 660a | 15.23 µs · 2464B · 9a | 26.96 µs · 1992B · 196a | 24.47 µs · 2168B · 203a | — | 20.69 µs · 1640B · 219a |
| closure-counter | 5.502 µs · 64B · 2a | 913.9 ns · 64B · 2a | 6.02× | — | 40.37 ns · 0B · 0a | 41.33 µs · 92272B · 261a | 12.95 µs · 152B · 3a | 19.29 µs · 1264B · 13a | 77.11 µs · 58312B · 659a | — | 94.61 µs · 35629B · 922a |
| coroutine-gen | 78.65 µs · 128B · 2a | 78.59 µs · 128B · 2a | 1× | — | 42.11 µs · 208B · 7a | — | 318 µs · 149936B · 649a | 601.7 µs · 330650B · 6719a | 196 µs · 91800B · 3646a | — | — |
| fannkuch | 832.8 µs · 34608B · 1442a | 225.1 µs · 34608B · 1442a | 3.7× | — | 35.57 µs · 17280B · 720a | 2.016 ms · 456579B · 25621a | 1.541 ms · 443135B · 1460a | 18.99 ms · 14005385B · 164946a | 3.32 ms · 1367680B · 16944a | — | 4.917 ms · 2501449B · 88439a |
| fnv1a64 | 126.3 µs · 16400B · 2049a | 1.159 µs · 16400B · 2049a | 109× | — | 902.4 ns · 0B · 0a | 160.8 µs · 120808B · 3838a | — | 1.477 ms · 608251B · 18475a | 397.9 µs · 162921B · 7429a | — | 157.2 µs · 17281B · 2074a |
| host-call | 484 µs · 41752B · 3080a | 485.2 µs · 41752B · 3080a | 0.998× | — | 3.312 µs · 0B · 0a | 178.3 µs · 139264B · 3073a | 133.4 µs · 22536B · 1079a | 824.1 µs · 158048B · 14585a | 106.5 µs · 14920B · 1799a | — | 604.9 µs · 340641B · 9231a |
| i64-wide-fib | 28.76 ms · 2913456B · 364178a | 680.7 µs · 16B · 1a | 42.3× | — | 240 µs · 0B · 0a | 22.82 µs · 90520B · 52a | — | 160 ms · 64193280B · 1942305a | 344.5 ms · 253472560B · 4248839a | — | 131.9 ms · 98686549B · 2138708a |
| indirect-recursive-fib | 1.183 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.6× | 98.82 µs · 8B · 1a | 34.83 µs · 0B · 0a | 2.077 ms · 319346B · 28655a | 1.941 ms · 704B · 2a | 2.542 ms · 4680B · 39a | 11.45 ms · 10158187B · 109494a | — | 26.36 ms · 13061332B · 394061a |
| iterative-fib | 1.128 µs · 0B · 0a | 83.17 ns · 0B · 0a | 13.6× | 86.35 ns · 8B · 1a | 12.45 ns · 0B · 0a | 23.74 µs · 90592B · 61a | 1.154 µs · 160B · 0a | 4.271 µs · 368B · 20a | 6.147 µs · 2448B · 88a | — | 6.32 µs · 944B · 81a |
| mandelbrot | 292.3 µs · 0B · 0a | 11.26 µs · 0B · 0a | 26× | — | 6.321 µs · 0B · 0a | 963.9 µs · 292346B · 25008a | 480 µs · 169408B · 661a | 8.098 ms · 5558653B · 104191a | 1.507 ms · 324978B · 23643a | — | 1.576 ms · 365402B · 22513a |
| map-ops | 69.39 µs · 8816B · 3a | 11.07 µs · 8816B · 3a | 6.27× | — | 10.74 µs · 4904B · 3a | — | 278.3 µs · 114328B · 198a | 383.8 µs · 66728B · 4078a | — | — | 1.198 ms · 501523B · 15813a |
| mat-mul | 373.8 µs · 6216B · 6a | 8.498 µs · 6216B · 6a | 44× | — | 5.514 µs · 6144B · 3a | 1.389 ms · 431714B · 34871a | 669.9 µs · 143168B · 478a | 1.216 ms · 61304B · 537a | 1.477 ms · 90712B · 9350a | — | 925.3 µs · 36613B · 6036a |
| n-body | 613.2 µs · 504B · 14a | 18.97 µs · 504B · 14a | 32.3× | — | 7.358 µs · 0B · 0a | 1.516 ms · 386899B · 35892a | 1.206 ms · 258914B · 2002a | 3.348 ms · 658436B · 81545a | 2.462 ms · 382714B · 34975a | — | 1.768 ms · 445461B · 20669a |
| n-queens | 427.6 µs · 120B · 6a | 31.5 µs · 120B · 6a | 13.6× | — | 6.028 µs · 0B · 0a | 966.1 µs · 234865B · 17784a | 570.1 µs · 14423B · 63a | 1.095 ms · 7424B · 35a | 1.448 ms · 410346B · 5147a | — | 2.004 ms · 766003B · 33371a |
| permutation-flips | 129.7 µs · 7680B · 128a | 17.26 µs · 7680B · 128a | 7.51× | — | 1.876 ns · 0B · 0a | 558.7 µs · 292857B · 9856a | 237.1 µs · 78808B · 451a | 531.1 µs · 122504B · 765a | 491.6 µs · 111976B · 2368a | — | 1.069 µs · 528B · 13a |
| recursive-fib-20 | 714.1 µs · 0B · 0a | 47.18 µs · 0B · 0a | 15.1× | 42.77 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.951 ms · 319345B · 28655a | 2.177 ms · 704B · 2a | 2.963 ms · 4680B · 39a | 11.03 ms · 9807908B · 109494a | — | 11.37 ms · 8302698B · 192848a |
| recursive-fib-35 | 986.1 ms · 0B · 0a | 63.87 ms · 0B · 0a | 15.4× | 58.23 ms · 8B · 1a | 29.23 ms · 0B · 0a | 2.649 s · 312797456B · 39088168a | 2.967 s · 971008B · 3793a | 4.039 s · 375360B · 46373a | 15.34 s · 13378018272B · 149349967a | — | — |
| sieve | 20.09 µs · 1048B · 2a | 915.2 ns · 1048B · 2a | 22× | 1.026 µs · 8B · 1a | 372.1 ns · 0B · 0a | 136.4 µs · 122504B · 1611a | 50.06 µs · 18416B · 44a | 84.37 µs · 1872B · 25a | 77.07 µs · 5704B · 30a | — | 81.42 µs · 4368B · 795a |
| sort-stress | 505.5 µs · 5136B · 512a | 12.58 µs · 5136B · 512a | 40.2× | — | 5.253 µs · 1024B · 2a | 1.285 ms · 270945B · 19965a | 862.4 µs · 36568B · 365a | 1.793 ms · 49104B · 3621a | 1.787 ms · 23448B · 2034a | — | 928.2 µs · 17474B · 1708a |
| spectral-norm | 535.5 µs · 648B · 6a | 8.891 µs · 648B · 6a | 60.2× | — | 7.035 µs · 576B · 3a | 2.066 ms · 571370B · 54822a | 1.085 ms · 234784B · 925a | 2.496 ms · 275337B · 33729a | 4.587 ms · 2457134B · 52718a | — | 4.269 ms · 2448023B · 74233a |
| string-build | 742.9 µs · 85408B · 4107a | 500.4 µs · 85408B · 4107a | 1.48× | — | 358.1 µs · 855891B · 5001a | 2.363 ms · 2211276B · 36101a | 2.299 ms · 1078111B · 13859a | 3.071 ms · 1147384B · 26883a | 2.397 ms · 1326242B · 24469a | — | 3.057 ms · 2372779B · 41432a |
| string-compare | 1.089 ms · 21136B · 1284a | 239.6 µs · 21136B · 1284a | 4.55× | — | 64.59 µs · 9632B · 1585a | 1.699 ms · 296282B · 20528a | 1.144 ms · 59712B · 3431a | 2.315 ms · 50744B · 3848a | 1.828 ms · 209521B · 5520a | — | 2.435 ms · 503620B · 31846a |
| struct-tree-walk | 279.2 µs · 768B · 8a | 108.1 µs · 768B · 8a | 2.58× | — | 1.887 ns · 0B · 0a | 657.1 µs · 458379B · 5114a | 1.367 ms · 818512B · 11253a | 1.089 ms · 558960B · 6149a | 3.48 ms · 2570665B · 34797a | — | 1.297 µs · 600B · 17a |
| tail-ping-pong | 28.84 µs · 0B · 0a | 482.7 ns · 0B · 0a | 59.7× | — | 890.7 ns · 0B · 0a | 108.7 µs · 106112B · 2001a | 36.27 µs · 21856B · 85a | 115.2 µs · 13984B · 1726a | 120.3 µs · 14280B · 1731a | — | 556.8 µs · 348836B · 9016a |
| tail-sum | 28.8 µs · 0B · 0a | 678.8 ns · 0B · 0a | 42.4× | — | 305.7 ns · 0B · 0a | 110.7 µs · 106112B · 2001a | 35.73 µs · 21855B · 85a | 113.6 µs · 13984B · 1726a | 118.6 µs · 14280B · 1731a | — | 84.26 µs · 4608B · 1017a |
| typed-array-sum | 7.281 µs · 0B · 0a | 266.4 ns · 0B · 0a | 27.3× | 266.8 ns · 8B · 1a | 1.876 ns · 0B · 0a | 50.97 µs · 94208B · 513a | 8.337 µs · 4000B · 15a | 27.3 µs · 2080B · 238a | 17.45 µs · 2496B · 246a | — | — |
| xorshift-i64 | 114.4 µs · 18424B · 2046a | 1.373 µs · 18424B · 2046a | 83.3× | — | 356.1 ns · 0B · 0a | 102.3 µs · 107608B · 2188a | — | 1.013 ms · 447254B · 12736a | 421.7 µs · 199938B · 7772a | — | 102.8 µs · 13545B · 1317a |

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
