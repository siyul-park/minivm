# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.87 µs · 0B · 0a | 8.88 µs · 0B · 0a | 5.343 µs · 0B · 0a | 1.66× | — | 2.234 µs · 1024B · 128a | 38.1 µs · 96288B · 388a | 17 µs · 14376B · 256a | 69.83 µs · 78016B · 770a | 14.39 µs · 5712B · 266a | — | 31.01 µs · 2308B · 276a |
| binary-trees | 1.97 ms · 768B · 8a | 1.961 ms · 768B · 8a | 592.5 µs · 768B · 8a | 3.33× | — | 286.3 µs · 201936B · 8414a | 3.865 ms · 1468185B · 62877a | 10.57 ms · 6995679B · 92703a | 8.758 ms · 5198698B · 69052a | 26.52 ms · 19457634B · 280714a | — | 29.6 ms · 15128168B · 446106a |
| branch-tree | 1.344 µs · 0B · 0a | 19.79 ns · 0B · 0a | 85.01 ns · 0B · 0a | 15.8× | 626.9 ns · 16B · 1a | 160.5 ns · 0B · 0a | 45.45 µs · 95384B · 660a | 15.25 µs · 2464B · 9a | 26.83 µs · 1992B · 196a | 24.27 µs · 2168B · 203a | — | 20.46 µs · 1640B · 219a |
| closure-counter | 5.524 µs · 64B · 2a | 5.539 µs · 64B · 2a | 925.1 ns · 64B · 2a | 5.97× | — | 40.14 ns · 0B · 0a | 42.11 µs · 92272B · 261a | 12.79 µs · 151B · 3a | 19.17 µs · 1264B · 13a | 77.13 µs · 58312B · 659a | — | 92.5 µs · 35629B · 922a |
| coroutine-gen | 78.76 µs · 128B · 2a | 78.58 µs · 128B · 2a | 78.79 µs · 128B · 2a | 1× | — | 41.53 µs · 208B · 7a | — | 329.6 µs · 149936B · 649a | 589.8 µs · 330649B · 6719a | 197.7 µs · 91800B · 3646a | — | — |
| fannkuch | 836.7 µs · 34608B · 1442a | 822.1 µs · 34608B · 1442a | 219.7 µs · 34608B · 1442a | 3.81× | — | 35.22 µs · 17280B · 720a | 2.007 ms · 456579B · 25621a | 1.534 ms · 443136B · 1460a | 18.79 ms · 14005333B · 164946a | 3.279 ms · 1367678B · 16944a | — | 4.769 ms · 2501441B · 88439a |
| fnv1a64 | 125.7 µs · 16400B · 2049a | 125.5 µs · 16400B · 2049a | 1.138 µs · 16400B · 2049a | 110× | — | 902.7 ns · 0B · 0a | 160.4 µs · 120808B · 3838a | — | 1.455 ms · 608251B · 18475a | 387.7 µs · 162921B · 7429a | — | 156.2 µs · 17282B · 2074a |
| host-call | 470 µs · 41752B · 3080a | 481 µs · 41752B · 3080a | 488.4 µs · 41752B · 3080a | 0.962× | — | 3.313 µs · 0B · 0a | 175.2 µs · 139264B · 3073a | 132.3 µs · 22536B · 1079a | 809.6 µs · 158048B · 14585a | 107.6 µs · 14920B · 1799a | — | 595.2 µs · 340639B · 9231a |
| i64-wide-fib | 28.73 ms · 2913456B · 364178a | 21.13 ms · 1942304B · 242785a | 679.4 µs · 16B · 1a | 42.3× | — | 239.1 µs · 0B · 0a | 22.6 µs · 90520B · 52a | — | 157.5 ms · 64193288B · 1942305a | 335.9 ms · 253472496B · 4248839a | — | 130.1 ms · 98686613B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 1.185 ms · 0B · 0a | 60.45 µs · 0B · 0a | 19.6× | 99.16 µs · 8B · 1a | 34.88 µs · 0B · 0a | 2.092 ms · 319346B · 28655a | 1.955 ms · 703B · 2a | 2.549 ms · 4680B · 39a | 11.07 ms · 10158199B · 109494a | — | 25.69 ms · 13061313B · 394061a |
| iterative-fib | 1.132 µs · 0B · 0a | 1.071 µs · 0B · 0a | 78.45 ns · 0B · 0a | 14.4× | 87.47 ns · 8B · 1a | 11.89 ns · 0B · 0a | 22.88 µs · 90592B · 61a | 1.151 µs · 159B · 0a | 4.34 µs · 368B · 20a | 6.07 µs · 2448B · 88a | — | 6.261 µs · 944B · 81a |
| mandelbrot | 292.2 µs · 0B · 0a | 249.6 µs · 0B · 0a | 11.24 µs · 0B · 0a | 26× | — | 6.324 µs · 0B · 0a | 945.5 µs · 292346B · 25008a | 478.5 µs · 169408B · 661a | 7.833 ms · 5558652B · 104191a | 1.493 ms · 324978B · 23643a | — | 1.575 ms · 365396B · 22513a |
| map-ops | 70.06 µs · 8816B · 3a | 69.91 µs · 8816B · 3a | 10.64 µs · 8816B · 3a | 6.58× | — | 10.71 µs · 4904B · 3a | — | 272.5 µs · 114328B · 198a | 386 µs · 66728B · 4078a | — | — | 1.178 ms · 501521B · 15813a |
| mat-mul | 375.9 µs · 6216B · 6a | 374.4 µs · 6216B · 6a | 8.434 µs · 6216B · 6a | 44.6× | — | 5.269 µs · 6144B · 3a | 1.349 ms · 431714B · 34871a | 671.1 µs · 143168B · 478a | 1.218 ms · 61304B · 537a | 1.445 ms · 90712B · 9350a | — | 930.8 µs · 36614B · 6036a |
| n-body | 612.1 µs · 504B · 14a | 582.5 µs · 504B · 14a | 18.69 µs · 504B · 14a | 32.8× | — | 7.345 µs · 0B · 0a | 1.489 ms · 386899B · 35892a | 1.217 ms · 258913B · 2002a | 3.368 ms · 658436B · 81545a | 2.417 ms · 382714B · 34975a | — | 1.764 ms · 445468B · 20669a |
| n-queens | 429.5 µs · 120B · 6a | 430.2 µs · 120B · 6a | 31.55 µs · 120B · 6a | 13.6× | — | 6.015 µs · 0B · 0a | 954.4 µs · 234865B · 17784a | 570.9 µs · 14423B · 63a | 1.101 ms · 7424B · 35a | 1.429 ms · 410346B · 5147a | — | 2 ms · 766005B · 33371a |
| naive-checksum | 253.7 µs · 0B · 0a | 142.5 µs · 0B · 0a | 8.739 µs · 0B · 0a | 29× | — | 2.759 µs · 0B · 0a | 614.8 µs · 235754B · 18206a | 474.2 µs · 174768B · 682a | 890.8 µs · 155640B · 19402a | 791.1 µs · 163817B · 20409a | — | 312.6 µs · 8977B · 2071a |
| permutation-flips | 129.2 µs · 7680B · 128a | 129.4 µs · 7680B · 128a | 17.04 µs · 7680B · 128a | 7.58× | — | 1.869 ns · 0B · 0a | 540.7 µs · 292857B · 9856a | 231.2 µs · 78808B · 451a | 525.4 µs · 122504B · 765a | 486.5 µs · 111976B · 2368a | — | 1.046 µs · 528B · 13a |
| recursive-fib-20 | 711.5 µs · 0B · 0a | 715.8 µs · 0B · 0a | 47.11 µs · 0B · 0a | 15.1× | 42.7 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.96 ms · 319346B · 28655a | 2.189 ms · 703B · 2a | 2.961 ms · 4680B · 39a | 10.82 ms · 9807918B · 109494a | — | 11.61 ms · 8302718B · 192848a |
| recursive-fib-35 | 970.5 ms · 0B · 0a | 979.6 ms · 0B · 0a | 63.92 ms · 0B · 0a | 15.2× | 58.2 ms · 8B · 1a | 29.15 ms · 0B · 0a | 2.624 s · 312797552B · 39088169a | 3.043 s · 971008B · 3793a | 4.042 s · 375360B · 46373a | 15.19 s · 13378020640B · 149349988a | — | — |
| sieve | 20.15 µs · 1048B · 2a | 20.31 µs · 1048B · 2a | 927.4 ns · 1048B · 2a | 21.7× | 1.028 µs · 8B · 1a | 350.9 ns · 0B · 0a | 135.1 µs · 122504B · 1611a | 50.8 µs · 18416B · 44a | 84.39 µs · 1872B · 25a | 76.41 µs · 5704B · 30a | — | 81.07 µs · 4368B · 795a |
| sort-stress | 504.4 µs · 5136B · 512a | 507.9 µs · 5136B · 512a | 12.84 µs · 5136B · 512a | 39.3× | — | 5.776 µs · 1024B · 2a | 1.276 ms · 270945B · 19965a | 855.8 µs · 36567B · 365a | 1.802 ms · 49104B · 3621a | 1.78 ms · 23448B · 2034a | — | 925.6 µs · 17473B · 1708a |
| spectral-norm | 538.2 µs · 648B · 6a | 459.3 µs · 648B · 6a | 8.385 µs · 648B · 6a | 64.2× | — | 7.041 µs · 576B · 3a | 2.078 ms · 571371B · 54822a | 1.078 ms · 234784B · 925a | 2.499 ms · 275337B · 33729a | 4.546 ms · 2457135B · 52718a | — | 4.169 ms · 2448025B · 74233a |
| string-build | 734.9 µs · 85408B · 4107a | 733.8 µs · 85408B · 4107a | 482.9 µs · 85408B · 4107a | 1.52× | — | 349.1 µs · 855890B · 5001a | 2.351 ms · 2211273B · 36101a | 2.288 ms · 1078110B · 13859a | 3.078 ms · 1147382B · 26883a | 2.417 ms · 1326244B · 24469a | — | 3.06 ms · 2372798B · 41432a |
| string-compare | 1.089 ms · 21136B · 1284a | 1.086 ms · 21136B · 1284a | 222.6 µs · 21136B · 1284a | 4.89× | — | 64.19 µs · 9632B · 1585a | 1.672 ms · 296281B · 20528a | 1.143 ms · 59712B · 3431a | 2.322 ms · 50744B · 3848a | 1.814 ms · 209521B · 5520a | — | 2.406 ms · 503612B · 31846a |
| struct-tree-walk | 282 µs · 768B · 8a | 281.9 µs · 768B · 8a | 107.8 µs · 768B · 8a | 2.62× | — | 1.894 ns · 0B · 0a | 633.5 µs · 458379B · 5114a | 1.321 ms · 818512B · 11253a | 1.055 ms · 558961B · 6149a | 3.377 ms · 2570667B · 34797a | — | 1.221 µs · 600B · 17a |
| tail-ping-pong | 29.46 µs · 0B · 0a | 29.46 µs · 0B · 0a | 478.8 ns · 0B · 0a | 61.5× | — | 891.2 ns · 0B · 0a | 106.8 µs · 106112B · 2001a | 35.03 µs · 21856B · 85a | 112.2 µs · 13984B · 1726a | 117.3 µs · 14280B · 1731a | — | 539.9 µs · 348835B · 9016a |
| tail-sum | 29.32 µs · 0B · 0a | 29.39 µs · 0B · 0a | 665.6 ns · 0B · 0a | 44.1× | — | 300.9 ns · 0B · 0a | 105.3 µs · 106112B · 2001a | 35.61 µs · 21856B · 85a | 113.1 µs · 13984B · 1726a | 116.6 µs · 14280B · 1731a | — | 84.54 µs · 4608B · 1017a |
| typed-array-sum | 7.361 µs · 0B · 0a | 7.381 µs · 0B · 0a | 261.1 ns · 0B · 0a | 28.2× | 268 ns · 8B · 1a | 1.864 ns · 0B · 0a | 43.82 µs · 94208B · 513a | 8.197 µs · 4000B · 15a | 27.55 µs · 2080B · 238a | 17.54 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.5 µs · 18424B · 2046a | 112.8 µs · 18424B · 2046a | 1.284 µs · 18424B · 2046a | 87.6× | — | 361.6 ns · 0B · 0a | 101.9 µs · 107608B · 2188a | — | 969.2 µs · 447253B · 12735a | 439.7 µs · 199937B · 7772a | — | 101.9 µs · 13545B · 1317a |

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
