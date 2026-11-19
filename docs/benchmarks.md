# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.993 µs · 0B · 0a | 8.923 µs · 0B · 0a | 5.417 µs · 0B · 0a | 1.66× | — | 2.312 µs · 1024B · 128a | 38.59 µs · 96288B · 388a | 17.22 µs · 14376B · 256a | 78.01 µs · 78016B · 770a | 16.72 µs · 5712B · 266a | — | 30.88 µs · 2308B · 276a |
| binary-trees | 1.962 ms · 768B · 8a | 1.952 ms · 768B · 8a | 591.3 µs · 768B · 8a | 3.32× | — | 289.6 µs · 201940B · 8414a | 3.889 ms · 1468187B · 62877a | 10.6 ms · 6995677B · 92703a | 9.136 ms · 5198701B · 69052a | 27.86 ms · 19457621B · 280714a | — | 29.92 ms · 15128164B · 446106a |
| branch-tree | 1.323 µs · 0B · 0a | 19.72 ns · 0B · 0a | 91.92 ns · 0B · 0a | 14.4× | 623.9 ns · 16B · 1a | 160.6 ns · 0B · 0a | 47.18 µs · 95384B · 660a | 15.48 µs · 2464B · 9a | 26.66 µs · 1992B · 196a | 24.7 µs · 2168B · 203a | — | 20.69 µs · 1640B · 219a |
| closure-counter | 5.556 µs · 64B · 2a | 5.524 µs · 64B · 2a | 923.9 ns · 64B · 2a | 6.01× | — | 40.5 ns · 0B · 0a | 40.68 µs · 92272B · 261a | 12.87 µs · 151B · 3a | 19.23 µs · 1264B · 13a | 80.08 µs · 58312B · 659a | — | 89.04 µs · 35629B · 922a |
| coroutine-gen | 79.99 µs · 128B · 2a | 79.56 µs · 128B · 2a | 79.47 µs · 128B · 2a | 1.01× | — | 42.09 µs · 208B · 7a | — | 330.2 µs · 149936B · 649a | 609.1 µs · 330649B · 6719a | 199.8 µs · 91800B · 3646a | — | — |
| fannkuch | 829.1 µs · 34608B · 1442a | 815.3 µs · 34608B · 1442a | 218.9 µs · 34608B · 1442a | 3.79× | — | 35.92 µs · 17280B · 720a | 2.053 ms · 456579B · 25621a | 1.537 ms · 443136B · 1460a | 19.31 ms · 14005345B · 164946a | 3.406 ms · 1367677B · 16944a | — | 4.905 ms · 2501450B · 88439a |
| fnv1a64 | 126 µs · 16400B · 2049a | 125 µs · 16400B · 2049a | 1.166 µs · 16400B · 2049a | 108× | — | 903 ns · 0B · 0a | 163.1 µs · 120808B · 3838a | — | 1.514 ms · 608256B · 18475a | 398.9 µs · 162921B · 7429a | — | 153.4 µs · 17282B · 2074a |
| host-call | 462.5 µs · 41752B · 3080a | 474.7 µs · 41752B · 3080a | 472.8 µs · 41752B · 3080a | 0.978× | — | 3.312 µs · 0B · 0a | 180.8 µs · 139264B · 3073a | 134.7 µs · 22536B · 1079a | 801.7 µs · 158048B · 14585a | 106.9 µs · 14920B · 1799a | — | 581.9 µs · 340638B · 9231a |
| i64-wide-fib | 28.2 ms · 2913456B · 364178a | 21.11 ms · 1942304B · 242785a | 680.6 µs · 16B · 1a | 41.4× | — | 239.1 µs · 0B · 0a | 23.03 µs · 90520B · 52a | — | 161.1 ms · 64193312B · 1942305a | 345.7 ms · 253472672B · 4248839a | — | 128.3 ms · 98686581B · 2138708a |
| indirect-recursive-fib | 1.189 ms · 0B · 0a | 1.191 ms · 0B · 0a | 60.52 µs · 0B · 0a | 19.6× | 100.3 µs · 8B · 1a | 35.07 µs · 0B · 0a | 2.083 ms · 319346B · 28655a | 1.939 ms · 704B · 2a | 2.597 ms · 4680B · 39a | 11.66 ms · 10158195B · 109494a | — | 24.74 ms · 13061300B · 394061a |
| iterative-fib | 1.131 µs · 0B · 0a | 1.066 µs · 0B · 0a | 81.85 ns · 0B · 0a | 13.8× | 84.46 ns · 8B · 1a | 12.41 ns · 0B · 0a | 25.11 µs · 90592B · 61a | 1.151 µs · 159B · 0a | 4.266 µs · 368B · 20a | 6.221 µs · 2448B · 88a | — | 6.117 µs · 944B · 81a |
| mandelbrot | 291.2 µs · 0B · 0a | 248.3 µs · 0B · 0a | 11.33 µs · 0B · 0a | 25.7× | — | 6.322 µs · 0B · 0a | 962.9 µs · 292346B · 25008a | 484.8 µs · 169408B · 661a | 8.256 ms · 5558648B · 104191a | 1.503 ms · 324978B · 23643a | — | 1.555 ms · 365397B · 22513a |
| map-ops | 70.97 µs · 8816B · 3a | 70.64 µs · 8816B · 3a | 10.92 µs · 8816B · 3a | 6.5× | — | 10.96 µs · 4904B · 3a | — | 282.1 µs · 114328B · 198a | 377.7 µs · 66728B · 4078a | — | — | 1.112 ms · 501523B · 15813a |
| mat-mul | 375.8 µs · 6216B · 6a | 374.6 µs · 6216B · 6a | 8.743 µs · 6216B · 6a | 43× | — | 5.664 µs · 6144B · 3a | 1.385 ms · 431715B · 34871a | 666 µs · 143168B · 478a | 1.204 ms · 61304B · 537a | 1.468 ms · 90712B · 9350a | — | 929.6 µs · 36616B · 6036a |
| n-body | 613.2 µs · 504B · 14a | 583.8 µs · 504B · 14a | 19.12 µs · 504B · 14a | 32.1× | — | 7.357 µs · 0B · 0a | 1.532 ms · 386899B · 35892a | 1.226 ms · 258913B · 2002a | 3.306 ms · 658435B · 81545a | 2.423 ms · 382713B · 34975a | — | 1.751 ms · 445468B · 20669a |
| n-queens | 431.7 µs · 120B · 6a | 431.3 µs · 120B · 6a | 31.63 µs · 120B · 6a | 13.6× | — | 6.029 µs · 0B · 0a | 960.4 µs · 234865B · 17784a | 568.8 µs · 14423B · 63a | 1.097 ms · 7424B · 35a | 1.441 ms · 410345B · 5147a | — | 2.023 ms · 766003B · 33371a |
| naive-checksum | 251.5 µs · 0B · 0a | 142.4 µs · 0B · 0a | 8.724 µs · 0B · 0a | 28.8× | — | 2.76 µs · 0B · 0a | 603.8 µs · 235753B · 18206a | 483.5 µs · 174768B · 682a | 878.8 µs · 155640B · 19402a | 798.2 µs · 163817B · 20409a | — | 310.8 µs · 8977B · 2071a |
| permutation-flips | 129.1 µs · 7680B · 128a | 129 µs · 7680B · 128a | 17.46 µs · 7680B · 128a | 7.39× | — | 1.879 ns · 0B · 0a | 573.5 µs · 292858B · 9856a | 234.2 µs · 78808B · 451a | 526.8 µs · 122504B · 765a | 499.3 µs · 111976B · 2368a | — | 1.017 µs · 528B · 13a |
| recursive-fib-20 | 711.9 µs · 0B · 0a | 716.7 µs · 0B · 0a | 47.16 µs · 0B · 0a | 15.1× | 42.74 µs · 8B · 1a | 21.44 µs · 0B · 0a | 1.948 ms · 319346B · 28655a | 2.146 ms · 704B · 2a | 3 ms · 4680B · 39a | 11.69 ms · 9807912B · 109494a | — | 10.68 ms · 8302698B · 192848a |
| recursive-fib-35 | 968.6 ms · 0B · 0a | 979.4 ms · 0B · 0a | 63.95 ms · 0B · 0a | 15.1× | 58.21 ms · 8B · 1a | 29.01 ms · 0B · 0a | 2.608 s · 312796992B · 39088167a | 2.933 s · 971008B · 3793a | 4.087 s · 375360B · 46373a | 15.9 s · 13378019696B · 149349984a | — | — |
| sieve | 20.34 µs · 1048B · 2a | 20.4 µs · 1048B · 2a | 928.6 ns · 1048B · 2a | 21.9× | 1.043 µs · 8B · 1a | 371.8 ns · 0B · 0a | 140.5 µs · 122504B · 1611a | 50.29 µs · 18416B · 44a | 84.36 µs · 1872B · 25a | 76.71 µs · 5704B · 30a | — | 81.88 µs · 4368B · 795a |
| sort-stress | 504.8 µs · 5136B · 512a | 507 µs · 5136B · 512a | 12.62 µs · 5136B · 512a | 40× | — | 5.312 µs · 1024B · 2a | 1.318 ms · 270945B · 19965a | 841.9 µs · 36568B · 365a | 1.799 ms · 49104B · 3621a | 1.781 ms · 23448B · 2034a | — | 929.1 µs · 17475B · 1708a |
| spectral-norm | 541.8 µs · 648B · 6a | 464.1 µs · 648B · 6a | 8.461 µs · 648B · 6a | 64× | — | 7.032 µs · 576B · 3a | 2.136 ms · 571371B · 54822a | 1.076 ms · 234784B · 925a | 2.579 ms · 275337B · 33729a | 4.737 ms · 2457135B · 52718a | — | 4.288 ms · 2448012B · 74233a |
| string-build | 735.1 µs · 85408B · 4107a | 721.2 µs · 85408B · 4107a | 605 µs · 85408B · 4107a | 1.22× | — | 373.3 µs · 855890B · 5001a | 2.42 ms · 2211276B · 36101a | 2.318 ms · 1078111B · 13859a | 3.12 ms · 1147380B · 26883a | 2.435 ms · 1326246B · 24469a | — | 3.145 ms · 2372781B · 41432a |
| string-compare | 1.084 ms · 21136B · 1284a | 1.083 ms · 21136B · 1284a | 223.1 µs · 21136B · 1284a | 4.86× | — | 64.68 µs · 9632B · 1585a | 1.682 ms · 296281B · 20528a | 1.129 ms · 59712B · 3431a | 2.331 ms · 50744B · 3848a | 1.796 ms · 209521B · 5520a | — | 2.401 ms · 503607B · 31846a |
| struct-tree-walk | 282.5 µs · 768B · 8a | 282.3 µs · 768B · 8a | 107.9 µs · 768B · 8a | 2.62× | — | 1.899 ns · 0B · 0a | 655.1 µs · 458380B · 5114a | 1.374 ms · 818513B · 11253a | 1.099 ms · 558960B · 6149a | 3.514 ms · 2570667B · 34797a | — | 1.23 µs · 600B · 17a |
| tail-ping-pong | 29.5 µs · 0B · 0a | 29.49 µs · 0B · 0a | 485.3 ns · 0B · 0a | 60.8× | — | 890.9 ns · 0B · 0a | 108.5 µs · 106112B · 2001a | 35.42 µs · 21856B · 85a | 111 µs · 13984B · 1726a | 119 µs · 14280B · 1731a | — | 580.7 µs · 348834B · 9016a |
| tail-sum | 29.26 µs · 0B · 0a | 29.5 µs · 0B · 0a | 682.6 ns · 0B · 0a | 42.9× | — | 302.5 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 35.52 µs · 21856B · 85a | 110.9 µs · 13984B · 1726a | 119.3 µs · 14280B · 1731a | — | 84.8 µs · 4608B · 1017a |
| typed-array-sum | 7.358 µs · 0B · 0a | 7.357 µs · 0B · 0a | 264.2 ns · 0B · 0a | 27.9× | 260 ns · 8B · 1a | 1.897 ns · 0B · 0a | 45.91 µs · 94208B · 513a | 8.381 µs · 4000B · 15a | 27.48 µs · 2080B · 238a | 17.36 µs · 2496B · 246a | — | — |
| xorshift-i64 | 115.6 µs · 18424B · 2046a | 115.6 µs · 18424B · 2046a | 1.357 µs · 18424B · 2046a | 85.2× | — | 357.2 ns · 0B · 0a | 103 µs · 107608B · 2188a | — | 1.012 ms · 447255B · 12736a | 435.1 µs · 199938B · 7772a | — | 101.8 µs · 13545B · 1317a |

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
