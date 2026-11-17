# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.96 µs · 0B · 0a | 9.001 µs · 0B · 0a | 5.38 µs · 0B · 0a | 1.67× | — | 2.269 µs · 1024B · 128a | 37.69 µs · 96289B · 388a | 17.4 µs · 14376B · 256a | 70.42 µs · 78016B · 770a | 14.53 µs · 5712B · 266a | — | 30.78 µs · 2308B · 276a |
| binary-trees | 1.974 ms · 768B · 8a | 1.969 ms · 768B · 8a | 595.1 µs · 768B · 8a | 3.32× | — | 293.7 µs · 201936B · 8414a | 3.931 ms · 1468184B · 62877a | 10.57 ms · 6995681B · 92703a | 8.966 ms · 5198700B · 69052a | 27.62 ms · 19457602B · 280714a | — | 29.88 ms · 15128174B · 446106a |
| branch-tree | 1.359 µs · 0B · 0a | 19.93 ns · 0B · 0a | 81.84 ns · 0B · 0a | 16.6× | 625.2 ns · 16B · 1a | 160.4 ns · 0B · 0a | 45.97 µs · 95384B · 660a | 15.16 µs · 2464B · 9a | 26.88 µs · 1992B · 196a | 24.81 µs · 2168B · 203a | — | 20.62 µs · 1640B · 219a |
| closure-counter | 5.542 µs · 64B · 2a | 5.512 µs · 64B · 2a | 895.4 ns · 64B · 2a | 6.19× | — | 40.47 ns · 0B · 0a | 43.09 µs · 92272B · 261a | 13.13 µs · 151B · 3a | 19.42 µs · 1264B · 13a | 80.02 µs · 58312B · 659a | — | 89.5 µs · 35628B · 922a |
| coroutine-gen | 79.1 µs · 128B · 2a | 79.23 µs · 128B · 2a | 79.19 µs · 128B · 2a | 0.999× | — | 44.25 µs · 208B · 7a | — | 330.3 µs · 149936B · 649a | 604.1 µs · 330649B · 6719a | 203.6 µs · 91800B · 3646a | — | — |
| fannkuch | 825.9 µs · 34608B · 1442a | 811.7 µs · 34608B · 1442a | 223.7 µs · 34608B · 1442a | 3.69× | — | 35.67 µs · 17280B · 720a | 2.048 ms · 456578B · 25621a | 1.53 ms · 443136B · 1460a | 19.17 ms · 14005342B · 164946a | 3.402 ms · 1367679B · 16944a | — | 4.911 ms · 2501448B · 88439a |
| fnv1a64 | 128 µs · 16400B · 2049a | 127.9 µs · 16400B · 2049a | 1.154 µs · 16400B · 2049a | 111× | — | 902.4 ns · 0B · 0a | 162.8 µs · 120808B · 3838a | — | 1.486 ms · 608254B · 18475a | 390.5 µs · 162921B · 7429a | — | 154.8 µs · 17282B · 2074a |
| host-call | 465.4 µs · 41752B · 3080a | 478.8 µs · 41752B · 3080a | 473.8 µs · 41752B · 3080a | 0.982× | — | 3.312 µs · 0B · 0a | 180.9 µs · 139264B · 3073a | 131.5 µs · 22536B · 1079a | 815 µs · 158048B · 14585a | 109 µs · 14920B · 1799a | — | 601.5 µs · 340638B · 9231a |
| i64-wide-fib | 28.57 ms · 2913456B · 364178a | 20.93 ms · 1942304B · 242785a | 679.6 µs · 16B · 1a | 42× | — | 239.6 µs · 0B · 0a | 24.18 µs · 90520B · 52a | — | 159.9 ms · 64193280B · 1942305a | 368.3 ms · 253473008B · 4248843a | — | 134.5 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 1.194 ms · 0B · 0a | 1.189 ms · 0B · 0a | 60.5 µs · 0B · 0a | 19.7× | 99.04 µs · 8B · 1a | 34.78 µs · 0B · 0a | 2.095 ms · 319345B · 28655a | 1.94 ms · 704B · 2a | 2.555 ms · 4680B · 39a | 11.52 ms · 10158185B · 109494a | — | 25.8 ms · 13061341B · 394061a |
| iterative-fib | 1.135 µs · 0B · 0a | 1.069 µs · 0B · 0a | 79.9 ns · 0B · 0a | 14.2× | 84.79 ns · 8B · 1a | 12.41 ns · 0B · 0a | 25.16 µs · 90592B · 61a | 1.148 µs · 160B · 0a | 4.224 µs · 368B · 20a | 6.17 µs · 2448B · 88a | — | 6.133 µs · 944B · 81a |
| mandelbrot | 290 µs · 0B · 0a | 247.8 µs · 0B · 0a | 11.27 µs · 0B · 0a | 25.7× | — | 6.321 µs · 0B · 0a | 946.2 µs · 292345B · 25008a | 483.6 µs · 169408B · 661a | 8.342 ms · 5558642B · 104191a | 1.516 ms · 324978B · 23643a | — | 1.53 ms · 365402B · 22513a |
| map-ops | 70.92 µs · 8816B · 3a | 70.65 µs · 8816B · 3a | 11.06 µs · 8816B · 3a | 6.41× | — | 11.14 µs · 4904B · 3a | — | 281.5 µs · 114327B · 198a | 382.7 µs · 66728B · 4078a | — | — | 1.129 ms · 501518B · 15813a |
| mat-mul | 373.5 µs · 6216B · 6a | 375 µs · 6216B · 6a | 8.808 µs · 6216B · 6a | 42.4× | — | 5.551 µs · 6144B · 3a | 1.376 ms · 431715B · 34871a | 671.7 µs · 143168B · 478a | 1.205 ms · 61304B · 537a | 1.458 ms · 90712B · 9350a | — | 918.8 µs · 36613B · 6036a |
| n-body | 613.7 µs · 504B · 14a | 581.1 µs · 504B · 14a | 19.01 µs · 504B · 14a | 32.3× | — | 7.355 µs · 0B · 0a | 1.521 ms · 386899B · 35892a | 1.209 ms · 258913B · 2002a | 3.289 ms · 658434B · 81545a | 2.443 ms · 382714B · 34975a | — | 1.767 ms · 445468B · 20669a |
| n-queens | 430.9 µs · 120B · 6a | 430.1 µs · 120B · 6a | 31.57 µs · 120B · 6a | 13.6× | — | 6.028 µs · 0B · 0a | 958 µs · 234865B · 17784a | 567.5 µs · 14423B · 63a | 1.098 ms · 7424B · 35a | 1.448 ms · 410346B · 5147a | — | 2.035 ms · 766010B · 33371a |
| naive-checksum | 253.7 µs · 0B · 0a | 142.1 µs · 0B · 0a | 8.78 µs · 0B · 0a | 28.9× | — | 2.759 µs · 0B · 0a | 618.8 µs · 235753B · 18206a | 479.5 µs · 174768B · 682a | 907.2 µs · 155640B · 19402a | 794.3 µs · 163817B · 20409a | — | 311 µs · 8977B · 2071a |
| permutation-flips | 128.8 µs · 7680B · 128a | 128.8 µs · 7680B · 128a | 17.45 µs · 7680B · 128a | 7.38× | — | 1.879 ns · 0B · 0a | 564.6 µs · 292857B · 9856a | 234.9 µs · 78808B · 451a | 529.6 µs · 122504B · 765a | 492.6 µs · 111976B · 2368a | — | 1.021 µs · 528B · 13a |
| recursive-fib-20 | 711 µs · 0B · 0a | 715.9 µs · 0B · 0a | 47.12 µs · 0B · 0a | 15.1× | 42.73 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.925 ms · 319345B · 28655a | 2.166 ms · 704B · 2a | 2.973 ms · 4680B · 39a | 11.71 ms · 9807908B · 109494a | — | 11.08 ms · 8302668B · 192847a |
| recursive-fib-35 | 968.9 ms · 0B · 0a | 976.3 ms · 0B · 0a | 64 ms · 0B · 0a | 15.1× | 58.23 ms · 8B · 1a | 29.15 ms · 0B · 0a | 2.561 s · 312797712B · 39088169a | 2.947 s · 971008B · 3793a | 4.05 s · 375360B · 46373a | 15.85 s · 13378016720B · 149349957a | — | — |
| sieve | 20.48 µs · 1048B · 2a | 20.41 µs · 1048B · 2a | 995.5 ns · 1048B · 2a | 20.6× | 1.029 µs · 8B · 1a | 372 ns · 0B · 0a | 143.4 µs · 122504B · 1611a | 51.82 µs · 18416B · 44a | 83.62 µs · 1872B · 25a | 77.36 µs · 5704B · 30a | — | 81.69 µs · 4368B · 795a |
| sort-stress | 505.7 µs · 5136B · 512a | 508 µs · 5136B · 512a | 12.7 µs · 5136B · 512a | 39.8× | — | 5.427 µs · 1024B · 2a | 1.27 ms · 270945B · 19965a | 842.9 µs · 36568B · 365a | 1.799 ms · 49104B · 3621a | 1.771 ms · 23448B · 2034a | — | 924.1 µs · 17473B · 1708a |
| spectral-norm | 540.2 µs · 648B · 6a | 462.5 µs · 648B · 6a | 8.422 µs · 648B · 6a | 64.1× | — | 7.063 µs · 576B · 3a | 2.001 ms · 571370B · 54822a | 1.079 ms · 234784B · 925a | 2.469 ms · 275337B · 33729a | 4.531 ms · 2457136B · 52718a | — | 4.121 ms · 2448008B · 74233a |
| string-build | 728.1 µs · 85408B · 4107a | 719 µs · 85408B · 4107a | 601.7 µs · 85408B · 4107a | 1.21× | — | 420.8 µs · 855889B · 5001a | 2.571 ms · 2211274B · 36101a | 2.436 ms · 1078111B · 13859a | 3.192 ms · 1147387B · 26883a | 2.517 ms · 1326242B · 24469a | — | 3.162 ms · 2372779B · 41432a |
| string-compare | 1.088 ms · 21136B · 1284a | 1.086 ms · 21136B · 1284a | 222.8 µs · 21136B · 1284a | 4.88× | — | 63.66 µs · 9632B · 1585a | 1.676 ms · 296282B · 20528a | 1.129 ms · 59711B · 3431a | 2.336 ms · 50744B · 3848a | 1.844 ms · 209520B · 5520a | — | 2.379 ms · 503613B · 31846a |
| struct-tree-walk | 284.3 µs · 768B · 8a | 283.9 µs · 768B · 8a | 108 µs · 768B · 8a | 2.63× | — | 1.898 ns · 0B · 0a | 667 µs · 458378B · 5114a | 1.343 ms · 818513B · 11253a | 1.076 ms · 558961B · 6149a | 3.437 ms · 2570664B · 34797a | — | 1.163 µs · 600B · 17a |
| tail-ping-pong | 29.14 µs · 0B · 0a | 29.36 µs · 0B · 0a | 481.2 ns · 0B · 0a | 60.6× | — | 890.7 ns · 0B · 0a | 112.3 µs · 106112B · 2001a | 36.99 µs · 21856B · 85a | 110.2 µs · 13984B · 1726a | 117.9 µs · 14280B · 1731a | — | 535.3 µs · 348832B · 9016a |
| tail-sum | 29.5 µs · 0B · 0a | 29.42 µs · 0B · 0a | 684 ns · 0B · 0a | 43.1× | — | 302.5 ns · 0B · 0a | 112.2 µs · 106112B · 2001a | 36.72 µs · 21856B · 85a | 110.4 µs · 13984B · 1726a | 118.2 µs · 14280B · 1731a | — | 84.85 µs · 4608B · 1017a |
| typed-array-sum | 7.35 µs · 0B · 0a | 7.343 µs · 0B · 0a | 260.2 ns · 0B · 0a | 28.2× | 266.8 ns · 8B · 1a | 1.876 ns · 0B · 0a | 49.17 µs · 94208B · 513a | 8.556 µs · 4000B · 15a | 26.76 µs · 2080B · 238a | 17.08 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.6 µs · 18424B · 2046a | 111.8 µs · 18424B · 2046a | 1.472 µs · 18424B · 2046a | 75.8× | — | 356.1 ns · 0B · 0a | 106 µs · 107608B · 2188a | — | 919.8 µs · 447254B · 12735a | 397.2 µs · 199938B · 7772a | — | 101.7 µs · 13545B · 1317a |

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
