# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.213 µs · 0B · 0a | 8.204 µs · 0B · 0a | 5.31 µs · 0B · 0a | 1.55× | — | 2.26 µs · 1024B · 128a | 36.26 µs · 96288B · 388a | 17.39 µs · 14376B · 256a | 68.88 µs · 78016B · 770a | 14.52 µs · 5712B · 266a | — | 31.85 µs · 2308B · 276a |
| binary-trees | 1.64 ms · 768B · 8a | 1.643 ms · 768B · 8a | 552.7 µs · 768B · 8a | 2.97× | — | 287.6 µs · 201936B · 8414a | 3.891 ms · 1468184B · 62877a | 10.44 ms · 6995671B · 92703a | 8.879 ms · 5198699B · 69052a | 27.05 ms · 19457584B · 280714a | — | 30.85 ms · 15128171B · 446106a |
| branch-tree | 955 ns · 0B · 0a | 17.19 ns · 0B · 0a | 88.63 ns · 0B · 0a | 10.8× | 624.7 ns · 16B · 1a | 160.7 ns · 0B · 0a | 43.57 µs · 95385B · 660a | 15.17 µs · 2464B · 9a | 26.98 µs · 1992B · 196a | 24.2 µs · 2168B · 203a | — | 20.67 µs · 1640B · 219a |
| closure-counter | 5.223 µs · 64B · 2a | 5.252 µs · 64B · 2a | 949.4 ns · 64B · 2a | 5.5× | — | 40.48 ns · 0B · 0a | 38.85 µs · 92272B · 261a | 12.99 µs · 152B · 3a | 19.12 µs · 1264B · 13a | 79.1 µs · 58312B · 659a | — | 93.26 µs · 35628B · 922a |
| coroutine-gen | 57.74 µs · 128B · 2a | 57.89 µs · 128B · 2a | 57.87 µs · 128B · 2a | 0.998× | — | 42.75 µs · 208B · 7a | — | 325.4 µs · 149936B · 649a | 597.2 µs · 330650B · 6719a | 213.1 µs · 91800B · 3646a | — | — |
| fannkuch | 663.6 µs · 34608B · 1442a | 642 µs · 34608B · 1442a | 229.7 µs · 34608B · 1442a | 2.89× | — | 35.13 µs · 17280B · 720a | 2.033 ms · 456579B · 25621a | 1.52 ms · 443137B · 1460a | 19.33 ms · 14005357B · 164946a | 3.37 ms · 1367678B · 16944a | — | 4.868 ms · 2501436B · 88439a |
| fnv1a64 | 119.3 µs · 16400B · 2049a | 119.8 µs · 16400B · 2049a | 1.145 µs · 16400B · 2049a | 104× | — | 902.5 ns · 0B · 0a | 162.9 µs · 120808B · 3838a | — | 1.485 ms · 608257B · 18475a | 404.5 µs · 162921B · 7429a | — | 156.9 µs · 17282B · 2074a |
| host-call | 472.7 µs · 41752B · 3080a | 486.4 µs · 41752B · 3080a | 491.4 µs · 41752B · 3080a | 0.962× | — | 3.312 µs · 0B · 0a | 178.3 µs · 139264B · 3073a | 132.5 µs · 22536B · 1079a | 818.3 µs · 158048B · 14585a | 106.6 µs · 14920B · 1799a | — | 592.8 µs · 340637B · 9231a |
| i64-wide-fib | 27.38 ms · 2913456B · 364178a | 20.78 ms · 1942304B · 242785a | 682.2 µs · 16B · 1a | 40.1× | — | 239.6 µs · 0B · 0a | 21.92 µs · 90520B · 52a | — | 160 ms · 64193240B · 1942305a | 343.6 ms · 253472528B · 4248839a | — | 132.8 ms · 98686565B · 2138708a |
| indirect-recursive-fib | 978.5 µs · 0B · 0a | 970 µs · 0B · 0a | 60.45 µs · 0B · 0a | 16.2× | 99.43 µs · 8B · 1a | 34.79 µs · 0B · 0a | 2.124 ms · 319345B · 28655a | 1.946 ms · 704B · 2a | 2.527 ms · 4680B · 39a | 11.41 ms · 10158180B · 109494a | — | 25.96 ms · 13061338B · 394061a |
| iterative-fib | 893.1 ns · 0B · 0a | 876.9 ns · 0B · 0a | 76.69 ns · 0B · 0a | 11.6× | 86.74 ns · 8B · 1a | 12.41 ns · 0B · 0a | 23.28 µs · 90592B · 61a | 1.157 µs · 160B · 0a | 4.286 µs · 368B · 20a | 6.127 µs · 2448B · 88a | — | 6.275 µs · 944B · 81a |
| mandelbrot | 227.4 µs · 0B · 0a | 190.2 µs · 0B · 0a | 11.23 µs · 0B · 0a | 20.2× | — | 6.316 µs · 0B · 0a | 971.6 µs · 292346B · 25008a | 495.3 µs · 169408B · 661a | 8.013 ms · 5558650B · 104191a | 1.523 ms · 324978B · 23643a | — | 1.583 ms · 365398B · 22513a |
| map-churn | 2.424 ms · 786608B · 49a | 2.422 ms · 786608B · 49a | 622.2 µs · 786608B · 49a | 3.9× | — | 684.6 µs · 19024B · 5a | — | 11.36 ms · 7213815B · 6563a | 15.92 ms · 2369295B · 176707a | — | — | 56.37 ms · 24395128B · 725811a |
| map-iter | 3.047 ms · 249040B · 24645a | 3.046 ms · 249040B · 24645a | 2.992 ms · 249040B · 24645a | 1.02× | — | 536 µs · 19024B · 5a | — | 12.36 ms · 6177870B · 26325a | 22.84 ms · 13957121B · 201529a | — | — | 6.082 ms · 972932B · 93724a |
| map-ops | 61.04 µs · 12976B · 4a | 60.2 µs · 12976B · 4a | 11.35 µs · 12976B · 4a | 5.38× | — | 10.77 µs · 4904B · 3a | — | 271.4 µs · 114328B · 198a | 388.9 µs · 66728B · 4078a | — | — | 1.197 ms · 501522B · 15813a |
| map-string | 6.51 ms · 174184B · 9244a | 6.536 ms · 174184B · 9244a | 2.12 ms · 725176B · 13234a | 3.07× | — | 1.114 ms · 52632B · 2112a | 8.344 ms · 1463608B · 126292a | 9.508 ms · 967160B · 9917a | 16.35 ms · 590598B · 59508a | 9.031 ms · 1037368B · 80210a | — | 9.472 ms · 1281220B · 94103a |
| mat-mul | 310.7 µs · 6216B · 6a | 311 µs · 6216B · 6a | 8.475 µs · 6216B · 6a | 36.7× | — | 5.364 µs · 6144B · 3a | 1.386 ms · 431715B · 34871a | 665.7 µs · 143168B · 478a | 1.205 ms · 61304B · 537a | 1.48 ms · 90712B · 9350a | — | 924.9 µs · 36615B · 6036a |
| n-body | 495 µs · 504B · 14a | 412.3 µs · 504B · 14a | 18.84 µs · 504B · 14a | 26.3× | — | 7.355 µs · 0B · 0a | 1.539 ms · 386897B · 35892a | 1.225 ms · 258913B · 2002a | 3.396 ms · 658435B · 81545a | 2.521 ms · 382714B · 34975a | — | 1.768 ms · 445463B · 20669a |
| n-queens | 278.8 µs · 120B · 6a | 278.1 µs · 120B · 6a | 31.77 µs · 120B · 6a | 8.77× | — | 6.019 µs · 0B · 0a | 969.5 µs · 234865B · 17784a | 573.7 µs · 14423B · 63a | 1.093 ms · 7424B · 35a | 1.43 ms · 410346B · 5147a | — | 2.032 ms · 766007B · 33371a |
| naive-checksum | 187.7 µs · 0B · 0a | 107.8 µs · 0B · 0a | 8.715 µs · 0B · 0a | 21.5× | — | 2.759 µs · 0B · 0a | 623.8 µs · 235753B · 18206a | 477.8 µs · 174768B · 682a | 910.9 µs · 155640B · 19402a | 795.4 µs · 163816B · 20409a | — | 312.3 µs · 8977B · 2071a |
| permutation-flips | 104.5 µs · 7680B · 128a | 104.4 µs · 7680B · 128a | 17.26 µs · 7680B · 128a | 6.06× | — | 1.898 ns · 0B · 0a | 560.1 µs · 292858B · 9856a | 232.9 µs · 78808B · 451a | 527.5 µs · 122504B · 765a | 493.6 µs · 111976B · 2368a | — | 1.081 µs · 528B · 13a |
| recursive-fib-20 | 581.4 µs · 0B · 0a | 582.2 µs · 0B · 0a | 47.1 µs · 0B · 0a | 12.3× | 42.74 µs · 8B · 1a | 21.5 µs · 0B · 0a | 1.964 ms · 319346B · 28655a | 2.182 ms · 704B · 2a | 2.954 ms · 4680B · 39a | 11.58 ms · 9807921B · 109494a | — | 11.77 ms · 8302708B · 192848a |
| recursive-fib-35 | 791.6 ms · 0B · 0a | 791.9 ms · 0B · 0a | 64.33 ms · 0B · 0a | 12.3× | 58.19 ms · 8B · 1a | 29.01 ms · 0B · 0a | 2.646 s · 312797776B · 39088171a | 2.976 s · 971008B · 3793a | 4.023 s · 375360B · 46373a | 15.56 s · 13378017792B · 149349965a | — | — |
| sieve | 15.23 µs · 1048B · 2a | 15.21 µs · 1048B · 2a | 920.3 ns · 1048B · 2a | 16.5× | 1.037 µs · 8B · 1a | 371.8 ns · 0B · 0a | 139.1 µs · 122504B · 1611a | 50.86 µs · 18416B · 44a | 84.2 µs · 1872B · 25a | 77.53 µs · 5704B · 30a | — | 80.58 µs · 4368B · 795a |
| sort-stress | 386.2 µs · 5136B · 512a | 369 µs · 5136B · 512a | 12.73 µs · 5136B · 512a | 30.3× | — | 5.273 µs · 1024B · 2a | 1.315 ms · 270945B · 19965a | 844.2 µs · 36567B · 365a | 1.794 ms · 49104B · 3621a | 1.786 ms · 23448B · 2034a | — | 924.2 µs · 17475B · 1708a |
| spectral-norm | 432.9 µs · 648B · 6a | 353 µs · 648B · 6a | 8.509 µs · 648B · 6a | 50.9× | — | 7.053 µs · 576B · 3a | 2.063 ms · 571371B · 54822a | 1.083 ms · 234783B · 925a | 2.504 ms · 275337B · 33729a | 4.684 ms · 2457134B · 52718a | — | 4.298 ms · 2448014B · 74233a |
| string-build | 620 µs · 85408B · 4107a | 606.9 µs · 85408B · 4107a | 560.6 µs · 85408B · 4107a | 1.11× | — | 350.1 µs · 855890B · 5001a | 2.391 ms · 2211274B · 36101a | 2.288 ms · 1078110B · 13859a | 3.162 ms · 1147381B · 26883a | 2.479 ms · 1326244B · 24469a | — | 3.108 ms · 2372788B · 41432a |
| string-compare | 998.7 µs · 21136B · 1284a | 996.1 µs · 21136B · 1284a | 221.6 µs · 21136B · 1284a | 4.51× | — | 65.67 µs · 9632B · 1585a | 1.693 ms · 296281B · 20528a | 1.143 ms · 59712B · 3431a | 2.34 ms · 50745B · 3848a | 1.871 ms · 209521B · 5520a | — | 2.447 ms · 503605B · 31846a |
| struct-tree-walk | 248 µs · 768B · 8a | 248.2 µs · 768B · 8a | 102.1 µs · 768B · 8a | 2.43× | — | 1.899 ns · 0B · 0a | 644.5 µs · 458378B · 5114a | 1.356 ms · 818512B · 11253a | 1.082 ms · 558961B · 6149a | 3.461 ms · 2570665B · 34797a | — | 1.242 µs · 600B · 17a |
| tail-ping-pong | 21.61 µs · 0B · 0a | 21.6 µs · 0B · 0a | 473 ns · 0B · 0a | 45.7× | — | 890.7 ns · 0B · 0a | 108.2 µs · 106112B · 2001a | 35.65 µs · 21856B · 85a | 114.8 µs · 13984B · 1726a | 120.5 µs · 14280B · 1731a | — | 568.7 µs · 348834B · 9016a |
| tail-sum | 21.6 µs · 0B · 0a | 21.71 µs · 0B · 0a | 667.5 ns · 0B · 0a | 32.4× | — | 302.5 ns · 0B · 0a | 110 µs · 106112B · 2001a | 35.82 µs · 21856B · 85a | 115 µs · 13984B · 1726a | 119.7 µs · 14280B · 1731a | — | 84.7 µs · 4608B · 1017a |
| typed-array-sum | 5.677 µs · 0B · 0a | 5.696 µs · 0B · 0a | 262.9 ns · 0B · 0a | 21.6× | 261.9 ns · 8B · 1a | 1.878 ns · 0B · 0a | 42.29 µs · 94208B · 513a | 8.295 µs · 4000B · 15a | 28 µs · 2080B · 238a | 17.49 µs · 2496B · 246a | — | — |
| xorshift-i64 | 109.4 µs · 18424B · 2046a | 109.2 µs · 18424B · 2046a | 1.277 µs · 18424B · 2046a | 85.7× | — | 356.1 ns · 0B · 0a | 102.1 µs · 107608B · 2188a | — | 1.014 ms · 447255B · 12735a | 436.8 µs · 199938B · 7772a | — | 103.4 µs · 13546B · 1317a |

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
