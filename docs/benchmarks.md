# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.891 µs · 0B · 0a | 9.108 µs · 0B · 0a | 5.281 µs · 0B · 0a | 1.68× | — | 2.259 µs · 1024B · 128a | 39.69 µs · 96288B · 388a | 19.56 µs · 14376B · 256a | 73.03 µs · 78016B · 770a | 13.97 µs · 5712B · 266a | — | 31.71 µs · 2308B · 276a |
| binary-trees | 1.975 ms · 768B · 8a | 1.974 ms · 768B · 8a | 589.4 µs · 768B · 8a | 3.35× | — | 283.7 µs · 201936B · 8414a | 3.889 ms · 1468245B · 62877a | 10.26 ms · 6995670B · 92703a | 8.737 ms · 5198698B · 69052a | 26.81 ms · 19457644B · 280714a | — | 29.23 ms · 15128174B · 446106a |
| branch-tree | 1.335 µs · 0B · 0a | 19.75 ns · 0B · 0a | 88.92 ns · 0B · 0a | 15× | 624.6 ns · 16B · 1a | 160.4 ns · 0B · 0a | 45.8 µs · 95384B · 660a | 15.11 µs · 2464B · 9a | 27.03 µs · 1992B · 196a | 24.73 µs · 2168B · 203a | — | 20.92 µs · 1640B · 219a |
| closure-counter | 5.561 µs · 64B · 2a | 5.544 µs · 64B · 2a | 919.4 ns · 64B · 2a | 6.05× | — | 40.16 ns · 0B · 0a | 39.63 µs · 92272B · 261a | 13.17 µs · 151B · 3a | 19.2 µs · 1264B · 13a | 76.92 µs · 58312B · 659a | — | 93.58 µs · 35629B · 922a |
| coroutine-gen | 78.93 µs · 128B · 2a | 78.95 µs · 128B · 2a | 78.26 µs · 128B · 2a | 1.01× | — | 42.51 µs · 208B · 7a | — | 329.6 µs · 149936B · 649a | 599.3 µs · 330650B · 6719a | 200.4 µs · 91800B · 3646a | — | — |
| fannkuch | 838.1 µs · 34608B · 1442a | 822.5 µs · 34608B · 1442a | 222.4 µs · 34608B · 1442a | 3.77× | — | 35.36 µs · 17280B · 720a | 2.041 ms · 456578B · 25621a | 1.524 ms · 443137B · 1460a | 19.02 ms · 14005352B · 164946a | 3.359 ms · 1367680B · 16944a | — | 4.87 ms · 2501438B · 88439a |
| fnv1a64 | 128.7 µs · 16400B · 2049a | 128.3 µs · 16400B · 2049a | 1.161 µs · 16400B · 2049a | 111× | — | 902.7 ns · 0B · 0a | 162.7 µs · 120808B · 3838a | — | 1.463 ms · 608252B · 18475a | 393.5 µs · 162921B · 7429a | — | 157.4 µs · 17282B · 2074a |
| host-call | 469 µs · 41752B · 3080a | 484.4 µs · 41752B · 3080a | 483.3 µs · 41752B · 3080a | 0.97× | — | 3.313 µs · 0B · 0a | 177.6 µs · 139264B · 3073a | 135.3 µs · 22536B · 1079a | 807.1 µs · 158048B · 14585a | 106.9 µs · 14920B · 1799a | — | 596.5 µs · 340636B · 9231a |
| i64-wide-fib | 29.02 ms · 2913440B · 364178a | 21.51 ms · 1942304B · 242785a | 679.8 µs · 16B · 1a | 42.7× | — | 238.9 µs · 0B · 0a | 21.48 µs · 90520B · 52a | — | 157.8 ms · 64193328B · 1942306a | 335.5 ms · 253472640B · 4248839a | — | 128 ms · 98686602B · 2138708a |
| indirect-recursive-fib | 1.2 ms · 0B · 0a | 1.188 ms · 0B · 0a | 60.46 µs · 0B · 0a | 19.8× | 99.08 µs · 8B · 1a | 35.22 µs · 0B · 0a | 2.097 ms · 319345B · 28655a | 1.98 ms · 704B · 2a | 2.581 ms · 4680B · 39a | 10.91 ms · 10158181B · 109494a | — | 25.71 ms · 13061297B · 394061a |
| iterative-fib | 1.136 µs · 0B · 0a | 1.071 µs · 0B · 0a | 84.72 ns · 0B · 0a | 13.4× | 85.75 ns · 8B · 1a | 11.89 ns · 0B · 0a | 22.8 µs · 90592B · 61a | 1.145 µs · 160B · 0a | 4.278 µs · 368B · 20a | 6.079 µs · 2448B · 88a | — | 6.312 µs · 944B · 81a |
| mandelbrot | 293.8 µs · 0B · 0a | 248.2 µs · 0B · 0a | 11.24 µs · 0B · 0a | 26.1× | — | 6.341 µs · 0B · 0a | 959.2 µs · 292345B · 25008a | 481.9 µs · 169408B · 661a | 7.918 ms · 5558641B · 104191a | 1.494 ms · 324979B · 23643a | — | 1.575 ms · 365404B · 22513a |
| map-ops | 70.75 µs · 8816B · 3a | 70.7 µs · 8816B · 3a | 10.9 µs · 8816B · 3a | 6.49× | — | 10.94 µs · 4904B · 3a | — | 274.6 µs · 114327B · 198a | 378.9 µs · 66728B · 4078a | — | — | 1.182 ms · 501520B · 15813a |
| mat-mul | 375.2 µs · 6216B · 6a | 374.9 µs · 6216B · 6a | 8.585 µs · 6216B · 6a | 43.7× | — | 5.483 µs · 6144B · 3a | 1.369 ms · 431714B · 34871a | 677.3 µs · 143168B · 478a | 1.208 ms · 61304B · 537a | 1.462 ms · 90712B · 9350a | — | 928.3 µs · 36615B · 6036a |
| n-body | 612.3 µs · 504B · 14a | 582.7 µs · 504B · 14a | 19.01 µs · 504B · 14a | 32.2× | — | 7.343 µs · 0B · 0a | 1.555 ms · 386898B · 35892a | 1.214 ms · 258912B · 2002a | 3.331 ms · 658436B · 81545a | 2.448 ms · 382714B · 34975a | — | 1.782 ms · 445466B · 20669a |
| n-queens | 430.4 µs · 120B · 6a | 431 µs · 120B · 6a | 31.59 µs · 120B · 6a | 13.6× | — | 6.016 µs · 0B · 0a | 963.5 µs · 234866B · 17784a | 567.5 µs · 14423B · 63a | 1.098 ms · 7424B · 35a | 1.423 ms · 410345B · 5147a | — | 2.019 ms · 766005B · 33371a |
| naive-checksum | 253.7 µs · 0B · 0a | 142.1 µs · 0B · 0a | 8.73 µs · 0B · 0a | 29.1× | — | 2.758 µs · 0B · 0a | 614.1 µs · 235753B · 18206a | 472 µs · 174768B · 682a | 896.6 µs · 155641B · 19402a | 812.5 µs · 163817B · 20409a | — | 313.7 µs · 8977B · 2071a |
| permutation-flips | 129 µs · 7680B · 128a | 129.4 µs · 7680B · 128a | 17.05 µs · 7680B · 128a | 7.57× | — | 1.88 ns · 0B · 0a | 552.2 µs · 292857B · 9856a | 233.6 µs · 78808B · 451a | 517.2 µs · 122504B · 765a | 492.4 µs · 111976B · 2368a | — | 1.046 µs · 528B · 13a |
| recursive-fib-20 | 711.6 µs · 0B · 0a | 717.9 µs · 0B · 0a | 47.34 µs · 0B · 0a | 15× | 42.7 µs · 8B · 1a | 21.47 µs · 0B · 0a | 1.944 ms · 319345B · 28655a | 2.148 ms · 704B · 2a | 2.974 ms · 4680B · 39a | 10.98 ms · 9807924B · 109494a | — | 11.49 ms · 8302735B · 192848a |
| recursive-fib-35 | 969.2 ms · 0B · 0a | 980.6 ms · 0B · 0a | 64.33 ms · 0B · 0a | 15.1× | 58.21 ms · 8B · 1a | 29.12 ms · 0B · 0a | 2.665 s · 312797632B · 39088169a | 2.937 s · 971008B · 3793a | 4.057 s · 375360B · 46373a | 15.01 s · 13378015952B · 149349954a | — | — |
| sieve | 20.32 µs · 1048B · 2a | 20.33 µs · 1048B · 2a | 906.1 ns · 1048B · 2a | 22.4× | 1.03 µs · 8B · 1a | 350.8 ns · 0B · 0a | 132.6 µs · 122504B · 1611a | 50.12 µs · 18416B · 44a | 83.75 µs · 1872B · 25a | 76.3 µs · 5704B · 30a | — | 81.86 µs · 4368B · 795a |
| sort-stress | 506.3 µs · 5136B · 512a | 506.5 µs · 5136B · 512a | 12.8 µs · 5136B · 512a | 39.5× | — | 5.769 µs · 1024B · 2a | 1.314 ms · 270945B · 19965a | 853 µs · 36568B · 365a | 1.798 ms · 49104B · 3621a | 1.758 ms · 23448B · 2034a | — | 920 µs · 17473B · 1708a |
| spectral-norm | 538.3 µs · 648B · 6a | 461.3 µs · 648B · 6a | 8.381 µs · 648B · 6a | 64.2× | — | 7.04 µs · 576B · 3a | 2.072 ms · 571371B · 54822a | 1.089 ms · 234784B · 925a | 2.502 ms · 275337B · 33729a | 4.505 ms · 2457134B · 52718a | — | 4.31 ms · 2448023B · 74233a |
| string-build | 738.6 µs · 85408B · 4107a | 734.9 µs · 85408B · 4107a | 606.5 µs · 85408B · 4107a | 1.22× | — | 338.1 µs · 855890B · 5001a | 2.295 ms · 2211273B · 36101a | 2.33 ms · 1078111B · 13859a | 3.064 ms · 1147387B · 26883a | 2.386 ms · 1326242B · 24469a | — | 3.023 ms · 2372784B · 41432a |
| string-compare | 1.087 ms · 21136B · 1284a | 1.08 ms · 21136B · 1284a | 221 µs · 21136B · 1284a | 4.92× | — | 64.79 µs · 9632B · 1585a | 1.679 ms · 296281B · 20528a | 1.157 ms · 59712B · 3431a | 2.329 ms · 50744B · 3848a | 1.793 ms · 209521B · 5520a | — | 2.426 ms · 503614B · 31846a |
| struct-tree-walk | 286.6 µs · 768B · 8a | 282.6 µs · 768B · 8a | 107.4 µs · 768B · 8a | 2.67× | — | 1.899 ns · 0B · 0a | 630.9 µs · 458379B · 5114a | 1.309 ms · 818512B · 11253a | 1.053 ms · 558960B · 6149a | 3.361 ms · 2570664B · 34797a | — | 1.248 µs · 600B · 17a |
| tail-ping-pong | 29.42 µs · 0B · 0a | 29.52 µs · 0B · 0a | 482.3 ns · 0B · 0a | 61× | — | 891.3 ns · 0B · 0a | 105.1 µs · 106112B · 2001a | 35.1 µs · 21856B · 85a | 113.8 µs · 13984B · 1726a | 120.6 µs · 14280B · 1731a | — | 551 µs · 348833B · 9016a |
| tail-sum | 29.54 µs · 0B · 0a | 29.49 µs · 0B · 0a | 683.2 ns · 0B · 0a | 43.2× | — | 300.9 ns · 0B · 0a | 106.2 µs · 106112B · 2001a | 34.68 µs · 21856B · 85a | 114.1 µs · 13984B · 1726a | 120.4 µs · 14280B · 1731a | — | 86.01 µs · 4608B · 1017a |
| typed-array-sum | 7.377 µs · 0B · 0a | 7.375 µs · 0B · 0a | 258.7 ns · 0B · 0a | 28.5× | 262.2 ns · 8B · 1a | 1.876 ns · 0B · 0a | 43.29 µs · 94208B · 513a | 8.293 µs · 4000B · 15a | 27.8 µs · 2080B · 238a | 17.44 µs · 2496B · 246a | — | — |
| xorshift-i64 | 115.9 µs · 18424B · 2046a | 114.6 µs · 18424B · 2046a | 1.344 µs · 18424B · 2046a | 86.2× | — | 361.4 ns · 0B · 0a | 100 µs · 107608B · 2188a | — | 985.8 µs · 447252B · 12735a | 423 µs · 199937B · 7772a | — | 103.4 µs · 13545B · 1317a |

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
