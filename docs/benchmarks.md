# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.268 µs · 0B · 0a | 8.259 µs · 0B · 0a | 5.399 µs · 0B · 0a | 1.53× | — | 2.211 µs · 1024B · 128a | 37.04 µs · 96288B · 388a | 17.47 µs · 14376B · 256a | 72.54 µs · 78016B · 770a | 14.25 µs · 5712B · 266a | — | 30.75 µs · 2308B · 276a |
| binary-trees | 1.732 ms · 768B · 8a | 1.648 ms · 768B · 8a | 559.1 µs · 768B · 8a | 3.1× | — | 291 µs · 201936B · 8414a | 3.894 ms · 1468185B · 62877a | 10.53 ms · 6995670B · 92703a | 8.822 ms · 5198700B · 69052a | 27.63 ms · 19457598B · 280714a | — | 28.1 ms · 15128161B · 446106a |
| branch-tree | 933 ns · 0B · 0a | 15.33 ns · 0B · 0a | 87.77 ns · 0B · 0a | 10.6× | 628 ns · 16B · 1a | 160.6 ns · 0B · 0a | 58.68 µs · 95384B · 660a | 15.7 µs · 2464B · 9a | 26.62 µs · 1992B · 196a | 23.95 µs · 2168B · 203a | — | 20.64 µs · 1640B · 219a |
| closure-counter | 5.174 µs · 64B · 2a | 5.148 µs · 64B · 2a | 917.9 ns · 64B · 2a | 5.64× | — | 40.1 ns · 0B · 0a | 41.91 µs · 92272B · 261a | 13.07 µs · 151B · 3a | 19.06 µs · 1264B · 13a | 79.81 µs · 58312B · 659a | — | 89.15 µs · 35628B · 922a |
| coroutine-gen | 66.41 µs · 128B · 2a | 66.67 µs · 128B · 2a | 66.56 µs · 128B · 2a | 0.998× | — | 42.36 µs · 208B · 7a | — | 332.5 µs · 149936B · 649a | 597.7 µs · 330659B · 6719a | 198.2 µs · 91800B · 3646a | — | — |
| fannkuch | 659.4 µs · 34608B · 1442a | 634.9 µs · 34608B · 1442a | 212.2 µs · 34608B · 1442a | 3.11× | — | 35.08 µs · 17280B · 720a | 1.959 ms · 456579B · 25621a | 1.551 ms · 443136B · 1460a | 19.36 ms · 14005361B · 164946a | 3.343 ms · 1367677B · 16944a | — | 4.726 ms · 2501412B · 88439a |
| fnv1a64 | 113.3 µs · 16400B · 2049a | 113.1 µs · 16400B · 2049a | 1.151 µs · 16400B · 2049a | 98.4× | — | 902.4 ns · 0B · 0a | 162.7 µs · 120808B · 3838a | — | 1.456 ms · 608259B · 18475a | 389.7 µs · 162921B · 7429a | — | 155.1 µs · 17282B · 2074a |
| host-call | 469.4 µs · 41752B · 3080a | 485.4 µs · 41752B · 3080a | 482.5 µs · 41752B · 3080a | 0.973× | — | 3.311 µs · 0B · 0a | 178.7 µs · 139264B · 3073a | 127.9 µs · 22536B · 1079a | 785.6 µs · 158049B · 14585a | 107.2 µs · 14920B · 1799a | — | 554.7 µs · 340637B · 9231a |
| i64-wide-fib | 26.95 ms · 2913456B · 364178a | 20.23 ms · 1942304B · 242785a | 686.4 µs · 16B · 1a | 39.3× | — | 239.4 µs · 0B · 0a | 22.87 µs · 90520B · 52a | — | 155.1 ms · 64193328B · 1942306a | 351.2 ms · 253472992B · 4248843a | — | 124.6 ms · 98686618B · 2138708a |
| indirect-recursive-fib | 1.015 ms · 0B · 0a | 1.016 ms · 0B · 0a | 60.5 µs · 0B · 0a | 16.8× | 100.1 µs · 8B · 1a | 34.85 µs · 0B · 0a | 2.066 ms · 319345B · 28655a | 1.946 ms · 704B · 2a | 2.531 ms · 4680B · 39a | 11.75 ms · 10158192B · 109494a | — | 24.14 ms · 13061308B · 394061a |
| iterative-fib | 887.2 ns · 0B · 0a | 878.1 ns · 0B · 0a | 82.01 ns · 0B · 0a | 10.8× | 86.15 ns · 8B · 1a | 11.9 ns · 0B · 0a | 23.64 µs · 90592B · 61a | 1.159 µs · 160B · 0a | 4.234 µs · 368B · 20a | 6.107 µs · 2448B · 88a | — | 6.177 µs · 944B · 81a |
| mandelbrot | 228.8 µs · 0B · 0a | 191.5 µs · 0B · 0a | 11.26 µs · 0B · 0a | 20.3× | — | 6.324 µs · 0B · 0a | 966.4 µs · 292345B · 25008a | 490.2 µs · 169408B · 661a | 8.222 ms · 5558658B · 104191a | 1.484 ms · 324978B · 23643a | — | 1.519 ms · 365403B · 22513a |
| map-churn | 2.434 ms · 786608B · 49a | 2.426 ms · 786608B · 49a | 629.9 µs · 786608B · 49a | 3.86× | — | 685.6 µs · 19024B · 5a | — | 11.81 ms · 7213803B · 6563a | 15.5 ms · 2369293B · 176707a | — | — | 52.05 ms · 24395146B · 725811a |
| map-iter | 3.395 ms · 249040B · 24645a | 3.393 ms · 249040B · 24645a | 3.341 ms · 249040B · 24645a | 1.02× | — | 535 µs · 19024B · 5a | — | 12.63 ms · 6177872B · 26325a | 22.9 ms · 13957140B · 201529a | — | — | 6.039 ms · 972948B · 93724a |
| map-ops | 62.92 µs · 12976B · 4a | 62.53 µs · 12976B · 4a | 11.88 µs · 12976B · 4a | 5.3× | — | 11.14 µs · 4904B · 3a | — | 282.5 µs · 114328B · 198a | 380.9 µs · 66728B · 4078a | — | — | 1.113 ms · 501522B · 15813a |
| map-string | 6.539 ms · 174184B · 9244a | 6.541 ms · 174184B · 9244a | 2.098 ms · 634816B · 13303a | 3.12× | — | 1.118 ms · 52632B · 2112a | 8.331 ms · 1463609B · 126292a | 9.487 ms · 967163B · 9917a | 16.04 ms · 590600B · 59508a | 8.866 ms · 1037353B · 80210a | — | 9.367 ms · 1281251B · 94104a |
| mat-mul | 314.2 µs · 6216B · 6a | 313.7 µs · 6216B · 6a | 8.756 µs · 6216B · 6a | 35.9× | — | 5.926 µs · 6144B · 3a | 1.379 ms · 431715B · 34871a | 677.9 µs · 143168B · 478a | 1.214 ms · 61305B · 537a | 1.456 ms · 90712B · 9350a | — | 922.4 µs · 36613B · 6036a |
| n-body | 496.6 µs · 504B · 14a | 411.7 µs · 504B · 14a | 18.9 µs · 504B · 14a | 26.3× | — | 7.347 µs · 0B · 0a | 1.505 ms · 386897B · 35892a | 1.233 ms · 258913B · 2002a | 3.317 ms · 658436B · 81545a | 2.427 ms · 382714B · 34975a | — | 1.757 ms · 445460B · 20669a |
| n-queens | 278.5 µs · 120B · 6a | 279.2 µs · 120B · 6a | 31.79 µs · 120B · 6a | 8.76× | — | 6.006 µs · 0B · 0a | 948.6 µs · 234865B · 17784a | 570.7 µs · 14424B · 63a | 1.095 ms · 7424B · 35a | 1.448 ms · 410346B · 5147a | — | 2.007 ms · 766008B · 33371a |
| naive-checksum | 189.6 µs · 0B · 0a | 107.9 µs · 0B · 0a | 8.71 µs · 0B · 0a | 21.8× | — | 2.759 µs · 0B · 0a | 616.4 µs · 235753B · 18206a | 487.2 µs · 174768B · 682a | 880.4 µs · 155641B · 19402a | 790.9 µs · 163816B · 20409a | — | 312.7 µs · 8977B · 2071a |
| permutation-flips | 104.3 µs · 7680B · 128a | 104.2 µs · 7680B · 128a | 17.23 µs · 7680B · 128a | 6.05× | — | 1.898 ns · 0B · 0a | 570.7 µs · 292857B · 9856a | 238.2 µs · 78808B · 451a | 530.9 µs · 122504B · 765a | 492.9 µs · 111976B · 2368a | — | 1.002 µs · 528B · 13a |
| recursive-fib-20 | 641.3 µs · 0B · 0a | 644 µs · 0B · 0a | 47.11 µs · 0B · 0a | 13.6× | 42.76 µs · 8B · 1a | 21.45 µs · 0B · 0a | 1.948 ms · 319345B · 28655a | 2.18 ms · 704B · 2a | 2.958 ms · 4680B · 39a | 11.55 ms · 9807923B · 109494a | — | 10.71 ms · 8302673B · 192847a |
| recursive-fib-35 | 874.6 ms · 0B · 0a | 878.5 ms · 0B · 0a | 63.97 ms · 0B · 0a | 13.7× | 58.23 ms · 8B · 1a | 29.08 ms · 0B · 0a | 2.561 s · 312797536B · 39088170a | 2.966 s · 971008B · 3793a | 4.019 s · 375360B · 46373a | 15.67 s · 13378016384B · 149349943a | — | — |
| sieve | 15.18 µs · 1048B · 2a | 15.18 µs · 1048B · 2a | 944.3 ns · 1048B · 2a | 16.1× | 1.035 µs · 8B · 1a | 351.1 ns · 0B · 0a | 139.5 µs · 122504B · 1611a | 50.88 µs · 18416B · 44a | 84.27 µs · 1872B · 25a | 76.78 µs · 5704B · 30a | — | 81.3 µs · 4368B · 795a |
| sort-stress | 388.3 µs · 5136B · 512a | 370.8 µs · 5136B · 512a | 12.74 µs · 5136B · 512a | 30.5× | — | 5.822 µs · 1024B · 2a | 1.244 ms · 270945B · 19965a | 844.4 µs · 36568B · 365a | 1.788 ms · 49104B · 3621a | 1.788 ms · 23448B · 2034a | — | 927.2 µs · 17473B · 1708a |
| spectral-norm | 428.4 µs · 648B · 6a | 342.9 µs · 648B · 6a | 8.344 µs · 648B · 6a | 51.3× | — | 7.036 µs · 576B · 3a | 2.006 ms · 571371B · 54822a | 1.085 ms · 234784B · 925a | 2.428 ms · 275337B · 33729a | 4.624 ms · 2457136B · 52718a | — | 4.089 ms · 2448015B · 74233a |
| string-build | 609.2 µs · 85408B · 4107a | 589.4 µs · 85408B · 4107a | 470.6 µs · 85408B · 4107a | 1.29× | — | 364.2 µs · 855890B · 5001a | 2.451 ms · 2211275B · 36101a | 2.293 ms · 1078112B · 13859a | 3.124 ms · 1147383B · 26883a | 2.486 ms · 1326242B · 24469a | — | 3.103 ms · 2372773B · 41432a |
| string-compare | 994.7 µs · 21136B · 1284a | 995.3 µs · 21136B · 1284a | 217.1 µs · 21136B · 1284a | 4.58× | — | 62.32 µs · 9632B · 1585a | 1.67 ms · 296281B · 20528a | 1.128 ms · 59712B · 3431a | 2.33 ms · 50744B · 3848a | 1.816 ms · 209521B · 5520a | — | 2.374 ms · 503615B · 31846a |
| struct-tree-walk | 251.9 µs · 768B · 8a | 252.1 µs · 768B · 8a | 102.5 µs · 768B · 8a | 2.46× | — | 1.899 ns · 0B · 0a | 644.2 µs · 458378B · 5114a | 1.366 ms · 818512B · 11253a | 1.082 ms · 558960B · 6149a | 3.48 ms · 2570666B · 34797a | — | 1.215 µs · 600B · 17a |
| tail-ping-pong | 21.69 µs · 0B · 0a | 21.68 µs · 0B · 0a | 473.5 ns · 0B · 0a | 45.8× | — | 890.9 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 35.9 µs · 21856B · 85a | 113.1 µs · 13984B · 1726a | 116.3 µs · 14280B · 1731a | — | 563.2 µs · 348834B · 9016a |
| tail-sum | 21.56 µs · 0B · 0a | 21.76 µs · 0B · 0a | 672.3 ns · 0B · 0a | 32.1× | — | 301.4 ns · 0B · 0a | 108.3 µs · 106112B · 2001a | 35.74 µs · 21856B · 85a | 112.8 µs · 13984B · 1726a | 116.7 µs · 14280B · 1731a | — | 83.87 µs · 4608B · 1017a |
| typed-array-sum | 5.682 µs · 0B · 0a | 5.698 µs · 0B · 0a | 264.3 ns · 0B · 0a | 21.5× | 265.1 ns · 8B · 1a | 1.875 ns · 0B · 0a | 43.55 µs · 94208B · 513a | 8.295 µs · 3999B · 15a | 27.62 µs · 2080B · 238a | 17.31 µs · 2496B · 246a | — | — |
| xorshift-i64 | 105.4 µs · 18424B · 2046a | 106 µs · 18424B · 2046a | 1.323 µs · 18424B · 2046a | 79.7× | — | 360.2 ns · 0B · 0a | 103.3 µs · 107608B · 2188a | — | 984 µs · 447251B · 12735a | 434.8 µs · 199937B · 7772a | — | 102 µs · 13546B · 1317a |

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
