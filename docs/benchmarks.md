# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.317 µs · 0B · 0a | 5.524 µs · 0B · 0a | 1.69× | — | 2.254 µs · 1024B · 128a | 40.46 µs · 96288B · 388a | 17.55 µs · 14376B · 256a | 71.05 µs · 78016B · 770a | 14.57 µs · 5712B · 266a | — | 30.48 µs · 2308B · 276a |
| binary-trees | 1.97 ms · 768B · 8a | 602.5 µs · 768B · 8a | 3.27× | — | 317.1 µs · 201937B · 8414a | 3.982 ms · 1468185B · 62877a | 10.47 ms · 6995673B · 92703a | 8.949 ms · 5198692B · 69052a | 27.33 ms · 19457602B · 280714a | — | 29.89 ms · 15128186B · 446106a |
| branch-tree | 1.301 µs · 0B · 0a | 137.3 ns · 0B · 0a | 9.48× | 624.7 ns · 16B · 1a | 160.6 ns · 0B · 0a | 46.59 µs · 95385B · 660a | 15.12 µs · 2464B · 9a | 26.42 µs · 1992B · 196a | 24.29 µs · 2168B · 203a | — | 20.7 µs · 1640B · 219a |
| closure-counter | 5.468 µs · 64B · 2a | 909.7 ns · 64B · 2a | 6.01× | — | 39.8 ns · 0B · 0a | 40.93 µs · 92272B · 261a | 12.87 µs · 151B · 3a | 19.11 µs · 1264B · 13a | 78.21 µs · 58312B · 659a | — | 94.51 µs · 35629B · 922a |
| fannkuch | 822.5 µs · 34608B · 1442a | 272 µs · 34608B · 1442a | 3.02× | — | 34.91 µs · 17280B · 720a | 2.06 ms · 456578B · 25621a | 1.528 ms · 443135B · 1460a | 19.22 ms · 14005368B · 164946a | 3.37 ms · 1367678B · 16944a | — | 4.843 ms · 2501445B · 88439a |
| fnv1a64 | 123.2 µs · 16400B · 2049a | 1.152 µs · 16400B · 2049a | 107× | — | 902.8 ns · 0B · 0a | 161 µs · 120808B · 3838a | — | 1.476 ms · 608255B · 18475a | 398.9 µs · 162921B · 7429a | — | 153.9 µs · 17282B · 2074a |
| i64-wide-fib | 28.27 ms · 2913456B · 364178a | 679.6 µs · 16B · 1a | 41.6× | — | 238.7 µs · 0B · 0a | 23.58 µs · 90520B · 52a | — | 158.9 ms · 64193192B · 1942305a | 343.1 ms · 253473024B · 4248842a | — | 130.7 ms · 98686581B · 2138708a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.6× | 100.3 µs · 8B · 1a | 35.08 µs · 0B · 0a | 2.114 ms · 319346B · 28655a | 1.927 ms · 704B · 2a | 2.517 ms · 4680B · 39a | 11.51 ms · 10158191B · 109494a | — | 26.04 ms · 13061333B · 394061a |
| iterative-fib | 1.108 µs · 0B · 0a | 70.38 ns · 0B · 0a | 15.7× | 85.87 ns · 8B · 1a | 11.89 ns · 0B · 0a | 23.18 µs · 90592B · 61a | 1.155 µs · 160B · 0a | 4.251 µs · 368B · 20a | 6.113 µs · 2448B · 88a | — | 6.207 µs · 944B · 81a |
| mandelbrot | 289 µs · 0B · 0a | 10.91 µs · 0B · 0a | 26.5× | — | 6.325 µs · 0B · 0a | 973.4 µs · 292345B · 25008a | 500.4 µs · 169408B · 661a | 8.143 ms · 5558652B · 104191a | 1.509 ms · 324978B · 23643a | — | 1.562 ms · 365403B · 22513a |
| mat-mul | 370.9 µs · 6216B · 6a | 8.676 µs · 6216B · 6a | 42.8× | — | 5.648 µs · 6144B · 3a | 1.387 ms · 431714B · 34871a | 661.2 µs · 143168B · 478a | 1.207 ms · 61304B · 537a | 1.467 ms · 90712B · 9350a | — | 923.1 µs · 36614B · 6036a |
| n-body | 608.4 µs · 504B · 14a | 18.51 µs · 504B · 14a | 32.9× | — | 7.353 µs · 0B · 0a | 1.535 ms · 386898B · 35892a | 1.211 ms · 258913B · 2002a | 3.377 ms · 658435B · 81545a | 2.459 ms · 382714B · 34975a | — | 1.751 ms · 445467B · 20669a |
| n-queens | 427.7 µs · 120B · 6a | 31.51 µs · 120B · 6a | 13.6× | — | 6.017 µs · 0B · 0a | 963.8 µs · 234865B · 17784a | 567.3 µs · 14423B · 63a | 1.096 ms · 7424B · 35a | 1.417 ms · 410345B · 5147a | — | 2.021 ms · 766007B · 33371a |
| permutation-flips | 127.2 µs · 7680B · 128a | 18.23 µs · 7680B · 128a | 6.98× | — | 1.874 ns · 0B · 0a | 566.9 µs · 292857B · 9856a | 235.2 µs · 78808B · 451a | 529.3 µs · 122504B · 765a | 497.2 µs · 111976B · 2368a | — | 1.077 µs · 528B · 13a |
| recursive-fib-20 | 710.6 µs · 0B · 0a | 47.26 µs · 0B · 0a | 15× | 42.71 µs · 8B · 1a | 21.56 µs · 0B · 0a | 2.032 ms · 319346B · 28655a | 2.172 ms · 704B · 2a | 2.977 ms · 4680B · 39a | 11.52 ms · 9807910B · 109494a | — | 11.4 ms · 8302711B · 192848a |
| recursive-fib-35 | 980 ms · 0B · 0a | 63.92 ms · 0B · 0a | 15.3× | 58.17 ms · 8B · 1a | 29.11 ms · 0B · 0a | 2.707 s · 312797424B · 39088167a | 3.003 s · 971008B · 3793a | 4.049 s · 375360B · 46373a | 15.37 s · 13378015664B · 149349953a | — | — |
| sieve | 19.9 µs · 1048B · 2a | 916.5 ns · 1048B · 2a | 21.7× | 1.029 µs · 8B · 1a | 350.9 ns · 0B · 0a | 134.6 µs · 122504B · 1611a | 49.61 µs · 18416B · 44a | 83.74 µs · 1872B · 25a | 76.18 µs · 5704B · 30a | — | 81.49 µs · 4368B · 795a |
| sort-stress | 497.7 µs · 5136B · 512a | 12.59 µs · 5136B · 512a | 39.5× | — | 5.796 µs · 1024B · 2a | 1.312 ms · 270945B · 19965a | 848.9 µs · 36568B · 365a | 1.802 ms · 49104B · 3621a | 1.787 ms · 23448B · 2034a | — | 922.6 µs · 17473B · 1708a |
| spectral-norm | 533.5 µs · 648B · 6a | 8.462 µs · 648B · 6a | 63× | — | 7.056 µs · 576B · 3a | 2.117 ms · 571371B · 54822a | 1.072 ms · 234784B · 925a | 2.503 ms · 275337B · 33729a | 4.634 ms · 2457136B · 52718a | — | 4.229 ms · 2448008B · 74233a |
| string-build | 742.8 µs · 85408B · 4107a | 537.7 µs · 85408B · 4107a | 1.38× | — | 360 µs · 855891B · 5001a | 2.353 ms · 2211272B · 36101a | 2.266 ms · 1078110B · 13859a | 3.092 ms · 1147384B · 26883a | 2.427 ms · 1326245B · 24469a | — | 3.13 ms · 2372800B · 41432a |
| string-compare | 1.082 ms · 21136B · 1284a | 249.2 µs · 21136B · 1284a | 4.34× | — | 64.2 µs · 9632B · 1585a | 1.691 ms · 296281B · 20528a | 1.135 ms · 59712B · 3431a | 2.325 ms · 50744B · 3848a | 1.843 ms · 209521B · 5520a | — | 2.433 ms · 503612B · 31846a |
| struct-tree-walk | 284.5 µs · 768B · 8a | 108.3 µs · 768B · 8a | 2.63× | — | 1.876 ns · 0B · 0a | 642.8 µs · 458378B · 5114a | 1.365 ms · 818512B · 11253a | 1.083 ms · 558961B · 6149a | 3.431 ms · 2570667B · 34797a | — | 1.251 µs · 600B · 17a |
| tail-ping-pong | 29.23 µs · 0B · 0a | 474.4 ns · 0B · 0a | 61.6× | — | 890.7 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 35.37 µs · 21855B · 85a | 114 µs · 13984B · 1726a | 118.8 µs · 14280B · 1731a | — | 565.7 µs · 348836B · 9016a |
| tail-sum | 29.23 µs · 0B · 0a | 669.5 ns · 0B · 0a | 43.7× | — | 302 ns · 0B · 0a | 105.9 µs · 106112B · 2001a | 35.13 µs · 21856B · 85a | 114.1 µs · 13984B · 1726a | 117.5 µs · 14280B · 1731a | — | 85.63 µs · 4608B · 1017a |
| typed-array-sum | 7.28 µs · 0B · 0a | 263.8 ns · 0B · 0a | 27.6× | 266.6 ns · 8B · 1a | 1.888 ns · 0B · 0a | 44.58 µs · 94208B · 513a | 8.346 µs · 4000B · 15a | 27.49 µs · 2080B · 238a | 17.25 µs · 2496B · 246a | — | — |
| xorshift-i64 | 116.6 µs · 18424B · 2046a | 1.456 µs · 18424B · 2046a | 80.1× | — | 359.9 ns · 0B · 0a | 105.2 µs · 107608B · 2188a | — | 1.009 ms · 447248B · 12735a | 431.8 µs · 199937B · 7772a | — | 102.8 µs · 13545B · 1317a |

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
