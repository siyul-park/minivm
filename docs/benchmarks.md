# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.961 µs · 0B · 0a | 8.911 µs · 0B · 0a | 5.299 µs · 0B · 0a | 1.69× | — | 2.183 µs · 1024B · 128a | 39.85 µs · 96288B · 388a | 17.99 µs · 14376B · 256a | 76.37 µs · 78017B · 770a | 15.69 µs · 5712B · 266a | — | 31.43 µs · 2308B · 276a |
| binary-trees | 1.964 ms · 768B · 8a | 1.967 ms · 768B · 8a | 594 µs · 768B · 8a | 3.31× | — | 301.2 µs · 201936B · 8414a | 3.926 ms · 1468185B · 62877a | 10.98 ms · 6995682B · 92703a | 9.236 ms · 5198712B · 69052a | 28.63 ms · 19457668B · 280714a | — | 29.33 ms · 15128176B · 446106a |
| branch-tree | 1.351 µs · 0B · 0a | 19.84 ns · 0B · 0a | 88.03 ns · 0B · 0a | 15.3× | 634.9 ns · 16B · 1a | 161 ns · 0B · 0a | 48.53 µs · 95384B · 660a | 15.48 µs · 2464B · 9a | 26.88 µs · 1992B · 196a | 24.52 µs · 2168B · 203a | — | 20.63 µs · 1640B · 219a |
| closure-counter | 5.529 µs · 64B · 2a | 5.522 µs · 64B · 2a | 908.6 ns · 64B · 2a | 6.09× | — | 40.54 ns · 0B · 0a | 41.58 µs · 92272B · 261a | 13.26 µs · 151B · 3a | 18.85 µs · 1264B · 13a | 82.08 µs · 58312B · 659a | — | 88.34 µs · 35628B · 922a |
| coroutine-gen | 79.16 µs · 128B · 2a | 79.05 µs · 128B · 2a | 78.92 µs · 128B · 2a | 1× | — | 42.6 µs · 208B · 7a | — | 331 µs · 149936B · 649a | 605.1 µs · 330649B · 6719a | 204.4 µs · 91800B · 3646a | — | — |
| fannkuch | 832.2 µs · 34608B · 1442a | 813.6 µs · 34608B · 1442a | 211.7 µs · 34608B · 1442a | 3.93× | — | 35.96 µs · 17280B · 720a | 2.029 ms · 456579B · 25621a | 1.563 ms · 443136B · 1460a | 19.66 ms · 14005344B · 164946a | 3.379 ms · 1367679B · 16944a | — | 4.727 ms · 2501444B · 88439a |
| fnv1a64 | 122.4 µs · 16400B · 2049a | 122.4 µs · 16400B · 2049a | 1.147 µs · 16400B · 2049a | 107× | — | 902.3 ns · 0B · 0a | 165.9 µs · 120808B · 3838a | — | 1.351 ms · 608254B · 18475a | 382 µs · 162921B · 7429a | — | 152.8 µs · 17282B · 2074a |
| host-call | 456.5 µs · 41752B · 3080a | 470.3 µs · 41752B · 3080a | 466.9 µs · 41752B · 3080a | 0.978× | — | 3.312 µs · 0B · 0a | 185 µs · 139264B · 3073a | 129.7 µs · 22536B · 1079a | 775.6 µs · 158048B · 14585a | 104.2 µs · 14920B · 1799a | — | 562.5 µs · 340639B · 9231a |
| i64-wide-fib | 27.89 ms · 2913456B · 364178a | 20.81 ms · 1942304B · 242785a | 679.7 µs · 16B · 1a | 41× | — | 239.8 µs · 0B · 0a | 24.56 µs · 90520B · 52a | — | 156.6 ms · 64193336B · 1942305a | 355.9 ms · 253472560B · 4248839a | — | 129.2 ms · 98686592B · 2138708a |
| indirect-recursive-fib | 1.191 ms · 0B · 0a | 1.186 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.7× | 99.5 µs · 8B · 1a | 34.79 µs · 0B · 0a | 2.09 ms · 319345B · 28655a | 1.948 ms · 703B · 2a | 2.534 ms · 4680B · 39a | 11.81 ms · 10158202B · 109494a | — | 23.93 ms · 13061286B · 394061a |
| iterative-fib | 1.137 µs · 0B · 0a | 1.074 µs · 0B · 0a | 84.49 ns · 0B · 0a | 13.5× | 84.21 ns · 8B · 1a | 12.41 ns · 0B · 0a | 24.47 µs · 90592B · 61a | 1.203 µs · 159B · 0a | 4.254 µs · 368B · 20a | 6.164 µs · 2448B · 88a | — | 6.175 µs · 944B · 81a |
| mandelbrot | 291.5 µs · 0B · 0a | 248.2 µs · 0B · 0a | 11.27 µs · 0B · 0a | 25.9× | — | 6.317 µs · 0B · 0a | 952.9 µs · 292345B · 25008a | 482.3 µs · 169408B · 661a | 8.369 ms · 5558648B · 104191a | 1.505 ms · 324978B · 23643a | — | 1.53 ms · 365398B · 22513a |
| map-ops | 70.27 µs · 8816B · 3a | 70.16 µs · 8816B · 3a | 10.82 µs · 8816B · 3a | 6.49× | — | 10.99 µs · 4904B · 3a | — | 278.7 µs · 114327B · 198a | 381.3 µs · 66728B · 4078a | — | — | 1.106 ms · 501517B · 15813a |
| mat-mul | 375.8 µs · 6216B · 6a | 375 µs · 6216B · 6a | 8.694 µs · 6216B · 6a | 43.2× | — | 5.496 µs · 6144B · 3a | 1.383 ms · 431714B · 34871a | 665.5 µs · 143168B · 478a | 1.205 ms · 61304B · 537a | 1.454 ms · 90713B · 9350a | — | 920.4 µs · 36612B · 6036a |
| n-body | 614.3 µs · 504B · 14a | 582.4 µs · 504B · 14a | 18.97 µs · 504B · 14a | 32.4× | — | 7.356 µs · 0B · 0a | 1.514 ms · 386898B · 35892a | 1.225 ms · 258914B · 2002a | 3.293 ms · 658437B · 81545a | 2.397 ms · 382714B · 34975a | — | 1.738 ms · 445464B · 20669a |
| n-queens | 429 µs · 120B · 6a | 438.7 µs · 120B · 6a | 31.56 µs · 120B · 6a | 13.6× | — | 6.009 µs · 0B · 0a | 946.2 µs · 234865B · 17784a | 567.3 µs · 14423B · 63a | 1.102 ms · 7424B · 35a | 1.397 ms · 410346B · 5147a | — | 2.007 ms · 766002B · 33371a |
| naive-checksum | 250 µs · 0B · 0a | 142.6 µs · 0B · 0a | 8.705 µs · 0B · 0a | 28.7× | — | 2.758 µs · 0B · 0a | 626.2 µs · 235753B · 18206a | 504.4 µs · 174767B · 682a | 875.5 µs · 155640B · 19402a | 790.6 µs · 163816B · 20409a | — | 311.6 µs · 8977B · 2071a |
| permutation-flips | 129.6 µs · 7680B · 128a | 129.6 µs · 7680B · 128a | 16.99 µs · 7680B · 128a | 7.63× | — | 1.873 ns · 0B · 0a | 573.4 µs · 292857B · 9856a | 235.4 µs · 78808B · 451a | 538.4 µs · 122504B · 765a | 495.5 µs · 111976B · 2368a | — | 1.006 µs · 528B · 13a |
| recursive-fib-20 | 710.3 µs · 0B · 0a | 718.4 µs · 0B · 0a | 47.15 µs · 0B · 0a | 15.1× | 42.74 µs · 8B · 1a | 21.51 µs · 0B · 0a | 1.927 ms · 319346B · 28655a | 2.2 ms · 703B · 2a | 2.978 ms · 4680B · 39a | 11.67 ms · 9807914B · 109494a | — | 10.81 ms · 8302696B · 192848a |
| recursive-fib-35 | 965.6 ms · 0B · 0a | 978.7 ms · 0B · 0a | 64.33 ms · 0B · 0a | 15× | 58.22 ms · 8B · 1a | 29.14 ms · 0B · 0a | 2.561 s · 312797344B · 39088168a | 2.966 s · 971008B · 3793a | 4.066 s · 375360B · 46373a | 16.12 s · 13378017504B · 149349966a | — | — |
| sieve | 20.39 µs · 1048B · 2a | 20.28 µs · 1048B · 2a | 940.1 ns · 1048B · 2a | 21.7× | 1.041 µs · 8B · 1a | 371.9 ns · 0B · 0a | 140.2 µs · 122504B · 1611a | 50.58 µs · 18416B · 44a | 83.82 µs · 1872B · 25a | 76.84 µs · 5704B · 30a | — | 81.59 µs · 4368B · 795a |
| sort-stress | 505.2 µs · 5136B · 512a | 509.1 µs · 5136B · 512a | 12.82 µs · 5136B · 512a | 39.4× | — | 5.333 µs · 1024B · 2a | 1.3 ms · 270945B · 19965a | 850.8 µs · 36568B · 365a | 1.796 ms · 49104B · 3621a | 1.77 ms · 23448B · 2034a | — | 924.5 µs · 17473B · 1708a |
| spectral-norm | 533.4 µs · 648B · 6a | 460.6 µs · 648B · 6a | 8.373 µs · 648B · 6a | 63.7× | — | 7.034 µs · 576B · 3a | 2.058 ms · 571371B · 54822a | 1.08 ms · 234784B · 925a | 2.481 ms · 275337B · 33729a | 4.707 ms · 2457136B · 52718a | — | 4.149 ms · 2448001B · 74233a |
| string-build | 731 µs · 85408B · 4107a | 715.8 µs · 85408B · 4107a | 471.7 µs · 85408B · 4107a | 1.55× | — | 373.3 µs · 855890B · 5001a | 2.441 ms · 2211272B · 36101a | 2.325 ms · 1078109B · 13859a | 3.159 ms · 1147383B · 26883a | 2.444 ms · 1326243B · 24469a | — | 3.152 ms · 2372794B · 41432a |
| string-compare | 1.086 ms · 21136B · 1284a | 1.081 ms · 21136B · 1284a | 220.2 µs · 21136B · 1284a | 4.93× | — | 63.79 µs · 9632B · 1585a | 1.674 ms · 296282B · 20528a | 1.142 ms · 59712B · 3431a | 2.321 ms · 50744B · 3848a | 1.817 ms · 209521B · 5520a | — | 2.356 ms · 503610B · 31846a |
| struct-tree-walk | 282.3 µs · 768B · 8a | 282.3 µs · 768B · 8a | 107.4 µs · 768B · 8a | 2.63× | — | 1.897 ns · 0B · 0a | 659.9 µs · 458379B · 5114a | 1.383 ms · 818514B · 11253a | 1.112 ms · 558961B · 6149a | 3.544 ms · 2570666B · 34797a | — | 1.17 µs · 600B · 17a |
| tail-ping-pong | 29.25 µs · 0B · 0a | 29.39 µs · 0B · 0a | 481.2 ns · 0B · 0a | 60.8× | — | 890.9 ns · 0B · 0a | 110.8 µs · 106112B · 2001a | 35.75 µs · 21855B · 85a | 113.1 µs · 13984B · 1726a | 116.8 µs · 14280B · 1731a | — | 570.4 µs · 348836B · 9016a |
| tail-sum | 29.29 µs · 0B · 0a | 29.4 µs · 0B · 0a | 667.9 ns · 0B · 0a | 43.9× | — | 302.5 ns · 0B · 0a | 111.6 µs · 106112B · 2001a | 36.31 µs · 21856B · 85a | 112.9 µs · 13984B · 1726a | 116.6 µs · 14280B · 1731a | — | 84.47 µs · 4608B · 1017a |
| typed-array-sum | 7.312 µs · 0B · 0a | 7.371 µs · 0B · 0a | 265.5 ns · 0B · 0a | 27.5× | 261.9 ns · 8B · 1a | 1.875 ns · 0B · 0a | 45.98 µs · 94208B · 513a | 8.393 µs · 4000B · 15a | 27.56 µs · 2080B · 238a | 17.17 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.4 µs · 18424B · 2046a | 112.5 µs · 18424B · 2046a | 1.363 µs · 18424B · 2046a | 82.5× | — | 356.1 ns · 0B · 0a | 106.4 µs · 107608B · 2188a | — | 996.3 µs · 447254B · 12735a | 432.8 µs · 199937B · 7772a | — | 102.1 µs · 13546B · 1317a |

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
