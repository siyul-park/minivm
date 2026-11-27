# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.279 µs · 0B · 0a | 8.356 µs · 0B · 0a | 5.435 µs · 0B · 0a | 1.52× | — | 2.471 µs · 1024B · 128a | 44.75 µs · 96288B · 388a | 17.73 µs · 14376B · 256a | 73.69 µs · 78016B · 770a | 14.11 µs · 5712B · 266a | — | 30.93 µs · 2308B · 276a |
| binary-trees | 1.654 ms · 768B · 8a | 1.646 ms · 768B · 8a | 557.4 µs · 768B · 8a | 2.97× | — | 294.3 µs · 201936B · 8414a | 3.831 ms · 1468184B · 62877a | 10.67 ms · 6995677B · 92703a | 9.125 ms · 5198870B · 69052a | 27.96 ms · 19457620B · 280714a | — | 28.61 ms · 15128182B · 446106a |
| branch-tree | 984.7 ns · 0B · 0a | 19.03 ns · 0B · 0a | 87.48 ns · 0B · 0a | 11.3× | 629.6 ns · 16B · 1a | 160.9 ns · 0B · 0a | 48.36 µs · 95384B · 660a | 15.3 µs · 2464B · 9a | 26.78 µs · 1992B · 196a | 24.88 µs · 2168B · 203a | — | 20.75 µs · 1640B · 219a |
| closure-counter | 5.19 µs · 64B · 2a | 5.22 µs · 64B · 2a | 905.8 ns · 64B · 2a | 5.73× | — | 40.17 ns · 0B · 0a | 41.56 µs · 92272B · 261a | 13.08 µs · 151B · 3a | 19.47 µs · 1264B · 13a | 80.86 µs · 58312B · 659a | — | 89.42 µs · 35629B · 922a |
| coroutine-gen | 66.63 µs · 128B · 2a | 66.31 µs · 128B · 2a | 66.48 µs · 128B · 2a | 1× | — | 42.72 µs · 208B · 7a | — | 344.9 µs · 149936B · 649a | 625.4 µs · 330649B · 6719a | 200.5 µs · 91800B · 3646a | — | — |
| fannkuch | 660.6 µs · 34608B · 1442a | 639.5 µs · 34608B · 1442a | 215.2 µs · 34608B · 1442a | 3.07× | — | 36.1 µs · 17280B · 720a | 2.026 ms · 456578B · 25621a | 1.638 ms · 443139B · 1460a | 20.51 ms · 14005358B · 164946a | 3.42 ms · 1367678B · 16944a | — | 4.743 ms · 2501429B · 88439a |
| fnv1a64 | 115.6 µs · 16400B · 2049a | 115 µs · 16400B · 2049a | 1.137 µs · 16400B · 2049a | 102× | — | 903.9 ns · 0B · 0a | 166.2 µs · 120808B · 3838a | — | 1.472 ms · 608254B · 18475a | 396.3 µs · 162921B · 7429a | — | 153.9 µs · 17282B · 2074a |
| host-call | 461.9 µs · 41752B · 3080a | 468.4 µs · 41752B · 3080a | 463.6 µs · 41752B · 3080a | 0.996× | — | 3.316 µs · 0B · 0a | 185.2 µs · 139264B · 3073a | 132.7 µs · 22536B · 1079a | 793.3 µs · 158048B · 14585a | 105.2 µs · 14920B · 1799a | — | 563.6 µs · 340634B · 9231a |
| i64-wide-fib | 26.95 ms · 2913456B · 364178a | 20.26 ms · 1942304B · 242785a | 681 µs · 16B · 1a | 39.6× | — | 239.8 µs · 0B · 0a | 24.66 µs · 90520B · 52a | — | 162.7 ms · 64193384B · 1942306a | 361.4 ms · 253472784B · 4248841a | — | 125.5 ms · 98686570B · 2138708a |
| indirect-recursive-fib | 1.015 ms · 0B · 0a | 1.033 ms · 0B · 0a | 60.58 µs · 0B · 0a | 16.8× | 99.21 µs · 8B · 1a | 34.99 µs · 0B · 0a | 2.091 ms · 319345B · 28655a | 1.946 ms · 704B · 2a | 2.551 ms · 4680B · 39a | 11.87 ms · 10158177B · 109494a | — | 24.15 ms · 13061333B · 394061a |
| iterative-fib | 884.8 ns · 0B · 0a | 880.7 ns · 0B · 0a | 78.17 ns · 0B · 0a | 11.3× | 84.59 ns · 8B · 1a | 11.93 ns · 0B · 0a | 24.45 µs · 90592B · 61a | 1.159 µs · 159B · 0a | 4.285 µs · 368B · 20a | 6.178 µs · 2448B · 88a | — | 6.238 µs · 944B · 81a |
| mandelbrot | 226.9 µs · 0B · 0a | 191.9 µs · 0B · 0a | 11.28 µs · 0B · 0a | 20.1× | — | 6.331 µs · 0B · 0a | 950.4 µs · 292345B · 25008a | 486 µs · 169408B · 661a | 8.273 ms · 5558653B · 104191a | 1.473 ms · 324978B · 23643a | — | 1.529 ms · 365398B · 22513a |
| map-churn | 2.447 ms · 786608B · 49a | 2.456 ms · 786608B · 49a | 627.9 µs · 786608B · 49a | 3.9× | — | 686.7 µs · 19024B · 5a | — | 11.89 ms · 7213826B · 6564a | 15.29 ms · 2369296B · 176707a | — | — | 50.9 ms · 24395130B · 725811a |
| map-iter | 3.431 ms · 249040B · 24645a | 3.422 ms · 249040B · 24645a | 3.362 ms · 249040B · 24645a | 1.02× | — | 536 µs · 19024B · 5a | — | 12.57 ms · 6177864B · 26325a | 22.63 ms · 13957132B · 201529a | — | — | 5.997 ms · 972952B · 93724a |
| map-ops | 61.29 µs · 12976B · 4a | 61.09 µs · 12976B · 4a | 11.66 µs · 12976B · 4a | 5.26× | — | 10.96 µs · 4904B · 3a | — | 282.8 µs · 114327B · 198a | 377.8 µs · 66728B · 4078a | — | — | 1.107 ms · 501519B · 15813a |
| map-string | 6.529 ms · 174184B · 9244a | 6.531 ms · 174184B · 9244a | 2.093 ms · 563664B · 11822a | 3.12× | — | 1.115 ms · 52632B · 2112a | 8.173 ms · 1463603B · 126292a | 9.488 ms · 967128B · 9917a | 16.14 ms · 590605B · 59508a | 8.748 ms · 1037364B · 80210a | — | 9.377 ms · 1281206B · 94103a |
| mat-mul | 311.8 µs · 6216B · 6a | 312.6 µs · 6216B · 6a | 8.71 µs · 6216B · 6a | 35.8× | — | 5.56 µs · 6144B · 3a | 1.359 ms · 431715B · 34871a | 670.1 µs · 143168B · 478a | 1.201 ms · 61304B · 537a | 1.454 ms · 90714B · 9350a | — | 931.4 µs · 36615B · 6036a |
| n-body | 497.8 µs · 504B · 14a | 415.2 µs · 504B · 14a | 19.16 µs · 504B · 14a | 26× | — | 7.354 µs · 0B · 0a | 1.486 ms · 386898B · 35892a | 1.224 ms · 258913B · 2002a | 3.252 ms · 658435B · 81545a | 2.388 ms · 382714B · 34975a | — | 1.747 ms · 445462B · 20669a |
| n-queens | 277.3 µs · 120B · 6a | 276.6 µs · 120B · 6a | 31.73 µs · 120B · 6a | 8.74× | — | 5.999 µs · 0B · 0a | 938.8 µs · 234864B · 17784a | 567 µs · 14423B · 63a | 1.11 ms · 7424B · 35a | 1.408 ms · 410346B · 5147a | — | 1.995 ms · 766006B · 33371a |
| naive-checksum | 188.4 µs · 0B · 0a | 108.6 µs · 0B · 0a | 8.726 µs · 0B · 0a | 21.6× | — | 2.762 µs · 0B · 0a | 616.7 µs · 235753B · 18206a | 473.3 µs · 174768B · 682a | 861.8 µs · 155640B · 19402a | 765.1 µs · 163816B · 20409a | — | 314 µs · 8977B · 2071a |
| permutation-flips | 104.1 µs · 7680B · 128a | 104 µs · 7680B · 128a | 16.71 µs · 7680B · 128a | 6.23× | — | 1.896 ns · 0B · 0a | 570.7 µs · 292857B · 9856a | 236.6 µs · 78808B · 451a | 531.2 µs · 122504B · 765a | 491.5 µs · 111976B · 2368a | — | 1.005 µs · 528B · 13a |
| recursive-fib-20 | 648.7 µs · 0B · 0a | 640.5 µs · 0B · 0a | 47.3 µs · 0B · 0a | 13.7× | 42.8 µs · 8B · 1a | 21.48 µs · 0B · 0a | 1.93 ms · 319345B · 28655a | 2.158 ms · 704B · 2a | 2.998 ms · 4680B · 39a | 11.64 ms · 9807916B · 109494a | — | 10.73 ms · 8302730B · 192848a |
| recursive-fib-35 | 870.2 ms · 0B · 0a | 876.2 ms · 0B · 0a | 64 ms · 0B · 0a | 13.6× | 58.29 ms · 8B · 1a | 29.13 ms · 0B · 0a | 2.574 s · 312797376B · 39088167a | 2.957 s · 971008B · 3793a | 4.084 s · 375360B · 46373a | 15.63 s · 13378015952B · 149349952a | — | — |
| sieve | 15.16 µs · 1048B · 2a | 15.07 µs · 1048B · 2a | 938.4 ns · 1048B · 2a | 16.2× | 1.028 µs · 8B · 1a | 351.4 ns · 0B · 0a | 139.8 µs · 122504B · 1611a | 50.89 µs · 18416B · 44a | 85.37 µs · 1872B · 25a | 76.92 µs · 5704B · 30a | — | 82.1 µs · 4368B · 795a |
| sort-stress | 389.4 µs · 5136B · 512a | 373.7 µs · 5136B · 512a | 12.9 µs · 5136B · 512a | 30.2× | — | 5.841 µs · 1024B · 2a | 1.264 ms · 270945B · 19965a | 846.3 µs · 36567B · 365a | 1.793 ms · 49104B · 3621a | 1.77 ms · 23448B · 2034a | — | 919.5 µs · 17473B · 1708a |
| spectral-norm | 428.3 µs · 648B · 6a | 353 µs · 648B · 6a | 8.342 µs · 648B · 6a | 51.3× | — | 7.043 µs · 576B · 3a | 2.028 ms · 571371B · 54822a | 1.082 ms · 234784B · 925a | 2.483 ms · 275337B · 33729a | 4.615 ms · 2457134B · 52718a | — | 4.13 ms · 2448018B · 74233a |
| string-build | 607.2 µs · 85408B · 4107a | 591.7 µs · 85408B · 4107a | 542.4 µs · 85408B · 4107a | 1.12× | — | 357.1 µs · 855889B · 5001a | 2.412 ms · 2211272B · 36101a | 2.32 ms · 1078110B · 13859a | 3.165 ms · 1147384B · 26883a | 2.453 ms · 1326244B · 24469a | — | 3.153 ms · 2372786B · 41432a |
| string-compare | 998.9 µs · 21136B · 1284a | 995.4 µs · 21136B · 1284a | 218.4 µs · 21136B · 1284a | 4.57× | — | 62.14 µs · 9632B · 1585a | 1.656 ms · 296282B · 20528a | 1.127 ms · 59712B · 3431a | 2.336 ms · 50744B · 3848a | 1.788 ms · 209521B · 5520a | — | 2.374 ms · 503612B · 31846a |
| struct-tree-walk | 253.1 µs · 768B · 8a | 252.9 µs · 768B · 8a | 103.1 µs · 768B · 8a | 2.46× | — | 1.895 ns · 0B · 0a | 640.9 µs · 458379B · 5114a | 1.352 ms · 818514B · 11253a | 1.081 ms · 558961B · 6149a | 3.405 ms · 2570663B · 34797a | — | 1.178 µs · 600B · 17a |
| tail-ping-pong | 21.65 µs · 0B · 0a | 21.67 µs · 0B · 0a | 483.5 ns · 0B · 0a | 44.8× | — | 892.6 ns · 0B · 0a | 109.1 µs · 106112B · 2001a | 35.54 µs · 21856B · 85a | 110.4 µs · 13984B · 1726a | 114.7 µs · 14280B · 1731a | — | 545.1 µs · 348835B · 9016a |
| tail-sum | 21.67 µs · 0B · 0a | 21.79 µs · 0B · 0a | 685.5 ns · 0B · 0a | 31.6× | — | 303.4 ns · 0B · 0a | 108.9 µs · 106112B · 2001a | 35.08 µs · 21856B · 85a | 111.4 µs · 13984B · 1726a | 114.5 µs · 14280B · 1731a | — | 85.53 µs · 4608B · 1017a |
| typed-array-sum | 5.728 µs · 0B · 0a | 5.657 µs · 0B · 0a | 267.6 ns · 0B · 0a | 21.4× | 264.8 ns · 8B · 1a | 1.868 ns · 0B · 0a | 46.11 µs · 94208B · 513a | 8.403 µs · 4000B · 15a | 27.47 µs · 2080B · 238a | 17.21 µs · 2496B · 246a | — | — |
| xorshift-i64 | 104.5 µs · 18424B · 2046a | 104.6 µs · 18424B · 2046a | 1.373 µs · 18424B · 2046a | 76.1× | — | 361.5 ns · 0B · 0a | 104.7 µs · 107608B · 2188a | — | 973.2 µs · 447251B · 12735a | 431.3 µs · 199938B · 7772a | — | 101.8 µs · 13546B · 1317a |

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
