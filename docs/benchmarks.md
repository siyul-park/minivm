# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.938 µs · 0B · 0a | 8.856 µs · 0B · 0a | 5.376 µs · 0B · 0a | 1.66× | — | 2.221 µs · 1024B · 128a | 38.25 µs · 96288B · 388a | 17.55 µs · 14376B · 256a | 73.67 µs · 78016B · 770a | 15.41 µs · 5712B · 266a | — | 31.51 µs · 2308B · 276a |
| binary-trees | 1.962 ms · 768B · 8a | 1.963 ms · 768B · 8a | 593.4 µs · 768B · 8a | 3.31× | — | 297.7 µs · 201936B · 8414a | 3.877 ms · 1468183B · 62877a | 10.76 ms · 6995672B · 92703a | 9.029 ms · 5198695B · 69052a | 28.22 ms · 19457617B · 280714a | — | 28.51 ms · 15128172B · 446106a |
| branch-tree | 1.351 µs · 0B · 0a | 20.13 ns · 0B · 0a | 84.79 ns · 0B · 0a | 15.9× | 626.8 ns · 16B · 1a | 160.4 ns · 0B · 0a | 46.44 µs · 95385B · 660a | 15.17 µs · 2464B · 9a | 26.26 µs · 1992B · 196a | 24.01 µs · 2168B · 203a | — | 20.58 µs · 1640B · 219a |
| closure-counter | 5.644 µs · 64B · 2a | 5.66 µs · 64B · 2a | 919.6 ns · 64B · 2a | 6.14× | — | 40.49 ns · 0B · 0a | 41.52 µs · 92272B · 261a | 12.98 µs · 151B · 3a | 19.07 µs · 1264B · 13a | 81.01 µs · 58312B · 659a | — | 87.75 µs · 35628B · 922a |
| coroutine-gen | 78.87 µs · 128B · 2a | 79.16 µs · 128B · 2a | 79.18 µs · 128B · 2a | 0.996× | — | 42.48 µs · 208B · 7a | — | 333.4 µs · 149936B · 649a | 605.8 µs · 330650B · 6719a | 202.7 µs · 91800B · 3646a | — | — |
| fannkuch | 827.9 µs · 34608B · 1442a | 814.6 µs · 34608B · 1442a | 216.1 µs · 34608B · 1442a | 3.83× | — | 35.77 µs · 17280B · 720a | 2.034 ms · 456578B · 25621a | 1.538 ms · 443135B · 1460a | 19.82 ms · 14005349B · 164946a | 3.412 ms · 1367678B · 16944a | — | 4.746 ms · 2501428B · 88439a |
| fnv1a64 | 123.1 µs · 16400B · 2049a | 122.1 µs · 16400B · 2049a | 1.16 µs · 16400B · 2049a | 106× | — | 902.7 ns · 0B · 0a | 165.8 µs · 120808B · 3838a | — | 1.504 ms · 608259B · 18475a | 400.2 µs · 162921B · 7429a | — | 154.7 µs · 17282B · 2074a |
| host-call | 462.1 µs · 41752B · 3080a | 475.9 µs · 41752B · 3080a | 481.9 µs · 41752B · 3080a | 0.959× | — | 3.314 µs · 0B · 0a | 182.4 µs · 139264B · 3073a | 131.6 µs · 22536B · 1079a | 788.5 µs · 158048B · 14585a | 105.6 µs · 14920B · 1799a | — | 578.2 µs · 340640B · 9231a |
| i64-wide-fib | 27.89 ms · 2913456B · 364178a | 20.83 ms · 1942304B · 242785a | 682.1 µs · 16B · 1a | 40.9× | — | 239.6 µs · 0B · 0a | 24.01 µs · 90520B · 52a | — | 160.5 ms · 64193328B · 1942305a | 351.6 ms · 253472656B · 4248840a | — | 132.2 ms · 98686666B · 2138708a |
| indirect-recursive-fib | 1.19 ms · 0B · 0a | 1.192 ms · 0B · 0a | 60.52 µs · 0B · 0a | 19.7× | 99.05 µs · 8B · 1a | 35.06 µs · 0B · 0a | 2.082 ms · 319345B · 28655a | 1.944 ms · 704B · 2a | 2.567 ms · 4680B · 39a | 11.5 ms · 10158192B · 109494a | — | 24.1 ms · 13061317B · 394061a |
| iterative-fib | 1.134 µs · 0B · 0a | 1.068 µs · 0B · 0a | 81.56 ns · 0B · 0a | 13.9× | 85.73 ns · 8B · 1a | 12.48 ns · 0B · 0a | 24.3 µs · 90592B · 61a | 1.159 µs · 160B · 0a | 4.222 µs · 368B · 20a | 6.257 µs · 2448B · 88a | — | 6.154 µs · 944B · 81a |
| mandelbrot | 289.7 µs · 0B · 0a | 248.7 µs · 0B · 0a | 11.26 µs · 0B · 0a | 25.7× | — | 6.321 µs · 0B · 0a | 959.3 µs · 292346B · 25008a | 489.4 µs · 169408B · 661a | 8.249 ms · 5558654B · 104191a | 1.515 ms · 324978B · 23643a | — | 1.528 ms · 365403B · 22513a |
| map-ops | 70.36 µs · 8816B · 3a | 70.05 µs · 8816B · 3a | 10.81 µs · 8816B · 3a | 6.51× | — | 10.88 µs · 4904B · 3a | — | 276.5 µs · 114328B · 198a | 385.1 µs · 66728B · 4078a | — | — | 1.139 ms · 501517B · 15813a |
| mat-mul | 375.5 µs · 6216B · 6a | 374.8 µs · 6216B · 6a | 8.617 µs · 6216B · 6a | 43.6× | — | 5.514 µs · 6144B · 3a | 1.367 ms · 431715B · 34871a | 673.3 µs · 143168B · 478a | 1.195 ms · 61304B · 537a | 1.456 ms · 90712B · 9350a | — | 915.2 µs · 36613B · 6036a |
| n-body | 608.9 µs · 504B · 14a | 584 µs · 504B · 14a | 19.04 µs · 504B · 14a | 32× | — | 7.358 µs · 0B · 0a | 1.506 ms · 386898B · 35892a | 1.22 ms · 258913B · 2002a | 3.284 ms · 658435B · 81545a | 2.4 ms · 382716B · 34975a | — | 1.754 ms · 445467B · 20669a |
| n-queens | 428.6 µs · 120B · 6a | 430.2 µs · 120B · 6a | 31.57 µs · 120B · 6a | 13.6× | — | 6.01 µs · 0B · 0a | 950.5 µs · 234865B · 17784a | 570 µs · 14424B · 63a | 1.096 ms · 7424B · 35a | 1.443 ms · 410346B · 5147a | — | 2.013 ms · 766000B · 33371a |
| naive-checksum | 252.3 µs · 0B · 0a | 142 µs · 0B · 0a | 8.728 µs · 0B · 0a | 28.9× | — | 2.758 µs · 0B · 0a | 616 µs · 235753B · 18206a | 485.5 µs · 174768B · 682a | 860.2 µs · 155640B · 19402a | 781.6 µs · 163816B · 20409a | — | 310.1 µs · 8976B · 2071a |
| permutation-flips | 127.8 µs · 7680B · 128a | 127.6 µs · 7680B · 128a | 17.27 µs · 7680B · 128a | 7.4× | — | 1.869 ns · 0B · 0a | 564.1 µs · 292857B · 9856a | 236.7 µs · 78808B · 451a | 528.1 µs · 122504B · 765a | 493.9 µs · 111976B · 2368a | — | 997.4 ns · 528B · 13a |
| recursive-fib-20 | 712.8 µs · 0B · 0a | 718 µs · 0B · 0a | 47.34 µs · 0B · 0a | 15.1× | 42.72 µs · 8B · 1a | 21.47 µs · 0B · 0a | 1.914 ms · 319346B · 28655a | 2.164 ms · 704B · 2a | 2.944 ms · 4680B · 39a | 11.81 ms · 9807920B · 109494a | — | 10.8 ms · 8302693B · 192848a |
| recursive-fib-35 | 971.9 ms · 0B · 0a | 980.3 ms · 0B · 0a | 63.88 ms · 0B · 0a | 15.2× | 58.24 ms · 8B · 1a | 29.38 ms · 0B · 0a | 2.568 s · 312797440B · 39088168a | 2.961 s · 971008B · 3793a | 4.016 s · 375360B · 46373a | 15.79 s · 13378015952B · 149349953a | — | — |
| sieve | 20.38 µs · 1048B · 2a | 20.36 µs · 1048B · 2a | 937.6 ns · 1048B · 2a | 21.7× | 1.045 µs · 8B · 1a | 371.9 ns · 0B · 0a | 137.7 µs · 122504B · 1611a | 50.42 µs · 18416B · 44a | 83.76 µs · 1872B · 25a | 77.09 µs · 5704B · 30a | — | 80.71 µs · 4368B · 795a |
| sort-stress | 503.4 µs · 5136B · 512a | 507 µs · 5136B · 512a | 12.66 µs · 5136B · 512a | 39.8× | — | 5.315 µs · 1024B · 2a | 1.307 ms · 270945B · 19965a | 846 µs · 36568B · 365a | 1.785 ms · 49104B · 3621a | 1.769 ms · 23448B · 2034a | — | 934.6 µs · 17475B · 1708a |
| spectral-norm | 541.2 µs · 648B · 6a | 462.1 µs · 648B · 6a | 8.371 µs · 648B · 6a | 64.6× | — | 7.065 µs · 576B · 3a | 2.147 ms · 571371B · 54822a | 1.084 ms · 234784B · 925a | 2.518 ms · 275336B · 33729a | 4.79 ms · 2457135B · 52718a | — | 4.341 ms · 2447999B · 74233a |
| string-build | 735.2 µs · 85408B · 4107a | 726.3 µs · 85408B · 4107a | 620.9 µs · 85408B · 4107a | 1.18× | — | 368.1 µs · 855889B · 5001a | 2.415 ms · 2211271B · 36101a | 2.405 ms · 1078110B · 13859a | 3.238 ms · 1147388B · 26883a | 2.471 ms · 1326247B · 24469a | — | 3.13 ms · 2372795B · 41432a |
| string-compare | 1.082 ms · 21136B · 1284a | 1.083 ms · 21136B · 1284a | 223 µs · 21136B · 1284a | 4.85× | — | 64.81 µs · 9632B · 1585a | 1.684 ms · 296281B · 20528a | 1.13 ms · 59712B · 3431a | 2.328 ms · 50744B · 3848a | 1.859 ms · 209521B · 5520a | — | 2.37 ms · 503607B · 31846a |
| struct-tree-walk | 281.7 µs · 768B · 8a | 281.8 µs · 768B · 8a | 107.9 µs · 768B · 8a | 2.61× | — | 1.895 ns · 0B · 0a | 655.9 µs · 458380B · 5114a | 1.377 ms · 818511B · 11253a | 1.087 ms · 558960B · 6149a | 3.544 ms · 2570661B · 34797a | — | 1.24 µs · 600B · 17a |
| tail-ping-pong | 29.49 µs · 0B · 0a | 29.42 µs · 0B · 0a | 475.8 ns · 0B · 0a | 62× | — | 891.7 ns · 0B · 0a | 108.2 µs · 106112B · 2001a | 35.53 µs · 21856B · 85a | 113.5 µs · 13984B · 1726a | 118.9 µs · 14280B · 1731a | — | 575.8 µs · 348833B · 9016a |
| tail-sum | 29.47 µs · 0B · 0a | 29.5 µs · 0B · 0a | 666.7 ns · 0B · 0a | 44.2× | — | 302.7 ns · 0B · 0a | 108 µs · 106112B · 2001a | 35.53 µs · 21856B · 85a | 113.8 µs · 13984B · 1726a | 119.3 µs · 14280B · 1731a | — | 84.15 µs · 4608B · 1017a |
| typed-array-sum | 7.335 µs · 0B · 0a | 7.373 µs · 0B · 0a | 259.4 ns · 0B · 0a | 28.3× | 266.2 ns · 8B · 1a | 1.865 ns · 0B · 0a | 45.86 µs · 94208B · 513a | 8.362 µs · 4000B · 15a | 27.08 µs · 2080B · 238a | 17.71 µs · 2496B · 246a | — | — |
| xorshift-i64 | 113 µs · 18424B · 2046a | 114.6 µs · 18424B · 2046a | 1.344 µs · 18424B · 2046a | 84.1× | — | 356.2 ns · 0B · 0a | 102.6 µs · 107608B · 2188a | — | 1.025 ms · 447252B · 12735a | 434.1 µs · 199939B · 7772a | — | 102.3 µs · 13545B · 1317a |

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
