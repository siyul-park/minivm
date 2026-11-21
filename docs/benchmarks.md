# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.975 µs · 0B · 0a | 8.928 µs · 0B · 0a | 5.275 µs · 0B · 0a | 1.7× | — | 2.222 µs · 1024B · 128a | 37.33 µs · 96288B · 388a | 16.74 µs · 14376B · 256a | 69.82 µs · 78016B · 770a | 14.34 µs · 5712B · 266a | — | 31.78 µs · 2308B · 276a |
| binary-trees | 1.971 ms · 768B · 8a | 1.956 ms · 768B · 8a | 591.7 µs · 768B · 8a | 3.33× | — | 296.4 µs · 201936B · 8414a | 3.854 ms · 1468184B · 62877a | 10.31 ms · 6995838B · 92703a | 8.796 ms · 5198702B · 69052a | 26.95 ms · 19457603B · 280714a | — | 29.9 ms · 15128168B · 446106a |
| branch-tree | 1.316 µs · 0B · 0a | 20.59 ns · 0B · 0a | 87.05 ns · 0B · 0a | 15.1× | 625.3 ns · 16B · 1a | 160.5 ns · 0B · 0a | 46.08 µs · 95384B · 660a | 15.46 µs · 2464B · 9a | 26.98 µs · 1992B · 196a | 24.8 µs · 2168B · 203a | — | 20.98 µs · 1640B · 219a |
| closure-counter | 5.599 µs · 64B · 2a | 5.587 µs · 64B · 2a | 924 ns · 64B · 2a | 6.06× | — | 40.14 ns · 0B · 0a | 40.12 µs · 92272B · 261a | 12.99 µs · 152B · 3a | 19.17 µs · 1264B · 13a | 77.87 µs · 58312B · 659a | — | 91.55 µs · 35628B · 922a |
| coroutine-gen | 78.92 µs · 128B · 2a | 78.77 µs · 128B · 2a | 78.72 µs · 128B · 2a | 1× | — | 41.51 µs · 208B · 7a | — | 329.9 µs · 149936B · 649a | 589.7 µs · 330649B · 6719a | 198.5 µs · 91800B · 3646a | — | — |
| fannkuch | 838.1 µs · 34608B · 1442a | 823.4 µs · 34608B · 1442a | 220.6 µs · 34608B · 1442a | 3.8× | — | 35.19 µs · 17280B · 720a | 2.065 ms · 456578B · 25621a | 1.547 ms · 443135B · 1460a | 19.14 ms · 14005336B · 164946a | 3.353 ms · 1367677B · 16944a | — | 4.802 ms · 2501442B · 88439a |
| fnv1a64 | 129.3 µs · 16400B · 2049a | 128.3 µs · 16400B · 2049a | 1.159 µs · 16400B · 2049a | 112× | — | 902.4 ns · 0B · 0a | 161.8 µs · 120809B · 3838a | — | 1.475 ms · 608253B · 18475a | 396.2 µs · 162921B · 7429a | — | 158.3 µs · 17283B · 2074a |
| host-call | 465.6 µs · 41752B · 3080a | 482.6 µs · 41752B · 3080a | 480.5 µs · 41752B · 3080a | 0.969× | — | 3.312 µs · 0B · 0a | 178.4 µs · 139264B · 3073a | 134.3 µs · 22536B · 1079a | 827 µs · 158049B · 14585a | 106.3 µs · 14920B · 1799a | — | 599.6 µs · 340638B · 9231a |
| i64-wide-fib | 29.22 ms · 2913456B · 364178a | 21.36 ms · 1942304B · 242785a | 679.7 µs · 16B · 1a | 43× | — | 238.9 µs · 0B · 0a | 22.47 µs · 90520B · 52a | — | 155.4 ms · 64193216B · 1942305a | 343.3 ms · 253472800B · 4248841a | — | 131.7 ms · 98686576B · 2138708a |
| indirect-recursive-fib | 1.201 ms · 0B · 0a | 1.221 ms · 0B · 0a | 60.46 µs · 0B · 0a | 19.9× | 99.27 µs · 8B · 1a | 34.76 µs · 0B · 0a | 2.099 ms · 319345B · 28655a | 1.946 ms · 704B · 2a | 2.575 ms · 4680B · 39a | 11.36 ms · 10158190B · 109494a | — | 26.73 ms · 13061343B · 394061a |
| iterative-fib | 1.136 µs · 0B · 0a | 1.075 µs · 0B · 0a | 86.53 ns · 0B · 0a | 13.1× | 85.69 ns · 8B · 1a | 11.89 ns · 0B · 0a | 23.73 µs · 90592B · 61a | 1.157 µs · 159B · 0a | 4.263 µs · 368B · 20a | 6.087 µs · 2448B · 88a | — | 6.242 µs · 944B · 81a |
| mandelbrot | 291.2 µs · 0B · 0a | 250.2 µs · 0B · 0a | 11.25 µs · 0B · 0a | 25.9× | — | 6.324 µs · 0B · 0a | 967.5 µs · 292345B · 25008a | 479.6 µs · 169408B · 661a | 8.136 ms · 5558648B · 104191a | 1.5 ms · 324978B · 23643a | — | 1.574 ms · 365406B · 22513a |
| map-ops | 70.42 µs · 8816B · 3a | 70.24 µs · 8816B · 3a | 10.74 µs · 8816B · 3a | 6.55× | — | 10.92 µs · 4904B · 3a | — | 277.9 µs · 114328B · 198a | 387.1 µs · 66728B · 4078a | — | — | 1.211 ms · 501521B · 15813a |
| mat-mul | 375 µs · 6216B · 6a | 375.6 µs · 6216B · 6a | 8.536 µs · 6216B · 6a | 43.9× | — | 5.486 µs · 6144B · 3a | 1.378 ms · 431714B · 34871a | 669.5 µs · 143168B · 478a | 1.23 ms · 61304B · 537a | 1.47 ms · 90712B · 9350a | — | 932.8 µs · 36614B · 6036a |
| n-body | 613.8 µs · 504B · 14a | 581.1 µs · 504B · 14a | 18.93 µs · 504B · 14a | 32.4× | — | 7.345 µs · 0B · 0a | 1.524 ms · 386898B · 35892a | 1.202 ms · 258913B · 2002a | 3.371 ms · 658436B · 81545a | 2.454 ms · 382714B · 34975a | — | 1.792 ms · 445464B · 20669a |
| n-queens | 431.8 µs · 120B · 6a | 432.3 µs · 120B · 6a | 31.54 µs · 120B · 6a | 13.7× | — | 5.998 µs · 0B · 0a | 955.5 µs · 234865B · 17784a | 572.5 µs · 14423B · 63a | 1.109 ms · 7424B · 35a | 1.439 ms · 410346B · 5147a | — | 2.018 ms · 766003B · 33371a |
| naive-checksum | 253.3 µs · 0B · 0a | 142.8 µs · 0B · 0a | 8.782 µs · 0B · 0a | 28.8× | — | 2.758 µs · 0B · 0a | 619.7 µs · 235753B · 18206a | 471.1 µs · 174767B · 682a | 901.7 µs · 155640B · 19402a | 804.2 µs · 163817B · 20409a | — | 314 µs · 8977B · 2071a |
| permutation-flips | 129.8 µs · 7680B · 128a | 131.1 µs · 7680B · 128a | 17.17 µs · 7680B · 128a | 7.56× | — | 1.879 ns · 0B · 0a | 558.1 µs · 292858B · 9856a | 235.1 µs · 78808B · 451a | 531.4 µs · 122504B · 765a | 489.7 µs · 111976B · 2368a | — | 1.05 µs · 528B · 13a |
| recursive-fib-20 | 711.1 µs · 0B · 0a | 718.6 µs · 0B · 0a | 47.14 µs · 0B · 0a | 15.1× | 42.69 µs · 8B · 1a | 21.51 µs · 0B · 0a | 1.971 ms · 319346B · 28655a | 2.18 ms · 704B · 2a | 2.988 ms · 4680B · 39a | 11.36 ms · 9807916B · 109494a | — | 11.66 ms · 8302746B · 192848a |
| recursive-fib-35 | 968.8 ms · 0B · 0a | 981 ms · 0B · 0a | 63.89 ms · 0B · 0a | 15.2× | 58.19 ms · 8B · 1a | 29.15 ms · 0B · 0a | 2.658 s · 312797504B · 39088167a | 2.99 s · 971008B · 3793a | 4.068 s · 375360B · 46373a | 15.39 s · 13378018544B · 149349994a | — | — |
| sieve | 20.41 µs · 1048B · 2a | 20.37 µs · 1048B · 2a | 926.4 ns · 1048B · 2a | 22× | 1.012 µs · 8B · 1a | 350.9 ns · 0B · 0a | 139.9 µs · 122504B · 1611a | 49.95 µs · 18416B · 44a | 84.61 µs · 1872B · 25a | 77.17 µs · 5704B · 30a | — | 82.43 µs · 4368B · 795a |
| sort-stress | 507.5 µs · 5136B · 512a | 507.4 µs · 5136B · 512a | 12.79 µs · 5136B · 512a | 39.7× | — | 5.789 µs · 1024B · 2a | 1.3 ms · 270945B · 19965a | 853.9 µs · 36567B · 365a | 1.807 ms · 49104B · 3621a | 1.776 ms · 23448B · 2034a | — | 927.2 µs · 17475B · 1708a |
| spectral-norm | 538 µs · 648B · 6a | 460.9 µs · 648B · 6a | 8.403 µs · 648B · 6a | 64× | — | 7.059 µs · 576B · 3a | 2.108 ms · 571372B · 54822a | 1.07 ms · 234784B · 925a | 2.506 ms · 275338B · 33729a | 4.613 ms · 2457134B · 52718a | — | 4.2 ms · 2447997B · 74233a |
| string-build | 727.9 µs · 85408B · 4107a | 727.1 µs · 85408B · 4107a | 475.9 µs · 85408B · 4107a | 1.53× | — | 368.3 µs · 855889B · 5001a | 2.444 ms · 2211272B · 36101a | 2.339 ms · 1078110B · 13859a | 3.131 ms · 1147385B · 26883a | 2.464 ms · 1326243B · 24469a | — | 3.049 ms · 2372768B · 41432a |
| string-compare | 1.087 ms · 21136B · 1284a | 1.087 ms · 21136B · 1284a | 221.2 µs · 21136B · 1284a | 4.91× | — | 64.62 µs · 9632B · 1585a | 1.673 ms · 296281B · 20528a | 1.15 ms · 59712B · 3431a | 2.338 ms · 50744B · 3848a | 1.83 ms · 209521B · 5520a | — | 2.427 ms · 503617B · 31846a |
| struct-tree-walk | 280.4 µs · 768B · 8a | 281.1 µs · 768B · 8a | 107.5 µs · 768B · 8a | 2.61× | — | 1.898 ns · 0B · 0a | 633.5 µs · 458379B · 5114a | 1.371 ms · 818512B · 11253a | 1.119 ms · 558960B · 6149a | 3.39 ms · 2570664B · 34797a | — | 1.227 µs · 600B · 17a |
| tail-ping-pong | 29.46 µs · 0B · 0a | 29.79 µs · 0B · 0a | 488.9 ns · 0B · 0a | 60.2× | — | 890.8 ns · 0B · 0a | 106 µs · 106112B · 2001a | 34.95 µs · 21856B · 85a | 113 µs · 13984B · 1726a | 118.4 µs · 14280B · 1731a | — | 564.1 µs · 348831B · 9016a |
| tail-sum | 29.64 µs · 0B · 0a | 29.81 µs · 0B · 0a | 683.6 ns · 0B · 0a | 43.4× | — | 301.1 ns · 0B · 0a | 107.1 µs · 106112B · 2001a | 34.8 µs · 21856B · 85a | 112.6 µs · 13984B · 1726a | 119.2 µs · 14280B · 1731a | — | 85.05 µs · 4608B · 1017a |
| typed-array-sum | 7.36 µs · 0B · 0a | 7.376 µs · 0B · 0a | 259.3 ns · 0B · 0a | 28.4× | 267.9 ns · 8B · 1a | 1.877 ns · 0B · 0a | 44.7 µs · 94208B · 513a | 8.146 µs · 4000B · 15a | 27.44 µs · 2080B · 238a | 17.44 µs · 2496B · 246a | — | — |
| xorshift-i64 | 115.8 µs · 18424B · 2046a | 115.3 µs · 18424B · 2046a | 1.323 µs · 18424B · 2046a | 87.5× | — | 360.8 ns · 0B · 0a | 101.2 µs · 107608B · 2188a | — | 971.9 µs · 447251B · 12735a | 431 µs · 199938B · 7772a | — | 103.2 µs · 13545B · 1317a |

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
