# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.139 µs · 0B · 0a | 5.33 µs · 0B · 0a | 1.71× | — | 2.111 µs · 1024B · 128a | 41.01 µs · 96289B · 388a | 19.81 µs · 14376B · 256a | 78.59 µs · 78016B · 770a | 14.14 µs · 5712B · 266a | — | 31.08 µs · 2308B · 276a |
| binary-trees | 1.981 ms · 768B · 8a | 601.9 µs · 768B · 8a | 3.29× | — | 287.9 µs · 201936B · 8414a | 3.762 ms · 1468186B · 62877a | 10.52 ms · 6995683B · 92703a | 8.931 ms · 5198701B · 69052a | 28.07 ms · 19457612B · 280714a | — | 28.07 ms · 15128180B · 446106a |
| branch-tree | 1.319 µs · 0B · 0a | 150.6 ns · 0B · 0a | 8.76× | 626.5 ns · 16B · 1a | 160.7 ns · 0B · 0a | 47.14 µs · 95384B · 660a | 15.29 µs · 2464B · 9a | 26.36 µs · 1992B · 196a | 23.83 µs · 2168B · 203a | — | 20.63 µs · 1640B · 219a |
| closure-counter | 5.502 µs · 64B · 2a | 901.8 ns · 64B · 2a | 6.1× | — | 40.36 ns · 0B · 0a | 41.87 µs · 92272B · 261a | 13.12 µs · 151B · 3a | 19.07 µs · 1264B · 13a | 79.87 µs · 58312B · 659a | — | 88.52 µs · 35629B · 922a |
| coroutine-gen | 77.77 µs · 128B · 2a | 78.42 µs · 128B · 2a | 0.992× | — | 42.68 µs · 208B · 7a | — | 330.8 µs · 149936B · 649a | 602.9 µs · 330650B · 6719a | 201.4 µs · 91800B · 3646a | — | — |
| fannkuch | 821.1 µs · 34608B · 1442a | 224.2 µs · 34608B · 1442a | 3.66× | — | 35.55 µs · 17280B · 720a | 1.992 ms · 456578B · 25621a | 1.557 ms · 443136B · 1460a | 19.17 ms · 14005356B · 164946a | 3.312 ms · 1367676B · 16944a | — | 4.73 ms · 2501439B · 88439a |
| fnv1a64 | 122.3 µs · 16400B · 2049a | 1.149 µs · 16400B · 2049a | 106× | — | 902.3 ns · 0B · 0a | 163.5 µs · 120808B · 3838a | — | 1.394 ms · 608251B · 18475a | 379.1 µs · 162921B · 7429a | — | 155.2 µs · 17282B · 2074a |
| host-call | 688.9 µs · 196592B · 14073a | 684.8 µs · 196592B · 14073a | 1.01× | — | 3.312 µs · 0B · 0a | 181.5 µs · 139264B · 3073a | 130.3 µs · 22536B · 1079a | 759.6 µs · 158048B · 14585a | 106.6 µs · 14920B · 1799a | — | 552.6 µs · 340635B · 9231a |
| i64-wide-fib | 28.13 ms · 2913456B · 364178a | 679.8 µs · 16B · 1a | 41.4× | — | 239.5 µs · 0B · 0a | 24.44 µs · 90520B · 52a | — | 150.4 ms · 64193354B · 1942306a | 343.5 ms · 253472944B · 4248841a | — | 122.5 ms · 98686586B · 2138708a |
| indirect-recursive-fib | 1.184 ms · 0B · 0a | 60.49 µs · 0B · 0a | 19.6× | 99.04 µs · 8B · 1a | 35.08 µs · 0B · 0a | 2.042 ms · 319345B · 28655a | 1.95 ms · 703B · 2a | 2.545 ms · 4680B · 39a | 11.44 ms · 10158190B · 109494a | — | 24.16 ms · 13061315B · 394061a |
| iterative-fib | 1.11 µs · 0B · 0a | 81.8 ns · 0B · 0a | 13.6× | 85 ns · 8B · 1a | 12.45 ns · 0B · 0a | 29.89 µs · 90592B · 61a | 1.167 µs · 160B · 0a | 4.219 µs · 368B · 20a | 5.973 µs · 2448B · 88a | — | 6.196 µs · 944B · 81a |
| mandelbrot | 294.9 µs · 0B · 0a | 10.95 µs · 0B · 0a | 26.9× | — | 6.319 µs · 0B · 0a | 913.6 µs · 292345B · 25008a | 492.4 µs · 169408B · 661a | 8.023 ms · 5558656B · 104191a | 1.467 ms · 324978B · 23643a | — | 1.525 ms · 365402B · 22513a |
| map-ops | 74.26 µs · 9584B · 5a | 74.66 µs · 9584B · 5a | 0.995× | — | 10.87 µs · 4904B · 3a | — | 280.7 µs · 114328B · 198a | 378.1 µs · 66728B · 4078a | — | — | 1.093 ms · 501521B · 15813a |
| mat-mul | 372.2 µs · 6216B · 6a | 8.753 µs · 6216B · 6a | 42.5× | — | 5.526 µs · 6144B · 3a | 1.333 ms · 431715B · 34871a | 674.9 µs · 143169B · 478a | 1.194 ms · 61304B · 537a | 1.454 ms · 90712B · 9350a | — | 919.4 µs · 36613B · 6036a |
| n-body | 607.6 µs · 504B · 14a | 18.9 µs · 504B · 14a | 32.1× | — | 7.357 µs · 0B · 0a | 1.454 ms · 386898B · 35892a | 1.228 ms · 258913B · 2002a | 3.209 ms · 658435B · 81545a | 2.386 ms · 382714B · 34975a | — | 1.748 ms · 445468B · 20669a |
| n-queens | 428.6 µs · 120B · 6a | 31.62 µs · 120B · 6a | 13.6× | — | 6.027 µs · 0B · 0a | 928.2 µs · 234865B · 17784a | 570 µs · 14423B · 63a | 1.092 ms · 7424B · 35a | 1.387 ms · 410346B · 5147a | — | 2.017 ms · 766012B · 33371a |
| permutation-flips | 128.6 µs · 7680B · 128a | 16.71 µs · 7680B · 128a | 7.7× | — | 1.876 ns · 0B · 0a | 562.4 µs · 292857B · 9856a | 232.6 µs · 78807B · 451a | 519.8 µs · 122504B · 765a | 484.3 µs · 111976B · 2368a | — | 992.4 ns · 528B · 13a |
| recursive-fib-20 | 714.6 µs · 0B · 0a | 47.15 µs · 0B · 0a | 15.2× | 42.72 µs · 8B · 1a | 21.45 µs · 0B · 0a | 1.914 ms · 319345B · 28655a | 2.174 ms · 704B · 2a | 2.961 ms · 4680B · 39a | 11.49 ms · 9807919B · 109494a | — | 10.59 ms · 8302723B · 192848a |
| recursive-fib-35 | 968.9 ms · 0B · 0a | 64.03 ms · 0B · 0a | 15.1× | 58.25 ms · 8B · 1a | 29.17 ms · 0B · 0a | 2.546 s · 312797712B · 39088170a | 2.969 s · 971008B · 3793a | 4.035 s · 375360B · 46373a | 15.7 s · 13378013632B · 149349937a | — | — |
| sieve | 20.06 µs · 1048B · 2a | 962.9 ns · 1048B · 2a | 20.8× | 1.033 µs · 8B · 1a | 371.7 ns · 0B · 0a | 139.4 µs · 122504B · 1611a | 51.13 µs · 18416B · 44a | 83.89 µs · 1872B · 25a | 77.16 µs · 5704B · 30a | — | 80.78 µs · 4368B · 795a |
| sort-stress | 506.7 µs · 5136B · 512a | 12.68 µs · 5136B · 512a | 40× | — | 5.372 µs · 1024B · 2a | 1.265 ms · 270945B · 19965a | 848.2 µs · 36568B · 365a | 1.786 ms · 49104B · 3621a | 1.779 ms · 23448B · 2034a | — | 932.2 µs · 17473B · 1708a |
| spectral-norm | 534.6 µs · 648B · 6a | 8.335 µs · 648B · 6a | 64.1× | — | 7.051 µs · 576B · 3a | 2.007 ms · 571372B · 54822a | 1.089 ms · 234784B · 925a | 2.43 ms · 275337B · 33729a | 4.579 ms · 2457137B · 52718a | — | 4.062 ms · 2448005B · 74233a |
| string-build | 716.4 µs · 85408B · 4107a | 493.5 µs · 85408B · 4107a | 1.45× | — | 394.6 µs · 855888B · 5001a | 2.495 ms · 2211271B · 36101a | 2.339 ms · 1078111B · 13859a | 3.158 ms · 1147384B · 26883a | 2.488 ms · 1326242B · 24469a | — | 3.082 ms · 2372781B · 41432a |
| string-compare | 1.09 ms · 21136B · 1284a | 235.4 µs · 21136B · 1284a | 4.63× | — | 63.96 µs · 9632B · 1585a | 1.661 ms · 296281B · 20528a | 1.142 ms · 59712B · 3431a | 2.318 ms · 50744B · 3848a | 1.833 ms · 209521B · 5520a | — | 2.353 ms · 503615B · 31846a |
| struct-tree-walk | 285.4 µs · 768B · 8a | 107.2 µs · 768B · 8a | 2.66× | — | 1.878 ns · 0B · 0a | 654.8 µs · 458379B · 5114a | 1.38 ms · 818512B · 11253a | 1.08 ms · 558961B · 6149a | 3.559 ms · 2570664B · 34797a | — | 1.167 µs · 600B · 17a |
| tail-ping-pong | 28.94 µs · 0B · 0a | 486.7 ns · 0B · 0a | 59.5× | — | 890.9 ns · 0B · 0a | 109.6 µs · 106112B · 2001a | 36.34 µs · 21856B · 85a | 111.9 µs · 13984B · 1726a | 117.7 µs · 14280B · 1731a | — | 543.8 µs · 348832B · 9016a |
| tail-sum | 28.8 µs · 0B · 0a | 682.5 ns · 0B · 0a | 42.2× | — | 302.5 ns · 0B · 0a | 110.7 µs · 106112B · 2001a | 36.41 µs · 21856B · 85a | 111.5 µs · 13984B · 1726a | 117.5 µs · 14280B · 1731a | — | 83.92 µs · 4608B · 1017a |
| typed-array-sum | 7.264 µs · 0B · 0a | 265.4 ns · 0B · 0a | 27.4× | 266.3 ns · 8B · 1a | 1.876 ns · 0B · 0a | 47.98 µs · 94208B · 513a | 8.574 µs · 4000B · 15a | 27.39 µs · 2080B · 238a | 17.34 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.4 µs · 18424B · 2046a | 1.497 µs · 18424B · 2046a | 75.1× | — | 356.2 ns · 0B · 0a | 106.4 µs · 107608B · 2188a | — | 963 µs · 447254B · 12735a | 412 µs · 199937B · 7772a | — | 101.8 µs · 13546B · 1317a |

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
