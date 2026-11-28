# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.31 µs · 0B · 0a | 8.316 µs · 0B · 0a | 5.464 µs · 0B · 0a | 1.52× | — | 2.328 µs · 1024B · 128a | 38.79 µs · 96289B · 388a | 17.81 µs · 14376B · 256a | 72.63 µs · 78016B · 770a | 14.45 µs · 5712B · 266a | — | 30.95 µs · 2308B · 276a |
| binary-trees | 1.661 ms · 768B · 8a | 1.681 ms · 768B · 8a | 556.4 µs · 768B · 8a | 2.98× | — | 290.6 µs · 201936B · 8414a | 3.915 ms · 1468188B · 62877a | 10.69 ms · 6995674B · 92703a | 9.108 ms · 5198699B · 69052a | 27.98 ms · 19457584B · 280714a | — | 29.5 ms · 15128165B · 446106a |
| branch-tree | 947.2 ns · 0B · 0a | 18.95 ns · 0B · 0a | 91.39 ns · 0B · 0a | 10.4× | 626 ns · 16B · 1a | 160.4 ns · 0B · 0a | 45.94 µs · 95384B · 660a | 15.36 µs · 2464B · 9a | 26.46 µs · 1992B · 196a | 24.09 µs · 2168B · 203a | — | 20.58 µs · 1640B · 219a |
| closure-counter | 5.138 µs · 64B · 2a | 5.143 µs · 64B · 2a | 900.8 ns · 64B · 2a | 5.7× | — | 40.49 ns · 0B · 0a | 40.33 µs · 92272B · 261a | 12.96 µs · 151B · 3a | 19.02 µs · 1264B · 13a | 81.58 µs · 58312B · 659a | — | 91.96 µs · 35629B · 922a |
| coroutine-gen | 57.93 µs · 128B · 2a | 58.11 µs · 128B · 2a | 58.55 µs · 128B · 2a | 0.99× | — | 42.41 µs · 208B · 7a | — | 328.1 µs · 149936B · 649a | 609 µs · 330650B · 6719a | 200.7 µs · 91800B · 3646a | — | — |
| fannkuch | 659.8 µs · 34608B · 1442a | 632 µs · 34608B · 1442a | 221.3 µs · 34608B · 1442a | 2.98× | — | 35.56 µs · 17280B · 720a | 2.039 ms · 456578B · 25621a | 1.54 ms · 443136B · 1460a | 19.62 ms · 14005344B · 164946a | 3.411 ms · 1367678B · 16944a | — | 4.945 ms · 2501449B · 88439a |
| fnv1a64 | 117.3 µs · 16400B · 2049a | 117.8 µs · 16400B · 2049a | 1.145 µs · 16400B · 2049a | 102× | — | 902.4 ns · 0B · 0a | 163.4 µs · 120808B · 3838a | — | 1.516 ms · 608260B · 18475a | 401.1 µs · 162921B · 7429a | — | 153.5 µs · 17282B · 2074a |
| host-call | 465.2 µs · 41752B · 3080a | 477.7 µs · 41752B · 3080a | 473.1 µs · 41752B · 3080a | 0.983× | — | 3.316 µs · 0B · 0a | 182.4 µs · 139264B · 3073a | 133.3 µs · 22536B · 1079a | 802.9 µs · 158048B · 14585a | 107.2 µs · 14920B · 1799a | — | 596.4 µs · 340640B · 9231a |
| i64-wide-fib | 27.15 ms · 2913456B · 364178a | 20.44 ms · 1942304B · 242785a | 682 µs · 16B · 1a | 39.8× | — | 239.4 µs · 0B · 0a | 25.17 µs · 90520B · 52a | — | 161.6 ms · 64193168B · 1942305a | 350.4 ms · 253472816B · 4248842a | — | 135.6 ms · 98686624B · 2138708a |
| indirect-recursive-fib | 978.8 µs · 0B · 0a | 973.1 µs · 0B · 0a | 60.5 µs · 0B · 0a | 16.2× | 99.02 µs · 8B · 1a | 34.78 µs · 0B · 0a | 2.099 ms · 319345B · 28655a | 1.948 ms · 703B · 2a | 2.522 ms · 4680B · 39a | 11.76 ms · 10158194B · 109494a | — | 25.98 ms · 13061294B · 394061a |
| iterative-fib | 885.9 ns · 0B · 0a | 880.6 ns · 0B · 0a | 81.68 ns · 0B · 0a | 10.8× | 85.6 ns · 8B · 1a | 12.42 ns · 0B · 0a | 25.69 µs · 90592B · 61a | 1.15 µs · 160B · 0a | 4.244 µs · 368B · 20a | 6.15 µs · 2448B · 88a | — | 6.166 µs · 944B · 81a |
| mandelbrot | 227.3 µs · 0B · 0a | 192.5 µs · 0B · 0a | 11.26 µs · 0B · 0a | 20.2× | — | 6.325 µs · 0B · 0a | 972.4 µs · 292345B · 25008a | 489.6 µs · 169408B · 661a | 8.149 ms · 5558651B · 104191a | 1.504 ms · 324979B · 23643a | — | 1.549 ms · 365401B · 22513a |
| map-churn | 2.445 ms · 786608B · 49a | 2.413 ms · 786608B · 49a | 621.2 µs · 786608B · 49a | 3.94× | — | 684.7 µs · 19024B · 5a | — | 11.63 ms · 7213833B · 6564a | 15.95 ms · 2369306B · 176707a | — | — | 54.89 ms · 24395136B · 725811a |
| map-iter | 3.051 ms · 249040B · 24645a | 3.046 ms · 249040B · 24645a | 2.997 ms · 249040B · 24645a | 1.02× | — | 535.6 µs · 19024B · 5a | — | 12.66 ms · 6177884B · 26325a | 22.77 ms · 13957134B · 201529a | — | — | 5.929 ms · 972952B · 93724a |
| map-ops | 60.73 µs · 12976B · 4a | 60.68 µs · 12976B · 4a | 11.43 µs · 12976B · 4a | 5.32× | — | 10.87 µs · 4904B · 3a | — | 278.1 µs · 114327B · 198a | 385.8 µs · 66728B · 4078a | — | — | 1.154 ms · 501521B · 15813a |
| map-string | 6.503 ms · 174184B · 9244a | 6.488 ms · 174184B · 9244a | 2.074 ms · 572272B · 11791a | 3.14× | — | 1.112 ms · 52632B · 2112a | 8.319 ms · 1463603B · 126292a | 9.471 ms · 967142B · 9917a | 16.12 ms · 590603B · 59508a | 8.842 ms · 1037299B · 80209a | — | 9.326 ms · 1281251B · 94104a |
| mat-mul | 316.2 µs · 6216B · 6a | 312.1 µs · 6216B · 6a | 8.596 µs · 6216B · 6a | 36.8× | — | 5.455 µs · 6144B · 3a | 1.379 ms · 431714B · 34871a | 685.3 µs · 143168B · 478a | 1.211 ms · 61305B · 537a | 1.475 ms · 90712B · 9350a | — | 918.1 µs · 36612B · 6036a |
| n-body | 495.9 µs · 504B · 14a | 412.7 µs · 504B · 14a | 17.67 µs · 504B · 14a | 28.1× | — | 7.359 µs · 0B · 0a | 1.522 ms · 386898B · 35892a | 1.218 ms · 258913B · 2002a | 3.373 ms · 658435B · 81545a | 2.471 ms · 382713B · 34975a | — | 1.765 ms · 445462B · 20669a |
| n-queens | 278.5 µs · 120B · 6a | 280.5 µs · 120B · 6a | 31.71 µs · 120B · 6a | 8.78× | — | 6.009 µs · 0B · 0a | 957.5 µs · 234865B · 17784a | 569.6 µs · 14423B · 63a | 1.093 ms · 7424B · 35a | 1.436 ms · 410346B · 5147a | — | 2.028 ms · 766003B · 33371a |
| naive-checksum | 188.7 µs · 0B · 0a | 108.2 µs · 0B · 0a | 8.769 µs · 0B · 0a | 21.5× | — | 2.758 µs · 0B · 0a | 618.5 µs · 235753B · 18206a | 480 µs · 174768B · 682a | 911.4 µs · 155640B · 19402a | 805.4 µs · 163816B · 20409a | — | 309.8 µs · 8977B · 2071a |
| permutation-flips | 103.7 µs · 7680B · 128a | 103.7 µs · 7680B · 128a | 17.19 µs · 7680B · 128a | 6.03× | — | 1.899 ns · 0B · 0a | 568.8 µs · 292857B · 9856a | 234.1 µs · 78808B · 451a | 530.7 µs · 122504B · 765a | 501.5 µs · 111976B · 2368a | — | 1.043 µs · 528B · 13a |
| recursive-fib-20 | 581 µs · 0B · 0a | 582.3 µs · 0B · 0a | 47.08 µs · 0B · 0a | 12.3× | 42.77 µs · 8B · 1a | 21.5 µs · 0B · 0a | 1.95 ms · 319345B · 28655a | 2.177 ms · 704B · 2a | 2.955 ms · 4680B · 39a | 11.48 ms · 9807914B · 109494a | — | 11.31 ms · 8302685B · 192847a |
| recursive-fib-35 | 797.5 ms · 0B · 0a | 794.4 ms · 0B · 0a | 64.04 ms · 0B · 0a | 12.5× | 58.19 ms · 8B · 1a | 29.14 ms · 0B · 0a | 2.596 s · 312797280B · 39088167a | 3.001 s · 971008B · 3793a | 4.025 s · 375360B · 46373a | 15.75 s · 13378017072B · 149349957a | — | — |
| sieve | 15.26 µs · 1048B · 2a | 15.24 µs · 1048B · 2a | 932.6 ns · 1048B · 2a | 16.4× | 1.01 µs · 8B · 1a | 371.9 ns · 0B · 0a | 139.5 µs · 122504B · 1611a | 50.47 µs · 18416B · 44a | 83.95 µs · 1872B · 25a | 77.59 µs · 5704B · 30a | — | 80.73 µs · 4368B · 795a |
| sort-stress | 387.7 µs · 5136B · 512a | 370.1 µs · 5136B · 512a | 12.76 µs · 5136B · 512a | 30.4× | — | 5.329 µs · 1024B · 2a | 1.312 ms · 270945B · 19965a | 847.7 µs · 36568B · 365a | 1.796 ms · 49104B · 3621a | 1.783 ms · 23448B · 2034a | — | 924.6 µs · 17475B · 1708a |
| spectral-norm | 432.3 µs · 648B · 6a | 347.8 µs · 648B · 6a | 8.697 µs · 648B · 6a | 49.7× | — | 7.046 µs · 576B · 3a | 2.116 ms · 571372B · 54822a | 1.084 ms · 234784B · 925a | 2.457 ms · 275338B · 33729a | 4.724 ms · 2457136B · 52718a | — | 4.32 ms · 2448013B · 74233a |
| string-build | 610.6 µs · 85408B · 4107a | 601.7 µs · 85408B · 4107a | 480 µs · 85408B · 4107a | 1.27× | — | 362.4 µs · 855889B · 5001a | 2.415 ms · 2211276B · 36101a | 2.3 ms · 1078111B · 13859a | 3.133 ms · 1147382B · 26883a | 2.483 ms · 1326242B · 24469a | — | 3.169 ms · 2372783B · 41432a |
| string-compare | 992.3 µs · 21136B · 1284a | 991.6 µs · 21136B · 1284a | 217.8 µs · 21136B · 1284a | 4.56× | — | 65.04 µs · 9632B · 1585a | 1.688 ms · 296281B · 20528a | 1.131 ms · 59712B · 3431a | 2.332 ms · 50744B · 3848a | 1.846 ms · 209520B · 5520a | — | 2.382 ms · 503620B · 31846a |
| struct-tree-walk | 248.8 µs · 768B · 8a | 248.7 µs · 768B · 8a | 101.9 µs · 768B · 8a | 2.44× | — | 1.898 ns · 0B · 0a | 654.1 µs · 458378B · 5114a | 1.377 ms · 818512B · 11253a | 1.095 ms · 558960B · 6149a | 3.522 ms · 2570660B · 34797a | — | 1.24 µs · 600B · 17a |
| tail-ping-pong | 21.68 µs · 0B · 0a | 21.64 µs · 0B · 0a | 474.8 ns · 0B · 0a | 45.7× | — | 891.3 ns · 0B · 0a | 107.8 µs · 106112B · 2001a | 35.7 µs · 21855B · 85a | 113.2 µs · 13984B · 1726a | 119.1 µs · 14280B · 1731a | — | 565.8 µs · 348835B · 9016a |
| tail-sum | 21.69 µs · 0B · 0a | 21.65 µs · 0B · 0a | 658.8 ns · 0B · 0a | 32.9× | — | 302.6 ns · 0B · 0a | 109.7 µs · 106112B · 2001a | 36.34 µs · 21856B · 85a | 114.7 µs · 13984B · 1726a | 119.3 µs · 14280B · 1731a | — | 84.38 µs · 4608B · 1017a |
| typed-array-sum | 5.664 µs · 0B · 0a | 5.667 µs · 0B · 0a | 263.7 ns · 0B · 0a | 21.5× | 260.8 ns · 8B · 1a | 1.876 ns · 0B · 0a | 43.86 µs · 94208B · 513a | 8.359 µs · 4000B · 15a | 27.77 µs · 2080B · 238a | 17.4 µs · 2496B · 246a | — | — |
| xorshift-i64 | 107.3 µs · 18424B · 2046a | 106.5 µs · 18424B · 2046a | 1.32 µs · 18424B · 2046a | 81.3× | — | 356.1 ns · 0B · 0a | 103.6 µs · 107608B · 2188a | — | 1.006 ms · 447253B · 12735a | 438.3 µs · 199937B · 7772a | — | 101.8 µs · 13545B · 1317a |

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
