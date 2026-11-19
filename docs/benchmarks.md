# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.9 µs · 0B · 0a | 8.89 µs · 0B · 0a | 5.358 µs · 0B · 0a | 1.66× | — | 2.223 µs · 1024B · 128a | 37.12 µs · 96288B · 388a | 17.06 µs · 14376B · 256a | 67.22 µs · 78016B · 770a | 14.34 µs · 5712B · 266a | — | 31.2 µs · 2308B · 276a |
| binary-trees | 1.962 ms · 768B · 8a | 1.961 ms · 768B · 8a | 595.4 µs · 768B · 8a | 3.3× | — | 290.1 µs · 201936B · 8414a | 4.393 ms · 1468203B · 62877a | 12.08 ms · 6995721B · 92703a | 9.139 ms · 5198704B · 69052a | 27.08 ms · 19457598B · 280714a | — | 29.82 ms · 15128161B · 446106a |
| branch-tree | 1.343 µs · 0B · 0a | 19.76 ns · 0B · 0a | 88.01 ns · 0B · 0a | 15.3× | 625.7 ns · 16B · 1a | 160.6 ns · 0B · 0a | 44.18 µs · 95384B · 660a | 15.23 µs · 2463B · 9a | 26.93 µs · 1992B · 196a | 24.16 µs · 2168B · 203a | — | 20.72 µs · 1640B · 219a |
| closure-counter | 5.529 µs · 64B · 2a | 5.609 µs · 64B · 2a | 935.4 ns · 64B · 2a | 5.91× | — | 40.44 ns · 0B · 0a | 41.17 µs · 92272B · 261a | 12.84 µs · 151B · 3a | 19.21 µs · 1264B · 13a | 76.47 µs · 58312B · 659a | — | 92.86 µs · 35629B · 922a |
| coroutine-gen | 78.99 µs · 128B · 2a | 78.69 µs · 128B · 2a | 79.78 µs · 128B · 2a | 0.99× | — | 41.72 µs · 208B · 7a | — | 335.6 µs · 149936B · 649a | 586.6 µs · 330649B · 6719a | 196.6 µs · 91800B · 3646a | — | — |
| fannkuch | 834.8 µs · 34608B · 1442a | 817.5 µs · 34608B · 1442a | 222.7 µs · 34608B · 1442a | 3.75× | — | 35.38 µs · 17280B · 720a | 2.032 ms · 456578B · 25621a | 1.526 ms · 443135B · 1460a | 18.78 ms · 14005340B · 164946a | 3.3 ms · 1367677B · 16944a | — | 4.804 ms · 2501436B · 88439a |
| fnv1a64 | 126.5 µs · 16400B · 2049a | 127.3 µs · 16400B · 2049a | 1.163 µs · 16400B · 2049a | 109× | — | 910.2 ns · 0B · 0a | 169.4 µs · 120808B · 3838a | — | 1.469 ms · 608252B · 18475a | 395.1 µs · 162921B · 7429a | — | 157.6 µs · 17282B · 2074a |
| host-call | 468.3 µs · 41752B · 3080a | 486.2 µs · 41752B · 3080a | 487.5 µs · 41752B · 3080a | 0.961× | — | 3.312 µs · 0B · 0a | 181 µs · 139264B · 3073a | 132.8 µs · 22536B · 1079a | 813.5 µs · 158048B · 14585a | 110 µs · 14920B · 1799a | — | 598.4 µs · 340636B · 9231a |
| i64-wide-fib | 29 ms · 2913456B · 364178a | 21.12 ms · 1942288B · 242785a | 683 µs · 16B · 1a | 42.5× | — | 239.5 µs · 0B · 0a | 22.85 µs · 90520B · 52a | — | 158.2 ms · 64193416B · 1942306a | 333.2 ms · 253472624B · 4248839a | — | 132.7 ms · 98686581B · 2138708a |
| indirect-recursive-fib | 1.189 ms · 0B · 0a | 1.188 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.7× | 98.75 µs · 8B · 1a | 34.94 µs · 0B · 0a | 2.109 ms · 319346B · 28655a | 1.942 ms · 704B · 2a | 2.529 ms · 4680B · 39a | 10.92 ms · 10158186B · 109494a | — | 26.09 ms · 13061318B · 394061a |
| iterative-fib | 1.135 µs · 0B · 0a | 1.076 µs · 0B · 0a | 87.37 ns · 0B · 0a | 13× | 86.89 ns · 8B · 1a | 12.47 ns · 0B · 0a | 23.81 µs · 90592B · 61a | 1.188 µs · 160B · 0a | 4.289 µs · 368B · 20a | 6.068 µs · 2448B · 88a | — | 6.319 µs · 944B · 81a |
| mandelbrot | 289.2 µs · 0B · 0a | 248.3 µs · 0B · 0a | 11.24 µs · 0B · 0a | 25.7× | — | 6.342 µs · 0B · 0a | 979.5 µs · 292345B · 25008a | 485.1 µs · 169408B · 661a | 7.993 ms · 5558649B · 104191a | 1.506 ms · 324978B · 23643a | — | 1.567 ms · 365402B · 22513a |
| map-ops | 70.42 µs · 8816B · 3a | 70.33 µs · 8816B · 3a | 10.96 µs · 8816B · 3a | 6.43× | — | 10.91 µs · 4904B · 3a | — | 272 µs · 114327B · 198a | 384.2 µs · 66728B · 4078a | — | — | 1.174 ms · 501517B · 15813a |
| mat-mul | 375 µs · 6216B · 6a | 373.3 µs · 6216B · 6a | 8.669 µs · 6216B · 6a | 43.3× | — | 5.484 µs · 6144B · 3a | 1.37 ms · 431714B · 34871a | 667.6 µs · 143168B · 478a | 1.208 ms · 61304B · 537a | 1.446 ms · 90712B · 9350a | — | 924.8 µs · 36615B · 6036a |
| n-body | 612.8 µs · 504B · 14a | 581.3 µs · 504B · 14a | 18.63 µs · 504B · 14a | 32.9× | — | 7.357 µs · 0B · 0a | 1.534 ms · 386898B · 35892a | 1.216 ms · 258913B · 2002a | 3.349 ms · 658435B · 81545a | 2.436 ms · 382714B · 34975a | — | 1.763 ms · 445474B · 20669a |
| n-queens | 428.3 µs · 120B · 6a | 429.8 µs · 120B · 6a | 31.66 µs · 120B · 6a | 13.5× | — | 6.003 µs · 0B · 0a | 959.4 µs · 234865B · 17784a | 587 µs · 14423B · 63a | 1.093 ms · 7424B · 35a | 1.431 ms · 410345B · 5147a | — | 2.041 ms · 766008B · 33371a |
| naive-checksum | 250.1 µs · 0B · 0a | 141.8 µs · 0B · 0a | 8.775 µs · 0B · 0a | 28.5× | — | 2.758 µs · 0B · 0a | 620.2 µs · 235753B · 18206a | 477.7 µs · 174768B · 682a | 875.1 µs · 155640B · 19402a | 798 µs · 163817B · 20409a | — | 314.4 µs · 8977B · 2071a |
| permutation-flips | 128.6 µs · 7680B · 128a | 129.3 µs · 7680B · 128a | 17.1 µs · 7680B · 128a | 7.52× | — | 1.864 ns · 0B · 0a | 557.5 µs · 292857B · 9856a | 232.2 µs · 78808B · 451a | 522 µs · 122504B · 765a | 494.2 µs · 111976B · 2368a | — | 1.054 µs · 528B · 13a |
| recursive-fib-20 | 709.2 µs · 0B · 0a | 718.9 µs · 0B · 0a | 47.18 µs · 0B · 0a | 15× | 42.72 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.975 ms · 319345B · 28655a | 2.177 ms · 704B · 2a | 2.952 ms · 4680B · 39a | 11.1 ms · 9807915B · 109494a | — | 11.28 ms · 8302722B · 192848a |
| recursive-fib-35 | 972.7 ms · 0B · 0a | 978.8 ms · 0B · 0a | 64.07 ms · 0B · 0a | 15.2× | 58.92 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.661 s · 312797328B · 39088168a | 2.968 s · 971008B · 3793a | 4.027 s · 375360B · 46373a | 15.17 s · 13378014656B · 149349951a | — | — |
| sieve | 20.32 µs · 1048B · 2a | 20.21 µs · 1048B · 2a | 939.6 ns · 1048B · 2a | 21.6× | 1.032 µs · 8B · 1a | 371.9 ns · 0B · 0a | 139.2 µs · 122504B · 1611a | 50.14 µs · 18416B · 44a | 83.98 µs · 1872B · 25a | 76.87 µs · 5704B · 30a | — | 80.92 µs · 4368B · 795a |
| sort-stress | 503.6 µs · 5136B · 512a | 507.6 µs · 5136B · 512a | 12.65 µs · 5136B · 512a | 39.8× | — | 5.352 µs · 1024B · 2a | 1.297 ms · 270945B · 19965a | 844.7 µs · 36567B · 365a | 1.788 ms · 49104B · 3621a | 1.772 ms · 23448B · 2034a | — | 927.9 µs · 17473B · 1708a |
| spectral-norm | 535.5 µs · 648B · 6a | 459.4 µs · 648B · 6a | 8.422 µs · 648B · 6a | 63.6× | — | 7.078 µs · 576B · 3a | 2.138 ms · 571370B · 54822a | 1.1 ms · 234784B · 925a | 2.519 ms · 275337B · 33729a | 4.591 ms · 2457138B · 52718a | — | 4.227 ms · 2447996B · 74233a |
| string-build | 734 µs · 85408B · 4107a | 731.7 µs · 85408B · 4107a | 613.5 µs · 85408B · 4107a | 1.2× | — | 386.6 µs · 855891B · 5001a | 2.365 ms · 2211274B · 36101a | 2.289 ms · 1078111B · 13859a | 3.094 ms · 1147385B · 26883a | 2.419 ms · 1326243B · 24469a | — | 3.148 ms · 2372800B · 41432a |
| string-compare | 1.09 ms · 21136B · 1284a | 1.091 ms · 21136B · 1284a | 225.4 µs · 21136B · 1284a | 4.83× | — | 64.64 µs · 9632B · 1585a | 1.685 ms · 296281B · 20528a | 1.137 ms · 59712B · 3431a | 2.338 ms · 50744B · 3848a | 1.834 ms · 209521B · 5520a | — | 2.448 ms · 503612B · 31846a |
| struct-tree-walk | 282.1 µs · 768B · 8a | 281.5 µs · 768B · 8a | 107.6 µs · 768B · 8a | 2.62× | — | 1.864 ns · 0B · 0a | 639.1 µs · 458379B · 5114a | 1.338 ms · 818512B · 11253a | 1.068 ms · 558961B · 6149a | 3.414 ms · 2570665B · 34797a | — | 1.248 µs · 600B · 17a |
| tail-ping-pong | 29.38 µs · 0B · 0a | 29.45 µs · 0B · 0a | 478.6 ns · 0B · 0a | 61.4× | — | 891.3 ns · 0B · 0a | 107.9 µs · 106112B · 2001a | 35.87 µs · 21856B · 85a | 114.8 µs · 13984B · 1726a | 118 µs · 14280B · 1731a | — | 558.7 µs · 348833B · 9016a |
| tail-sum | 29.23 µs · 0B · 0a | 29.38 µs · 0B · 0a | 698.5 ns · 0B · 0a | 41.8× | — | 302.6 ns · 0B · 0a | 110.6 µs · 106112B · 2001a | 35.73 µs · 21856B · 85a | 114.6 µs · 13984B · 1726a | 118.8 µs · 14280B · 1731a | — | 84.28 µs · 4608B · 1017a |
| typed-array-sum | 7.386 µs · 0B · 0a | 7.441 µs · 0B · 0a | 264.6 ns · 0B · 0a | 27.9× | 267.9 ns · 8B · 1a | 1.869 ns · 0B · 0a | 44.61 µs · 94208B · 513a | 8.587 µs · 4000B · 15a | 27.94 µs · 2080B · 238a | 17.77 µs · 2496B · 246a | — | — |
| xorshift-i64 | 116 µs · 18424B · 2046a | 113.4 µs · 18424B · 2046a | 1.366 µs · 18424B · 2046a | 84.9× | — | 356.9 ns · 0B · 0a | 103.1 µs · 107608B · 2188a | — | 985.2 µs · 447257B · 12736a | 421.9 µs · 199937B · 7772a | — | 104 µs · 13545B · 1317a |

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
