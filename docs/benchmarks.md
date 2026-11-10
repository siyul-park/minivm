# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.851 µs · 0B · 0a | 5.37 µs · 0B · 0a | 1.65× | — | 2.229 µs · 1024B · 128a | 43.53 µs · 96288B · 388a | 19.69 µs · 14376B · 256a | 72.2 µs · 78016B · 770a | 14.06 µs · 5712B · 266a | — | 30.86 µs · 2308B · 276a |
| binary-trees | 1.984 ms · 768B · 8a | 593.3 µs · 768B · 8a | 3.34× | — | 288.5 µs · 201936B · 8414a | 3.852 ms · 1468183B · 62877a | 10.72 ms · 6995675B · 92703a | 9.054 ms · 5198693B · 69052a | 28.23 ms · 19457628B · 280714a | — | 28.2 ms · 15128201B · 446106a |
| branch-tree | 1.318 µs · 0B · 0a | 140.6 ns · 0B · 0a | 9.37× | 625.8 ns · 16B · 1a | 160.4 ns · 0B · 0a | 46.95 µs · 95385B · 660a | 15.23 µs · 2464B · 9a | 26.18 µs · 1992B · 196a | 23.92 µs · 2168B · 203a | — | 20.64 µs · 1640B · 219a |
| closure-counter | 5.473 µs · 64B · 2a | 885 ns · 64B · 2a | 6.18× | — | 40.38 ns · 0B · 0a | 41.72 µs · 92272B · 261a | 12.85 µs · 151B · 3a | 19.02 µs · 1264B · 13a | 80.25 µs · 58312B · 659a | — | 89.44 µs · 35629B · 922a |
| coroutine-gen | 76.92 µs · 128B · 2a | 77.86 µs · 128B · 2a | 0.988× | — | 42.8 µs · 208B · 7a | — | 334 µs · 149936B · 649a | 619.5 µs · 330649B · 6719a | 202.2 µs · 91800B · 3646a | — | — |
| fannkuch | 826.4 µs · 34608B · 1442a | 224.6 µs · 34608B · 1442a | 3.68× | — | 36.1 µs · 17280B · 720a | 2.051 ms · 456579B · 25621a | 1.547 ms · 443136B · 1460a | 19.65 ms · 14005345B · 164946a | 3.405 ms · 1367682B · 16944a | — | 4.767 ms · 2501438B · 88439a |
| fnv1a64 | 124.3 µs · 16400B · 2049a | 1.15 µs · 16400B · 2049a | 108× | — | 904.1 ns · 0B · 0a | 167.6 µs · 120808B · 3838a | — | 1.481 ms · 608249B · 18475a | 395.4 µs · 162921B · 7429a | — | 153.9 µs · 17282B · 2074a |
| i64-wide-fib | 28.18 ms · 2913456B · 364178a | 680.8 µs · 16B · 1a | 41.4× | — | 238.8 µs · 0B · 0a | 24.89 µs · 90520B · 52a | — | 156.3 ms · 64193304B · 1942305a | 354.6 ms · 253472560B · 4248841a | — | 123.3 ms · 98686618B · 2138708a |
| indirect-recursive-fib | 1.189 ms · 0B · 0a | 60.51 µs · 0B · 0a | 19.6× | 99.25 µs · 8B · 1a | 35.04 µs · 0B · 0a | 2.087 ms · 319345B · 28655a | 1.952 ms · 703B · 2a | 2.521 ms · 4680B · 39a | 11.84 ms · 10158187B · 109494a | — | 24.25 ms · 13061304B · 394061a |
| iterative-fib | 1.108 µs · 0B · 0a | 72.28 ns · 0B · 0a | 15.3× | 85.41 ns · 8B · 1a | 12.38 ns · 0B · 0a | 25.51 µs · 90592B · 61a | 1.166 µs · 160B · 0a | 4.25 µs · 368B · 20a | 6.154 µs · 2448B · 88a | — | 6.189 µs · 944B · 81a |
| mandelbrot | 290.3 µs · 0B · 0a | 10.91 µs · 0B · 0a | 26.6× | — | 6.324 µs · 0B · 0a | 945.5 µs · 292345B · 25008a | 485 µs · 169407B · 661a | 8.184 ms · 5558649B · 104191a | 1.462 ms · 324979B · 23643a | — | 1.521 ms · 365400B · 22513a |
| map-ops | 73.88 µs · 9584B · 5a | 74.76 µs · 9584B · 5a | 0.988× | — | 10.89 µs · 4904B · 3a | — | 279 µs · 114327B · 198a | 373.2 µs · 66728B · 4078a | — | — | 1.104 ms · 501521B · 15813a |
| mat-mul | 373.6 µs · 6216B · 6a | 8.939 µs · 6216B · 6a | 41.8× | — | 5.85 µs · 6144B · 3a | 1.346 ms · 431714B · 34871a | 675.8 µs · 143168B · 478a | 1.208 ms · 61304B · 537a | 1.451 ms · 90712B · 9350a | — | 919.6 µs · 36615B · 6036a |
| n-body | 607.3 µs · 504B · 14a | 18.78 µs · 504B · 14a | 32.3× | — | 7.353 µs · 0B · 0a | 1.441 ms · 386898B · 35892a | 1.198 ms · 258913B · 2002a | 3.201 ms · 658434B · 81545a | 2.389 ms · 382714B · 34975a | — | 1.75 ms · 445464B · 20669a |
| n-queens | 431.3 µs · 120B · 6a | 31.41 µs · 120B · 6a | 13.7× | — | 6.012 µs · 0B · 0a | 947.1 µs · 234865B · 17784a | 570.5 µs · 14424B · 63a | 1.092 ms · 7424B · 35a | 1.424 ms · 410345B · 5147a | — | 2.008 ms · 766008B · 33371a |
| permutation-flips | 127.6 µs · 7680B · 128a | 17.18 µs · 7680B · 128a | 7.43× | — | 1.877 ns · 0B · 0a | 565.3 µs · 292857B · 9856a | 237.3 µs · 78807B · 451a | 524.3 µs · 122504B · 765a | 486.9 µs · 111976B · 2368a | — | 992.7 ns · 528B · 13a |
| recursive-fib-20 | 710.8 µs · 0B · 0a | 47.38 µs · 0B · 0a | 15× | 42.77 µs · 8B · 1a | 21.44 µs · 0B · 0a | 1.915 ms · 319346B · 28655a | 2.175 ms · 704B · 2a | 2.951 ms · 4680B · 39a | 11.57 ms · 9807910B · 109494a | — | 10.34 ms · 8302683B · 192847a |
| recursive-fib-35 | 966.1 ms · 0B · 0a | 64.02 ms · 0B · 0a | 15.1× | 58.28 ms · 8B · 1a | 29.27 ms · 0B · 0a | 2.555 s · 312797568B · 39088169a | 2.963 s · 971008B · 3793a | 4.024 s · 375360B · 46373a | 15.72 s · 13378013296B · 149349927a | — | — |
| sieve | 20.03 µs · 1048B · 2a | 937.3 ns · 1048B · 2a | 21.4× | 1.032 µs · 8B · 1a | 371.8 ns · 0B · 0a | 138.8 µs · 122504B · 1611a | 50.75 µs · 18416B · 44a | 84.12 µs · 1872B · 25a | 77.45 µs · 5704B · 30a | — | 80.83 µs · 4368B · 795a |
| sort-stress | 501.6 µs · 5136B · 512a | 12.56 µs · 5136B · 512a | 39.9× | — | 5.389 µs · 1024B · 2a | 1.27 ms · 270945B · 19965a | 842.3 µs · 36567B · 365a | 1.793 ms · 49104B · 3621a | 1.786 ms · 23448B · 2034a | — | 926 µs · 17475B · 1708a |
| spectral-norm | 535.2 µs · 648B · 6a | 8.306 µs · 648B · 6a | 64.4× | — | 7.03 µs · 576B · 3a | 2.032 ms · 571371B · 54822a | 1.087 ms · 234783B · 925a | 2.424 ms · 275337B · 33729a | 4.719 ms · 2457134B · 52718a | — | 4.204 ms · 2448030B · 74233a |
| string-build | 716.9 µs · 85408B · 4107a | 597 µs · 85408B · 4107a | 1.2× | — | 378 µs · 855892B · 5001a | 2.452 ms · 2211273B · 36101a | 2.348 ms · 1078110B · 13859a | 3.168 ms · 1147383B · 26883a | 2.487 ms · 1326244B · 24469a | — | 3.107 ms · 2372768B · 41432a |
| string-compare | 1.086 ms · 21136B · 1284a | 235.5 µs · 21136B · 1284a | 4.61× | — | 63.05 µs · 9632B · 1585a | 1.658 ms · 296281B · 20528a | 1.123 ms · 59712B · 3431a | 2.329 ms · 50744B · 3848a | 1.819 ms · 209521B · 5520a | — | 2.373 ms · 503618B · 31846a |
| struct-tree-walk | 283.6 µs · 768B · 8a | 107.2 µs · 768B · 8a | 2.64× | — | 1.877 ns · 0B · 0a | 676.1 µs · 458378B · 5114a | 1.372 ms · 818511B · 11253a | 1.093 ms · 558961B · 6149a | 3.573 ms · 2570668B · 34797a | — | 1.171 µs · 600B · 17a |
| tail-ping-pong | 29.17 µs · 0B · 0a | 466.5 ns · 0B · 0a | 62.5× | — | 890.9 ns · 0B · 0a | 108.4 µs · 106112B · 2001a | 35.98 µs · 21856B · 85a | 110.9 µs · 13984B · 1726a | 117.2 µs · 14280B · 1731a | — | 544.2 µs · 348832B · 9016a |
| tail-sum | 28.76 µs · 0B · 0a | 666.6 ns · 0B · 0a | 43.2× | — | 302.8 ns · 0B · 0a | 108.7 µs · 106112B · 2001a | 36.34 µs · 21856B · 85a | 111 µs · 13984B · 1726a | 117 µs · 14280B · 1731a | — | 84.35 µs · 4608B · 1017a |
| typed-array-sum | 7.244 µs · 0B · 0a | 268 ns · 0B · 0a | 27× | 265.7 ns · 8B · 1a | 1.889 ns · 0B · 0a | 46.47 µs · 94208B · 513a | 8.445 µs · 4000B · 15a | 27.33 µs · 2080B · 238a | 17.43 µs · 2496B · 246a | — | — |
| xorshift-i64 | 113.1 µs · 18424B · 2046a | 1.452 µs · 18424B · 2046a | 77.9× | — | 356.2 ns · 0B · 0a | 106.7 µs · 107608B · 2188a | — | 1.001 ms · 447250B · 12735a | 425.3 µs · 199937B · 7772a | — | 102.5 µs · 13545B · 1317a |

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
