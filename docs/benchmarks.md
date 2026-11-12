# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.881 µs · 0B · 0a | 5.378 µs · 0B · 0a | 1.65× | — | 2.209 µs · 1024B · 128a | 38.92 µs · 96288B · 388a | 17.64 µs · 14376B · 256a | 73.11 µs · 78016B · 770a | 14.62 µs · 5712B · 266a | — | 30.98 µs · 2308B · 276a |
| binary-trees | 1.955 ms · 768B · 8a | 593.1 µs · 768B · 8a | 3.3× | — | 291.3 µs · 201936B · 8414a | 3.855 ms · 1468187B · 62877a | 10.6 ms · 6995683B · 92703a | 9.058 ms · 5198696B · 69052a | 28.37 ms · 19457598B · 280714a | — | 28.74 ms · 15128161B · 446106a |
| branch-tree | 1.322 µs · 0B · 0a | 157.2 ns · 0B · 0a | 8.41× | 625.3 ns · 16B · 1a | 160.5 ns · 0B · 0a | 56.63 µs · 95384B · 660a | 15.46 µs · 2464B · 9a | 26.46 µs · 1992B · 196a | 24.45 µs · 2168B · 203a | — | 20.61 µs · 1640B · 219a |
| closure-counter | 5.51 µs · 64B · 2a | 907.4 ns · 64B · 2a | 6.07× | — | 40.37 ns · 0B · 0a | 45.37 µs · 92272B · 261a | 12.95 µs · 151B · 3a | 19.02 µs · 1264B · 13a | 80.78 µs · 58312B · 659a | — | 88.4 µs · 35628B · 922a |
| coroutine-gen | 79.19 µs · 128B · 2a | 79.42 µs · 128B · 2a | 0.997× | — | 42.4 µs · 208B · 7a | — | 329.7 µs · 149936B · 649a | 602.9 µs · 330649B · 6719a | 200.5 µs · 91800B · 3646a | — | — |
| fannkuch | 826.7 µs · 34608B · 1442a | 220.1 µs · 34608B · 1442a | 3.76× | — | 35.78 µs · 17280B · 720a | 2.005 ms · 456579B · 25621a | 1.547 ms · 443135B · 1460a | 19.83 ms · 14005351B · 164946a | 3.372 ms · 1367676B · 16944a | — | 4.752 ms · 2501436B · 88439a |
| fnv1a64 | 122.1 µs · 16400B · 2049a | 1.16 µs · 16400B · 2049a | 105× | — | 902.4 ns · 0B · 0a | 163.5 µs · 120808B · 3838a | — | 1.487 ms · 608252B · 18475a | 385.8 µs · 162921B · 7429a | — | 154.3 µs · 17281B · 2074a |
| host-call | 474.9 µs · 41752B · 3080a | 474 µs · 41752B · 3080a | 1× | — | 3.312 µs · 0B · 0a | 180.8 µs · 139264B · 3073a | 129.6 µs · 22536B · 1079a | 783.3 µs · 158048B · 14585a | 106.1 µs · 14920B · 1799a | — | 548.7 µs · 340639B · 9231a |
| i64-wide-fib | 28.03 ms · 2913456B · 364178a | 680.6 µs · 16B · 1a | 41.2× | — | 238.8 µs · 0B · 0a | 24.33 µs · 90520B · 52a | — | 153.4 ms · 64193288B · 1942305a | 355 ms · 253472480B · 4248839a | — | 131.3 ms · 98686677B · 2138708a |
| indirect-recursive-fib | 1.187 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.6× | 100.1 µs · 8B · 1a | 35.18 µs · 0B · 0a | 2.076 ms · 319345B · 28655a | 1.943 ms · 704B · 2a | 2.546 ms · 4680B · 39a | 11.99 ms · 10158182B · 109494a | — | 24.01 ms · 13061307B · 394061a |
| iterative-fib | 1.115 µs · 0B · 0a | 87.02 ns · 0B · 0a | 12.8× | 84.65 ns · 8B · 1a | 12.37 ns · 0B · 0a | 25.71 µs · 90592B · 61a | 1.156 µs · 160B · 0a | 4.256 µs · 368B · 20a | 6.095 µs · 2448B · 88a | — | 6.175 µs · 944B · 81a |
| mandelbrot | 312.5 µs · 0B · 0a | 10.88 µs · 0B · 0a | 28.7× | — | 6.322 µs · 0B · 0a | 947.7 µs · 292346B · 25008a | 494.6 µs · 169408B · 661a | 8.435 ms · 5558658B · 104191a | 1.486 ms · 324979B · 23643a | — | 1.525 ms · 365403B · 22513a |
| map-ops | 70.18 µs · 8816B · 3a | 11.32 µs · 8816B · 3a | 6.2× | — | 11.04 µs · 4904B · 3a | — | 285.7 µs · 114328B · 198a | 385.2 µs · 66728B · 4078a | — | — | 1.112 ms · 501520B · 15813a |
| mat-mul | 369.7 µs · 6216B · 6a | 8.841 µs · 6216B · 6a | 41.8× | — | 5.817 µs · 6144B · 3a | 1.37 ms · 431715B · 34871a | 667.7 µs · 143168B · 478a | 1.204 ms · 61304B · 537a | 1.468 ms · 90712B · 9350a | — | 921.4 µs · 36615B · 6036a |
| n-body | 608.3 µs · 504B · 14a | 18.59 µs · 504B · 14a | 32.7× | — | 7.356 µs · 0B · 0a | 1.506 ms · 386898B · 35892a | 1.23 ms · 258914B · 2002a | 3.337 ms · 658436B · 81545a | 2.435 ms · 382714B · 34975a | — | 1.804 ms · 445469B · 20669a |
| n-queens | 431.4 µs · 120B · 6a | 31.47 µs · 120B · 6a | 13.7× | — | 6.021 µs · 0B · 0a | 948.9 µs · 234865B · 17784a | 567.7 µs · 14423B · 63a | 1.098 ms · 7424B · 35a | 1.425 ms · 410346B · 5147a | — | 2.023 ms · 766003B · 33371a |
| permutation-flips | 128.9 µs · 7680B · 128a | 17.12 µs · 7680B · 128a | 7.53× | — | 1.877 ns · 0B · 0a | 573 µs · 292857B · 9856a | 240.4 µs · 78808B · 451a | 540.6 µs · 122504B · 765a | 494.8 µs · 111976B · 2368a | — | 1.014 µs · 528B · 13a |
| recursive-fib-20 | 717.7 µs · 0B · 0a | 47.27 µs · 0B · 0a | 15.2× | 42.77 µs · 8B · 1a | 21.4 µs · 0B · 0a | 1.936 ms · 319345B · 28655a | 2.174 ms · 704B · 2a | 2.989 ms · 4680B · 39a | 11.97 ms · 9807908B · 109494a | — | 10.53 ms · 8302685B · 192847a |
| recursive-fib-35 | 978.9 ms · 0B · 0a | 63.98 ms · 0B · 0a | 15.3× | 58.21 ms · 8B · 1a | 29.08 ms · 0B · 0a | 2.607 s · 312797376B · 39088167a | 2.962 s · 971008B · 3793a | 4.078 s · 375360B · 46373a | 16.27 s · 13378016864B · 149349984a | — | — |
| sieve | 20.13 µs · 1048B · 2a | 986.8 ns · 1048B · 2a | 20.4× | 1.042 µs · 8B · 1a | 372.1 ns · 0B · 0a | 146.1 µs · 122504B · 1611a | 54.78 µs · 18416B · 44a | 83.89 µs · 1872B · 25a | 76.63 µs · 5704B · 30a | — | 81.29 µs · 4368B · 795a |
| sort-stress | 506.3 µs · 5136B · 512a | 12.63 µs · 5136B · 512a | 40.1× | — | 5.348 µs · 1024B · 2a | 1.277 ms · 270945B · 19965a | 844.4 µs · 36568B · 365a | 1.788 ms · 49104B · 3621a | 1.764 ms · 23448B · 2034a | — | 920.6 µs · 17473B · 1708a |
| spectral-norm | 534.9 µs · 648B · 6a | 8.403 µs · 648B · 6a | 63.7× | — | 7.055 µs · 576B · 3a | 2.032 ms · 571371B · 54822a | 1.08 ms · 234784B · 925a | 2.48 ms · 275337B · 33729a | 4.726 ms · 2457135B · 52718a | — | 4.101 ms · 2447981B · 74233a |
| string-build | 726.3 µs · 85408B · 4107a | 493.8 µs · 85408B · 4107a | 1.47× | — | 401.3 µs · 855889B · 5001a | 2.571 ms · 2211279B · 36101a | 2.355 ms · 1078113B · 13859a | 3.218 ms · 1147383B · 26883a | 2.518 ms · 1326245B · 24469a | — | 3.131 ms · 2372783B · 41432a |
| string-compare | 1.079 ms · 21136B · 1284a | 236.8 µs · 21136B · 1284a | 4.56× | — | 63.11 µs · 9632B · 1585a | 1.669 ms · 296281B · 20528a | 1.128 ms · 59712B · 3431a | 2.324 ms · 50744B · 3848a | 1.843 ms · 209521B · 5520a | — | 2.367 ms · 503605B · 31846a |
| struct-tree-walk | 281.6 µs · 768B · 8a | 106.9 µs · 768B · 8a | 2.63× | — | 1.878 ns · 0B · 0a | 674.3 µs · 458378B · 5114a | 1.384 ms · 818513B · 11253a | 1.122 ms · 558961B · 6149a | 3.599 ms · 2570665B · 34797a | — | 1.17 µs · 600B · 17a |
| tail-ping-pong | 29.03 µs · 0B · 0a | 494.8 ns · 0B · 0a | 58.7× | — | 890.8 ns · 0B · 0a | 111.3 µs · 106112B · 2001a | 36.65 µs · 21855B · 85a | 111.1 µs · 13984B · 1726a | 118.3 µs · 14280B · 1731a | — | 543 µs · 348836B · 9016a |
| tail-sum | 28.95 µs · 0B · 0a | 679.4 ns · 0B · 0a | 42.6× | — | 302.5 ns · 0B · 0a | 110.3 µs · 106112B · 2001a | 36.39 µs · 21856B · 85a | 111.8 µs · 13984B · 1726a | 118.1 µs · 14280B · 1731a | — | 84.25 µs · 4608B · 1017a |
| typed-array-sum | 7.295 µs · 0B · 0a | 263.6 ns · 0B · 0a | 27.7× | 266.9 ns · 8B · 1a | 1.876 ns · 0B · 0a | 48.44 µs · 94208B · 513a | 8.451 µs · 4000B · 15a | 27.21 µs · 2080B · 238a | 17.09 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.9 µs · 18424B · 2046a | 1.446 µs · 18424B · 2046a | 76.7× | — | 356 ns · 0B · 0a | 107.1 µs · 107608B · 2188a | — | 998.5 µs · 447249B · 12735a | 428.8 µs · 199937B · 7772a | — | 101.3 µs · 13545B · 1317a |

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
