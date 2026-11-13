# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.993 µs · 0B · 0a | 5.39 µs · 0B · 0a | 1.67× | — | 2.314 µs · 1024B · 128a | 38.42 µs · 96289B · 388a | 19.94 µs · 14376B · 256a | 79.02 µs · 78016B · 770a | 14.54 µs · 5712B · 266a | — | 30.39 µs · 2308B · 276a |
| binary-trees | 1.957 ms · 768B · 8a | 592.5 µs · 768B · 8a | 3.3× | — | 289.9 µs · 201936B · 8414a | 3.869 ms · 1468186B · 62877a | 10.49 ms · 6995675B · 92703a | 8.97 ms · 5198694B · 69052a | 27.49 ms · 19457607B · 280714a | — | 30.27 ms · 15128179B · 446106a |
| branch-tree | 1.312 µs · 0B · 0a | 151.1 ns · 0B · 0a | 8.68× | 627.3 ns · 16B · 1a | 160.5 ns · 0B · 0a | 48.4 µs · 95384B · 660a | 15.41 µs · 2463B · 9a | 26.55 µs · 1992B · 196a | 23.98 µs · 2168B · 203a | — | 20.8 µs · 1640B · 219a |
| closure-counter | 5.5 µs · 64B · 2a | 887.8 ns · 64B · 2a | 6.2× | — | 40.42 ns · 0B · 0a | 42.21 µs · 92272B · 261a | 12.9 µs · 151B · 3a | 19.31 µs · 1264B · 13a | 85.38 µs · 58312B · 659a | — | 93.47 µs · 35629B · 922a |
| coroutine-gen | 79.36 µs · 128B · 2a | 79.16 µs · 128B · 2a | 1× | — | 42.75 µs · 208B · 7a | — | 324.6 µs · 149936B · 649a | 594.8 µs · 330649B · 6719a | 197.7 µs · 91800B · 3646a | — | — |
| fannkuch | 823.6 µs · 34608B · 1442a | 229 µs · 34608B · 1442a | 3.6× | — | 35.21 µs · 17280B · 720a | 2.052 ms · 456578B · 25621a | 1.521 ms · 443137B · 1460a | 19.09 ms · 14005372B · 164946a | 3.356 ms · 1367678B · 16944a | — | 4.863 ms · 2501440B · 88439a |
| fnv1a64 | 127.3 µs · 16400B · 2049a | 1.159 µs · 16400B · 2049a | 110× | — | 902.8 ns · 0B · 0a | 161.6 µs · 120808B · 3838a | — | 1.457 ms · 608254B · 18475a | 400.9 µs · 162921B · 7429a | — | 156.1 µs · 17282B · 2074a |
| host-call | 471 µs · 41752B · 3080a | 481.8 µs · 41752B · 3080a | 0.978× | — | 3.312 µs · 0B · 0a | 179.1 µs · 139264B · 3073a | 133.1 µs · 22536B · 1079a | 823.3 µs · 158048B · 14585a | 108.3 µs · 14920B · 1799a | — | 596 µs · 340637B · 9231a |
| i64-wide-fib | 29.15 ms · 2913440B · 364178a | 679.8 µs · 16B · 1a | 42.9× | — | 239 µs · 0B · 0a | 22.19 µs · 90520B · 52a | — | 157.7 ms · 64193280B · 1942305a | 340.5 ms · 253473280B · 4248844a | — | 130.7 ms · 98686565B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.6× | 99.12 µs · 8B · 1a | 34.77 µs · 0B · 0a | 2.09 ms · 319345B · 28655a | 1.943 ms · 704B · 2a | 2.553 ms · 4680B · 39a | 11.22 ms · 10158188B · 109494a | — | 26.48 ms · 13061312B · 394061a |
| iterative-fib | 1.129 µs · 0B · 0a | 82.64 ns · 0B · 0a | 13.7× | 85.87 ns · 8B · 1a | 12.46 ns · 0B · 0a | 24.35 µs · 90592B · 61a | 1.154 µs · 160B · 0a | 4.3 µs · 368B · 20a | 6.084 µs · 2448B · 88a | — | 6.179 µs · 944B · 81a |
| mandelbrot | 293 µs · 0B · 0a | 11.26 µs · 0B · 0a | 26× | — | 6.323 µs · 0B · 0a | 961.3 µs · 292345B · 25008a | 481.4 µs · 169408B · 661a | 7.95 ms · 5558648B · 104191a | 1.49 ms · 324979B · 23643a | — | 1.568 ms · 365404B · 22513a |
| map-ops | 69.24 µs · 8816B · 3a | 11.13 µs · 8816B · 3a | 6.22× | — | 10.77 µs · 4904B · 3a | — | 277.6 µs · 114328B · 198a | 389.7 µs · 66728B · 4078a | — | — | 1.183 ms · 501519B · 15813a |
| mat-mul | 370.5 µs · 6216B · 6a | 8.495 µs · 6216B · 6a | 43.6× | — | 5.367 µs · 6144B · 3a | 1.369 ms · 431714B · 34871a | 668.9 µs · 143168B · 478a | 1.202 ms · 61304B · 537a | 1.444 ms · 90712B · 9350a | — | 927.8 µs · 36616B · 6036a |
| n-body | 611.6 µs · 504B · 14a | 19.16 µs · 504B · 14a | 31.9× | — | 7.361 µs · 0B · 0a | 1.513 ms · 386898B · 35892a | 1.221 ms · 258914B · 2002a | 3.388 ms · 658437B · 81545a | 2.457 ms · 382714B · 34975a | — | 1.792 ms · 445470B · 20669a |
| n-queens | 429 µs · 120B · 6a | 31.6 µs · 120B · 6a | 13.6× | — | 6.005 µs · 0B · 0a | 950.4 µs · 234866B · 17784a | 570.4 µs · 14423B · 63a | 1.094 ms · 7424B · 35a | 1.438 ms · 410346B · 5147a | — | 2.019 ms · 766011B · 33371a |
| permutation-flips | 127.3 µs · 7680B · 128a | 17.44 µs · 7680B · 128a | 7.3× | — | 1.878 ns · 0B · 0a | 553.8 µs · 292857B · 9856a | 234.4 µs · 78807B · 451a | 528.2 µs · 122504B · 765a | 488.4 µs · 111976B · 2368a | — | 1.062 µs · 528B · 13a |
| recursive-fib-20 | 712.8 µs · 0B · 0a | 47.18 µs · 0B · 0a | 15.1× | 42.72 µs · 8B · 1a | 21.49 µs · 0B · 0a | 1.939 ms · 319346B · 28655a | 2.173 ms · 704B · 2a | 2.957 ms · 4680B · 39a | 11.31 ms · 9807917B · 109494a | — | 11.28 ms · 8302718B · 192848a |
| recursive-fib-35 | 974.2 ms · 0B · 0a | 64.07 ms · 0B · 0a | 15.2× | 58.22 ms · 8B · 1a | 29.07 ms · 0B · 0a | 2.635 s · 312797344B · 39088168a | 3.156 s · 971008B · 3793a | 4.032 s · 375360B · 46373a | 15.43 s · 13378019072B · 149349981a | — | — |
| sieve | 20.16 µs · 1048B · 2a | 946.9 ns · 1048B · 2a | 21.3× | 1.044 µs · 8B · 1a | 371.9 ns · 0B · 0a | 136.5 µs · 122504B · 1611a | 50.98 µs · 18416B · 44a | 84.09 µs · 1872B · 25a | 76.52 µs · 5704B · 30a | — | 80.65 µs · 4368B · 795a |
| sort-stress | 504.3 µs · 5136B · 512a | 12.65 µs · 5136B · 512a | 39.9× | — | 5.297 µs · 1024B · 2a | 1.304 ms · 270945B · 19965a | 843.3 µs · 36567B · 365a | 1.815 ms · 49104B · 3621a | 1.773 ms · 23448B · 2034a | — | 925.2 µs · 17474B · 1708a |
| spectral-norm | 533.8 µs · 648B · 6a | 8.906 µs · 648B · 6a | 59.9× | — | 7.084 µs · 576B · 3a | 2.088 ms · 571373B · 54822a | 1.096 ms · 234784B · 925a | 2.478 ms · 275337B · 33729a | 4.692 ms · 2457138B · 52718a | — | 4.217 ms · 2448008B · 74233a |
| string-build | 734 µs · 85408B · 4107a | 620.6 µs · 85408B · 4107a | 1.18× | — | 357.8 µs · 855890B · 5001a | 2.381 ms · 2211273B · 36101a | 2.319 ms · 1078112B · 13859a | 3.157 ms · 1147385B · 26883a | 2.434 ms · 1326243B · 24469a | — | 3.145 ms · 2372798B · 41432a |
| string-compare | 1.086 ms · 21136B · 1284a | 239.4 µs · 21136B · 1284a | 4.54× | — | 64.55 µs · 9632B · 1585a | 1.708 ms · 296281B · 20528a | 1.137 ms · 59712B · 3431a | 2.318 ms · 50744B · 3848a | 1.831 ms · 209521B · 5520a | — | 2.412 ms · 503611B · 31846a |
| struct-tree-walk | 280.3 µs · 768B · 8a | 107.1 µs · 768B · 8a | 2.62× | — | 1.879 ns · 0B · 0a | 630.7 µs · 458378B · 5114a | 1.367 ms · 818512B · 11253a | 1.084 ms · 558961B · 6149a | 3.507 ms · 2570668B · 34797a | — | 1.275 µs · 600B · 17a |
| tail-ping-pong | 28.91 µs · 0B · 0a | 489.3 ns · 0B · 0a | 59.1× | — | 891 ns · 0B · 0a | 110 µs · 106112B · 2001a | 35.48 µs · 21856B · 85a | 114.4 µs · 13984B · 1726a | 117.7 µs · 14280B · 1731a | — | 570.5 µs · 348835B · 9016a |
| tail-sum | 28.66 µs · 0B · 0a | 681.4 ns · 0B · 0a | 42.1× | — | 302.5 ns · 0B · 0a | 107.8 µs · 106112B · 2001a | 35.6 µs · 21856B · 85a | 114.2 µs · 13984B · 1726a | 118.2 µs · 14280B · 1731a | — | 84.18 µs · 4608B · 1017a |
| typed-array-sum | 7.266 µs · 0B · 0a | 268.8 ns · 0B · 0a | 27× | 265.4 ns · 8B · 1a | 1.876 ns · 0B · 0a | 44.78 µs · 94208B · 513a | 8.339 µs · 4000B · 15a | 27.47 µs · 2080B · 238a | 17.38 µs · 2496B · 246a | — | — |
| xorshift-i64 | 114.2 µs · 18424B · 2046a | 1.37 µs · 18424B · 2046a | 83.3× | — | 356.3 ns · 0B · 0a | 102.9 µs · 107608B · 2188a | — | 987.9 µs · 447255B · 12736a | 431.2 µs · 199937B · 7772a | — | 101.6 µs · 13546B · 1317a |

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
