# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.91 µs · 0B · 0a | 5.39 µs · 0B · 0a | 1.65× | — | 2.315 µs · 1024B · 128a | 42.6 µs · 96288B · 388a | 20.24 µs · 14376B · 256a | 73.3 µs · 78016B · 770a | 14.19 µs · 5712B · 266a | — | 31.57 µs · 2308B · 276a |
| binary-trees | 1.959 ms · 768B · 8a | 593.1 µs · 768B · 8a | 3.3× | — | 290.2 µs · 201936B · 8414a | 3.859 ms · 1468183B · 62877a | 10.83 ms · 6995680B · 92703a | 9.083 ms · 5198695B · 69052a | 28.21 ms · 19457581B · 280714a | — | 30.64 ms · 15128180B · 446106a |
| branch-tree | 1.349 µs · 0B · 0a | 148.2 ns · 0B · 0a | 9.1× | 627.1 ns · 16B · 1a | 160.5 ns · 0B · 0a | 48.33 µs · 95384B · 660a | 15.44 µs · 2463B · 9a | 26.81 µs · 1992B · 196a | 24.39 µs · 2168B · 203a | — | 20.68 µs · 1640B · 219a |
| closure-counter | 5.533 µs · 64B · 2a | 933.8 ns · 64B · 2a | 5.93× | — | 40.41 ns · 0B · 0a | 41.52 µs · 92272B · 261a | 13.01 µs · 151B · 3a | 19.16 µs · 1264B · 13a | 80.99 µs · 58312B · 659a | — | 94.3 µs · 35628B · 922a |
| coroutine-gen | 77.06 µs · 128B · 2a | 76.58 µs · 128B · 2a | 1.01× | — | 42.19 µs · 208B · 7a | — | 333.8 µs · 149936B · 649a | 605.5 µs · 330650B · 6719a | 194 µs · 91800B · 3646a | — | — |
| fannkuch | 861.6 µs · 34608B · 1442a | 233.7 µs · 34608B · 1442a | 3.69× | — | 35.68 µs · 17280B · 720a | 2.042 ms · 456578B · 25621a | 1.551 ms · 443136B · 1460a | 19.42 ms · 14005336B · 164946a | 3.383 ms · 1367678B · 16944a | — | 4.864 ms · 2501430B · 88439a |
| fnv1a64 | 124.1 µs · 16400B · 2049a | 1.153 µs · 16400B · 2049a | 108× | — | 902.7 ns · 0B · 0a | 174.3 µs · 120808B · 3838a | — | 1.494 ms · 608254B · 18475a | 404.4 µs · 162921B · 7429a | — | 155 µs · 17282B · 2074a |
| host-call | 732.8 µs · 196592B · 14073a | 743.7 µs · 196592B · 14073a | 0.985× | — | 3.313 µs · 0B · 0a | 181.2 µs · 139264B · 3073a | 132.7 µs · 22536B · 1079a | 804.2 µs · 158048B · 14585a | 106.6 µs · 14920B · 1799a | — | 605.6 µs · 340641B · 9231a |
| i64-wide-fib | 28.96 ms · 2913456B · 364178a | 679.7 µs · 16B · 1a | 42.6× | — | 239.1 µs · 0B · 0a | 22.71 µs · 90520B · 52a | — | 161.7 ms · 64193312B · 1942305a | 347.3 ms · 253472736B · 4248840a | — | 132.2 ms · 98686650B · 2138708a |
| indirect-recursive-fib | 1.187 ms · 0B · 0a | 60.49 µs · 0B · 0a | 19.6× | 99.78 µs · 8B · 1a | 35.07 µs · 0B · 0a | 2.127 ms · 319346B · 28655a | 1.945 ms · 704B · 2a | 2.548 ms · 4680B · 39a | 11.68 ms · 10158199B · 109494a | — | 26.03 ms · 13061311B · 394061a |
| iterative-fib | 1.119 µs · 0B · 0a | 79.59 ns · 0B · 0a | 14.1× | 85.37 ns · 8B · 1a | 12.45 ns · 0B · 0a | 23.97 µs · 90592B · 61a | 1.169 µs · 160B · 0a | 4.266 µs · 368B · 20a | 6.179 µs · 2448B · 88a | — | 6.2 µs · 944B · 81a |
| mandelbrot | 294 µs · 0B · 0a | 10.91 µs · 0B · 0a | 26.9× | — | 6.323 µs · 0B · 0a | 970.7 µs · 292345B · 25008a | 492.3 µs · 169408B · 661a | 8.251 ms · 5558647B · 104191a | 1.515 ms · 324978B · 23643a | — | 1.535 ms · 365400B · 22513a |
| map-ops | 70.66 µs · 8816B · 3a | 70.48 µs · 8816B · 3a | 1× | — | 10.99 µs · 4904B · 3a | — | 282.3 µs · 114328B · 198a | 391.4 µs · 66728B · 4078a | — | — | 1.193 ms · 501524B · 15813a |
| mat-mul | 372.6 µs · 6216B · 6a | 8.659 µs · 6216B · 6a | 43× | — | 5.739 µs · 6144B · 3a | 1.365 ms · 431714B · 34871a | 679.7 µs · 143168B · 478a | 1.212 ms · 61305B · 537a | 1.466 ms · 90712B · 9350a | — | 925.7 µs · 36613B · 6036a |
| n-body | 610.9 µs · 504B · 14a | 18.59 µs · 504B · 14a | 32.9× | — | 7.357 µs · 0B · 0a | 1.511 ms · 386898B · 35892a | 1.23 ms · 258914B · 2002a | 3.394 ms · 658437B · 81545a | 2.464 ms · 382715B · 34975a | — | 1.814 ms · 445467B · 20669a |
| n-queens | 430.9 µs · 120B · 6a | 31.59 µs · 120B · 6a | 13.6× | — | 6.009 µs · 0B · 0a | 970.7 µs · 234865B · 17784a | 572.7 µs · 14423B · 63a | 1.094 ms · 7424B · 35a | 1.455 ms · 410345B · 5147a | — | 2.042 ms · 766013B · 33371a |
| permutation-flips | 129.6 µs · 7680B · 128a | 17.52 µs · 7680B · 128a | 7.4× | — | 1.876 ns · 0B · 0a | 573.2 µs · 292857B · 9856a | 240 µs · 78808B · 451a | 530.8 µs · 122504B · 765a | 500.6 µs · 111976B · 2368a | — | 1.053 µs · 528B · 13a |
| recursive-fib-20 | 712.8 µs · 0B · 0a | 47.2 µs · 0B · 0a | 15.1× | 42.77 µs · 8B · 1a | 21.51 µs · 0B · 0a | 1.984 ms · 319345B · 28655a | 2.189 ms · 704B · 2a | 2.951 ms · 4680B · 39a | 11.87 ms · 9807923B · 109494a | — | 11.9 ms · 8302717B · 192848a |
| recursive-fib-35 | 973.5 ms · 0B · 0a | 63.89 ms · 0B · 0a | 15.2× | 58.21 ms · 8B · 1a | 29.05 ms · 0B · 0a | 2.656 s · 312797376B · 39088168a | 3.034 s · 971008B · 3793a | 4.035 s · 375360B · 46373a | 15.8 s · 13378017440B · 149349997a | — | — |
| sieve | 20.07 µs · 1048B · 2a | 944.5 ns · 1048B · 2a | 21.2× | 1.016 µs · 8B · 1a | 371.7 ns · 0B · 0a | 141.4 µs · 122504B · 1611a | 51.2 µs · 18416B · 44a | 84.3 µs · 1872B · 25a | 78.34 µs · 5704B · 30a | — | 81.44 µs · 4368B · 795a |
| sort-stress | 506.4 µs · 5136B · 512a | 12.66 µs · 5136B · 512a | 40× | — | 5.378 µs · 1024B · 2a | 1.287 ms · 270945B · 19965a | 848.9 µs · 36568B · 365a | 1.795 ms · 49104B · 3621a | 1.783 ms · 23448B · 2034a | — | 923.2 µs · 17473B · 1708a |
| spectral-norm | 531.5 µs · 648B · 6a | 8.38 µs · 648B · 6a | 63.4× | — | 7.112 µs · 576B · 3a | 2.098 ms · 571371B · 54822a | 1.085 ms · 234784B · 925a | 2.507 ms · 275337B · 33729a | 4.769 ms · 2457131B · 52718a | — | 4.427 ms · 2448025B · 74233a |
| string-build | 744.6 µs · 85408B · 4107a | 620.7 µs · 85408B · 4107a | 1.2× | — | 401.6 µs · 855890B · 5001a | 2.495 ms · 2211272B · 36101a | 2.345 ms · 1078110B · 13859a | 3.182 ms · 1147385B · 26883a | 2.483 ms · 1326242B · 24469a | — | 3.164 ms · 2372789B · 41432a |
| string-compare | 1.09 ms · 21136B · 1284a | 240.5 µs · 21136B · 1284a | 4.53× | — | 64.98 µs · 9632B · 1585a | 1.696 ms · 296282B · 20528a | 1.136 ms · 59712B · 3431a | 2.337 ms · 50744B · 3848a | 1.856 ms · 209521B · 5520a | — | 2.405 ms · 503614B · 31846a |
| struct-tree-walk | 282.6 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.62× | — | 1.877 ns · 0B · 0a | 659 µs · 458378B · 5114a | 1.373 ms · 818513B · 11253a | 1.091 ms · 558961B · 6149a | 3.526 ms · 2570669B · 34797a | — | 1.243 µs · 600B · 17a |
| tail-ping-pong | 28.83 µs · 0B · 0a | 477.8 ns · 0B · 0a | 60.3× | — | 890.8 ns · 0B · 0a | 109.3 µs · 106112B · 2001a | 36.16 µs · 21856B · 85a | 113.8 µs · 13984B · 1726a | 119.4 µs · 14280B · 1731a | — | 572.9 µs · 348835B · 9016a |
| tail-sum | 28.91 µs · 0B · 0a | 674.1 ns · 0B · 0a | 42.9× | — | 302.6 ns · 0B · 0a | 109.3 µs · 106112B · 2001a | 36.17 µs · 21856B · 85a | 113.8 µs · 13984B · 1726a | 119.8 µs · 14280B · 1731a | — | 84.65 µs · 4608B · 1017a |
| typed-array-sum | 7.314 µs · 0B · 0a | 262.3 ns · 0B · 0a | 27.9× | 265.4 ns · 8B · 1a | 1.876 ns · 0B · 0a | 45.6 µs · 94208B · 513a | 8.449 µs · 4000B · 15a | 27.4 µs · 2080B · 238a | 17.51 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.9 µs · 18424B · 2046a | 1.393 µs · 18424B · 2046a | 81× | — | 357 ns · 0B · 0a | 103.6 µs · 107608B · 2188a | — | 992.7 µs · 447250B · 12735a | 441.2 µs · 199938B · 7772a | — | 102.7 µs · 13545B · 1317a |

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
