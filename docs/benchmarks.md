# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.93 µs · 0B · 0a | 8.901 µs · 0B · 0a | 5.302 µs · 0B · 0a | 1.68× | — | 2.231 µs · 1024B · 128a | 37.12 µs · 96289B · 388a | 16.93 µs · 14376B · 256a | 70.23 µs · 78016B · 770a | 14.03 µs · 5712B · 266a | — | 30.87 µs · 2308B · 276a |
| binary-trees | 1.965 ms · 768B · 8a | 1.959 ms · 768B · 8a | 593.9 µs · 768B · 8a | 3.31× | — | 288.8 µs · 201936B · 8414a | 3.835 ms · 1468184B · 62877a | 10.55 ms · 6995671B · 92703a | 9.023 ms · 5198699B · 69052a | 27.57 ms · 19457629B · 280714a | — | 28.46 ms · 15128206B · 446106a |
| branch-tree | 1.355 µs · 0B · 0a | 19.74 ns · 0B · 0a | 86.8 ns · 0B · 0a | 15.6× | 625.7 ns · 16B · 1a | 160.5 ns · 0B · 0a | 46.52 µs · 95384B · 660a | 15.21 µs · 2464B · 9a | 26.8 µs · 1992B · 196a | 25.24 µs · 2168B · 203a | — | 20.74 µs · 1640B · 219a |
| closure-counter | 5.562 µs · 64B · 2a | 5.559 µs · 64B · 2a | 914.2 ns · 64B · 2a | 6.08× | — | 40.11 ns · 0B · 0a | 40.49 µs · 92272B · 261a | 13.12 µs · 152B · 3a | 19.15 µs · 1264B · 13a | 78.13 µs · 58312B · 659a | — | 90.52 µs · 35629B · 922a |
| coroutine-gen | 78.84 µs · 128B · 2a | 78.91 µs · 128B · 2a | 78.86 µs · 128B · 2a | 1× | — | 43.38 µs · 208B · 7a | — | 323.9 µs · 149936B · 649a | 603.9 µs · 330650B · 6719a | 198.8 µs · 91800B · 3646a | — | — |
| fannkuch | 832.7 µs · 34608B · 1442a | 818.9 µs · 34608B · 1442a | 211.5 µs · 34608B · 1442a | 3.94× | — | 35.39 µs · 17280B · 720a | 2.028 ms · 456578B · 25621a | 1.55 ms · 443136B · 1460a | 19.28 ms · 14005345B · 164946a | 3.387 ms · 1367677B · 16944a | — | 4.775 ms · 2501442B · 88439a |
| fnv1a64 | 124.6 µs · 16400B · 2049a | 124 µs · 16400B · 2049a | 1.163 µs · 16400B · 2049a | 107× | — | 902.5 ns · 0B · 0a | 162.6 µs · 120808B · 3838a | — | 1.5 ms · 608258B · 18475a | 398.5 µs · 162921B · 7429a | — | 154.5 µs · 17282B · 2074a |
| host-call | 460.3 µs · 41752B · 3080a | 473.1 µs · 41752B · 3080a | 471.1 µs · 41752B · 3080a | 0.977× | — | 3.311 µs · 0B · 0a | 180.6 µs · 139264B · 3073a | 129.5 µs · 22536B · 1079a | 787.7 µs · 158048B · 14585a | 103.6 µs · 14920B · 1799a | — | 554.8 µs · 340638B · 9231a |
| i64-wide-fib | 28.28 ms · 2913456B · 364178a | 21.27 ms · 1942304B · 242785a | 679.7 µs · 16B · 1a | 41.6× | — | 239.1 µs · 0B · 0a | 23.48 µs · 90520B · 52a | — | 153.6 ms · 64193400B · 1942306a | 352.6 ms · 253472720B · 4248839a | — | 132 ms · 98686554B · 2138708a |
| indirect-recursive-fib | 1.203 ms · 0B · 0a | 1.2 ms · 0B · 0a | 60.51 µs · 0B · 0a | 19.9× | 101.5 µs · 8B · 1a | 34.81 µs · 0B · 0a | 2.095 ms · 319346B · 28655a | 1.947 ms · 703B · 2a | 2.572 ms · 4680B · 39a | 11.6 ms · 10158195B · 109494a | — | 23.91 ms · 13061288B · 394061a |
| iterative-fib | 1.136 µs · 0B · 0a | 1.075 µs · 0B · 0a | 81.82 ns · 0B · 0a | 13.9× | 84.54 ns · 8B · 1a | 11.9 ns · 0B · 0a | 25.14 µs · 90592B · 61a | 1.139 µs · 159B · 0a | 4.224 µs · 368B · 20a | 6.051 µs · 2448B · 88a | — | 6.172 µs · 944B · 81a |
| mandelbrot | 290.6 µs · 0B · 0a | 249.7 µs · 0B · 0a | 11.26 µs · 0B · 0a | 25.8× | — | 6.325 µs · 0B · 0a | 972.4 µs · 292345B · 25008a | 490.3 µs · 169408B · 661a | 8.216 ms · 5558656B · 104191a | 1.494 ms · 324978B · 23643a | — | 1.522 ms · 365402B · 22513a |
| map-ops | 70.42 µs · 8816B · 3a | 70.41 µs · 8816B · 3a | 10.9 µs · 8816B · 3a | 6.46× | — | 10.9 µs · 4904B · 3a | — | 280.2 µs · 114328B · 198a | 383.5 µs · 66728B · 4078a | — | — | 1.113 ms · 501527B · 15813a |
| mat-mul | 375.6 µs · 6216B · 6a | 375.4 µs · 6216B · 6a | 8.695 µs · 6216B · 6a | 43.2× | — | 5.542 µs · 6144B · 3a | 1.366 ms · 431714B · 34871a | 660.7 µs · 143168B · 478a | 1.21 ms · 61304B · 537a | 1.452 ms · 90713B · 9350a | — | 928.1 µs · 36614B · 6036a |
| n-body | 614.4 µs · 504B · 14a | 582.3 µs · 504B · 14a | 18.64 µs · 504B · 14a | 33× | — | 7.345 µs · 0B · 0a | 1.529 ms · 386898B · 35892a | 1.216 ms · 258913B · 2002a | 3.287 ms · 658436B · 81545a | 2.423 ms · 382715B · 34975a | — | 1.75 ms · 445466B · 20669a |
| n-queens | 431 µs · 120B · 6a | 431.2 µs · 120B · 6a | 31.52 µs · 120B · 6a | 13.7× | — | 5.998 µs · 0B · 0a | 954.3 µs · 234865B · 17784a | 571.7 µs · 14423B · 63a | 1.102 ms · 7424B · 35a | 1.433 ms · 410345B · 5147a | — | 2.006 ms · 766010B · 33371a |
| naive-checksum | 252.4 µs · 0B · 0a | 142.8 µs · 0B · 0a | 8.765 µs · 0B · 0a | 28.8× | — | 2.758 µs · 0B · 0a | 619 µs · 235753B · 18206a | 477.5 µs · 174768B · 682a | 878.5 µs · 155640B · 19402a | 778.7 µs · 163816B · 20409a | — | 312.3 µs · 8977B · 2071a |
| permutation-flips | 128.4 µs · 7680B · 128a | 128.6 µs · 7680B · 128a | 17.17 µs · 7680B · 128a | 7.48× | — | 1.88 ns · 0B · 0a | 574.7 µs · 292858B · 9856a | 236.3 µs · 78807B · 451a | 531.2 µs · 122504B · 765a | 491.6 µs · 111976B · 2368a | — | 1.017 µs · 528B · 13a |
| recursive-fib-20 | 713.7 µs · 0B · 0a | 718.4 µs · 0B · 0a | 47.25 µs · 0B · 0a | 15.1× | 42.77 µs · 8B · 1a | 21.5 µs · 0B · 0a | 1.926 ms · 319345B · 28655a | 2.166 ms · 704B · 2a | 3.056 ms · 4680B · 39a | 11.52 ms · 9807909B · 109494a | — | 10.5 ms · 8302686B · 192848a |
| recursive-fib-35 | 969.9 ms · 0B · 0a | 980.1 ms · 0B · 0a | 64.27 ms · 0B · 0a | 15.1× | 58.18 ms · 8B · 1a | 29.06 ms · 0B · 0a | 2.559 s · 312797376B · 39088168a | 3.092 s · 971008B · 3793a | 4.17 s · 375360B · 46373a | 15.69 s · 13378019792B · 149349978a | — | — |
| sieve | 20.52 µs · 1048B · 2a | 20.48 µs · 1048B · 2a | 926.2 ns · 1048B · 2a | 22.2× | 1.029 µs · 8B · 1a | 350.8 ns · 0B · 0a | 142.8 µs · 122504B · 1611a | 50.53 µs · 18416B · 44a | 84.15 µs · 1872B · 25a | 76.78 µs · 5704B · 30a | — | 82.28 µs · 4368B · 795a |
| sort-stress | 507 µs · 5136B · 512a | 507.4 µs · 5136B · 512a | 12.8 µs · 5136B · 512a | 39.6× | — | 5.802 µs · 1024B · 2a | 1.29 ms · 270945B · 19965a | 855.7 µs · 36568B · 365a | 1.789 ms · 49104B · 3621a | 1.77 ms · 23448B · 2034a | — | 923.8 µs · 17475B · 1708a |
| spectral-norm | 540.7 µs · 648B · 6a | 460.5 µs · 648B · 6a | 8.372 µs · 648B · 6a | 64.6× | — | 7.015 µs · 576B · 3a | 2.052 ms · 571371B · 54822a | 1.082 ms · 234784B · 925a | 2.465 ms · 275337B · 33729a | 4.612 ms · 2457136B · 52718a | — | 4.078 ms · 2448011B · 74233a |
| string-build | 716.6 µs · 85408B · 4107a | 711.6 µs · 85408B · 4107a | 590.8 µs · 85408B · 4107a | 1.21× | — | 364.6 µs · 855889B · 5001a | 2.423 ms · 2211274B · 36101a | 2.311 ms · 1078112B · 13859a | 3.108 ms · 1147384B · 26883a | 2.464 ms · 1326242B · 24469a | — | 3.081 ms · 2372775B · 41432a |
| string-compare | 1.084 ms · 21136B · 1284a | 1.086 ms · 21136B · 1284a | 217.5 µs · 21136B · 1284a | 4.98× | — | 62.39 µs · 9632B · 1585a | 1.655 ms · 296281B · 20528a | 1.129 ms · 59711B · 3431a | 2.32 ms · 50744B · 3848a | 1.788 ms · 209521B · 5520a | — | 2.366 ms · 503617B · 31846a |
| struct-tree-walk | 281.1 µs · 768B · 8a | 281.7 µs · 768B · 8a | 109.1 µs · 768B · 8a | 2.58× | — | 1.899 ns · 0B · 0a | 650.3 µs · 458379B · 5114a | 1.353 ms · 818513B · 11253a | 1.086 ms · 558961B · 6149a | 3.485 ms · 2570662B · 34797a | — | 1.159 µs · 600B · 17a |
| tail-ping-pong | 29.76 µs · 0B · 0a | 29.83 µs · 0B · 0a | 486.8 ns · 0B · 0a | 61.1× | — | 891 ns · 0B · 0a | 108.9 µs · 106112B · 2001a | 36.56 µs · 21856B · 85a | 110.4 µs · 13984B · 1726a | 115.5 µs · 14280B · 1731a | — | 543.7 µs · 348835B · 9016a |
| tail-sum | 29.84 µs · 0B · 0a | 29.88 µs · 0B · 0a | 674.9 ns · 0B · 0a | 44.2× | — | 300.8 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 35.21 µs · 21856B · 85a | 110.5 µs · 13984B · 1726a | 115.2 µs · 14280B · 1731a | — | 84.95 µs · 4608B · 1017a |
| typed-array-sum | 7.359 µs · 0B · 0a | 7.359 µs · 0B · 0a | 266.3 ns · 0B · 0a | 27.6× | 265.2 ns · 8B · 1a | 1.877 ns · 0B · 0a | 45.83 µs · 94208B · 513a | 8.212 µs · 4000B · 15a | 27.04 µs · 2080B · 238a | 17.01 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.4 µs · 18424B · 2046a | 112 µs · 18424B · 2046a | 1.333 µs · 18424B · 2046a | 84.3× | — | 360.3 ns · 0B · 0a | 103.6 µs · 107608B · 2188a | — | 965.6 µs · 447254B · 12735a | 418.2 µs · 199937B · 7772a | — | 101.4 µs · 13546B · 1317a |

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
