# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.952 µs · 0B · 0a | 8.938 µs · 0B · 0a | 5.386 µs · 0B · 0a | 1.66× | — | 2.263 µs · 1024B · 128a | 36.81 µs · 96288B · 388a | 17.11 µs · 14376B · 256a | 68.44 µs · 78016B · 770a | 14.2 µs · 5712B · 266a | — | 30.69 µs · 2308B · 276a |
| binary-trees | 1.963 ms · 768B · 8a | 1.981 ms · 768B · 8a | 614.5 µs · 768B · 8a | 3.2× | — | 287.2 µs · 201936B · 8414a | 3.888 ms · 1468187B · 62877a | 10.52 ms · 6995677B · 92703a | 8.843 ms · 5198704B · 69052a | 27.12 ms · 19457622B · 280714a | — | 30.51 ms · 15128164B · 446106a |
| branch-tree | 1.321 µs · 0B · 0a | 20.39 ns · 0B · 0a | 83.94 ns · 0B · 0a | 15.7× | 627 ns · 16B · 1a | 160.4 ns · 0B · 0a | 45.03 µs · 95385B · 660a | 15.11 µs · 2464B · 9a | 26.81 µs · 1992B · 196a | 24.23 µs · 2168B · 203a | — | 20.61 µs · 1640B · 219a |
| closure-counter | 5.531 µs · 64B · 2a | 5.526 µs · 64B · 2a | 912.4 ns · 64B · 2a | 6.06× | — | 40.42 ns · 0B · 0a | 40.39 µs · 92272B · 261a | 13.05 µs · 151B · 3a | 19.13 µs · 1264B · 13a | 77.35 µs · 58312B · 659a | — | 95.28 µs · 35628B · 922a |
| coroutine-gen | 79.29 µs · 128B · 2a | 78.94 µs · 128B · 2a | 78.98 µs · 128B · 2a | 1× | — | 42.41 µs · 208B · 7a | — | 320.8 µs · 149936B · 649a | 591.9 µs · 330649B · 6719a | 195.3 µs · 91800B · 3646a | — | — |
| fannkuch | 844.8 µs · 34608B · 1442a | 821 µs · 34608B · 1442a | 225.3 µs · 34608B · 1442a | 3.75× | — | 35.13 µs · 17280B · 720a | 2.053 ms · 456579B · 25621a | 1.54 ms · 443135B · 1460a | 18.7 ms · 14005344B · 164946a | 3.336 ms · 1367680B · 16944a | — | 4.823 ms · 2501427B · 88439a |
| fnv1a64 | 125.3 µs · 16400B · 2049a | 125.2 µs · 16400B · 2049a | 1.153 µs · 16400B · 2049a | 109× | — | 902.3 ns · 0B · 0a | 160.8 µs · 120808B · 3838a | — | 1.485 ms · 608250B · 18475a | 394 µs · 162921B · 7429a | — | 158.4 µs · 17282B · 2074a |
| host-call | 478.1 µs · 41752B · 3080a | 489.5 µs · 41752B · 3080a | 489.7 µs · 41752B · 3080a | 0.976× | — | 3.314 µs · 0B · 0a | 177 µs · 139264B · 3073a | 131.6 µs · 22536B · 1079a | 829.2 µs · 158048B · 14585a | 107.9 µs · 14920B · 1799a | — | 594.4 µs · 340636B · 9231a |
| i64-wide-fib | 28.7 ms · 2913456B · 364178a | 21.45 ms · 1942304B · 242785a | 681.2 µs · 16B · 1a | 42.1× | — | 239 µs · 0B · 0a | 22.32 µs · 90520B · 52a | — | 153.7 ms · 64193280B · 1942305a | 335.5 ms · 253472640B · 4248840a | — | 133 ms · 98686645B · 2138708a |
| indirect-recursive-fib | 1.193 ms · 0B · 0a | 1.191 ms · 0B · 0a | 60.45 µs · 0B · 0a | 19.7× | 98.89 µs · 8B · 1a | 35.05 µs · 0B · 0a | 2.09 ms · 319345B · 28655a | 1.951 ms · 703B · 2a | 2.538 ms · 4680B · 39a | 11.31 ms · 10158198B · 109494a | — | 25.97 ms · 13061320B · 394061a |
| iterative-fib | 1.13 µs · 0B · 0a | 1.073 µs · 0B · 0a | 79.06 ns · 0B · 0a | 14.3× | 86.83 ns · 8B · 1a | 12.48 ns · 0B · 0a | 23.12 µs · 90592B · 61a | 1.156 µs · 160B · 0a | 4.258 µs · 368B · 20a | 6.107 µs · 2448B · 88a | — | 6.285 µs · 944B · 81a |
| mandelbrot | 289.8 µs · 0B · 0a | 248 µs · 0B · 0a | 11.25 µs · 0B · 0a | 25.8× | — | 6.32 µs · 0B · 0a | 961.4 µs · 292345B · 25008a | 494.9 µs · 169408B · 661a | 7.972 ms · 5558644B · 104191a | 1.509 ms · 324978B · 23643a | — | 1.557 ms · 365399B · 22513a |
| map-ops | 70.49 µs · 8816B · 3a | 70.38 µs · 8816B · 3a | 10.98 µs · 8816B · 3a | 6.42× | — | 11.08 µs · 4904B · 3a | — | 273.3 µs · 114328B · 198a | 387 µs · 66728B · 4078a | — | — | 1.172 ms · 501522B · 15813a |
| mat-mul | 373.3 µs · 6216B · 6a | 374 µs · 6216B · 6a | 8.696 µs · 6216B · 6a | 42.9× | — | 5.534 µs · 6144B · 3a | 1.373 ms · 431714B · 34871a | 671.4 µs · 143168B · 478a | 1.213 ms · 61304B · 537a | 1.468 ms · 90713B · 9350a | — | 920.5 µs · 36615B · 6036a |
| n-body | 615.7 µs · 504B · 14a | 581.2 µs · 504B · 14a | 18.92 µs · 504B · 14a | 32.5× | — | 7.36 µs · 0B · 0a | 1.527 ms · 386897B · 35892a | 1.217 ms · 258913B · 2002a | 3.362 ms · 658435B · 81545a | 2.482 ms · 382715B · 34975a | — | 1.781 ms · 445472B · 20669a |
| n-queens | 429.4 µs · 120B · 6a | 429.9 µs · 120B · 6a | 31.62 µs · 120B · 6a | 13.6× | — | 6.006 µs · 0B · 0a | 953.1 µs · 234865B · 17784a | 569.7 µs · 14423B · 63a | 1.103 ms · 7424B · 35a | 1.457 ms · 410346B · 5147a | — | 2.029 ms · 766007B · 33371a |
| naive-checksum | 252.8 µs · 0B · 0a | 141.9 µs · 0B · 0a | 8.77 µs · 0B · 0a | 28.8× | — | 2.759 µs · 0B · 0a | 617.8 µs · 235754B · 18206a | 473.7 µs · 174768B · 682a | 892.8 µs · 155640B · 19402a | 797.4 µs · 163817B · 20409a | — | 312.8 µs · 8977B · 2071a |
| permutation-flips | 128.7 µs · 7680B · 128a | 128.6 µs · 7680B · 128a | 17.38 µs · 7680B · 128a | 7.4× | — | 1.864 ns · 0B · 0a | 558.8 µs · 292858B · 9856a | 233.7 µs · 78808B · 451a | 519.8 µs · 122504B · 765a | 496.4 µs · 111976B · 2368a | — | 1.065 µs · 528B · 13a |
| recursive-fib-20 | 713.5 µs · 0B · 0a | 718.1 µs · 0B · 0a | 47.27 µs · 0B · 0a | 15.1× | 42.73 µs · 8B · 1a | 21.52 µs · 0B · 0a | 1.966 ms · 319345B · 28655a | 2.229 ms · 704B · 2a | 2.954 ms · 4680B · 39a | 11.3 ms · 9807931B · 109494a | — | 11.59 ms · 8302727B · 192848a |
| recursive-fib-35 | 971.7 ms · 0B · 0a | 979.3 ms · 0B · 0a | 63.9 ms · 0B · 0a | 15.2× | 58.21 ms · 8B · 1a | 29.02 ms · 0B · 0a | 2.623 s · 312797536B · 39088168a | 2.998 s · 971008B · 3793a | 4.025 s · 375360B · 46373a | 15.37 s · 13378016272B · 149349979a | — | — |
| sieve | 20.36 µs · 1048B · 2a | 20.45 µs · 1048B · 2a | 952 ns · 1048B · 2a | 21.4× | 1.04 µs · 8B · 1a | 372 ns · 0B · 0a | 139.4 µs · 122504B · 1611a | 50.1 µs · 18416B · 44a | 84.96 µs · 1872B · 25a | 77.29 µs · 5704B · 30a | — | 81.03 µs · 4368B · 795a |
| sort-stress | 502.3 µs · 5136B · 512a | 505.9 µs · 5136B · 512a | 12.66 µs · 5136B · 512a | 39.7× | — | 5.394 µs · 1024B · 2a | 1.325 ms · 270945B · 19965a | 847.7 µs · 36568B · 365a | 1.8 ms · 49104B · 3621a | 1.786 ms · 23448B · 2034a | — | 929.6 µs · 17473B · 1708a |
| spectral-norm | 538.1 µs · 648B · 6a | 459.4 µs · 648B · 6a | 8.399 µs · 648B · 6a | 64.1× | — | 7.085 µs · 576B · 3a | 2.1 ms · 571371B · 54822a | 1.082 ms · 234785B · 925a | 2.49 ms · 275337B · 33729a | 4.632 ms · 2457134B · 52718a | — | 4.303 ms · 2448025B · 74233a |
| string-build | 737.9 µs · 85408B · 4107a | 723.6 µs · 85408B · 4107a | 503 µs · 85408B · 4107a | 1.47× | — | 386.2 µs · 855890B · 5001a | 2.394 ms · 2211276B · 36101a | 2.319 ms · 1078113B · 13859a | 3.123 ms · 1147386B · 26883a | 2.437 ms · 1326243B · 24469a | — | 3.127 ms · 2372804B · 41432a |
| string-compare | 1.081 ms · 21136B · 1284a | 1.079 ms · 21136B · 1284a | 223.6 µs · 21136B · 1284a | 4.83× | — | 65.04 µs · 9632B · 1585a | 1.71 ms · 296281B · 20528a | 1.148 ms · 59712B · 3431a | 2.329 ms · 50744B · 3848a | 1.863 ms · 209521B · 5520a | — | 2.428 ms · 503622B · 31846a |
| struct-tree-walk | 282.7 µs · 768B · 8a | 282.4 µs · 768B · 8a | 108.2 µs · 768B · 8a | 2.61× | — | 1.864 ns · 0B · 0a | 633.9 µs · 458379B · 5114a | 1.362 ms · 818512B · 11253a | 1.061 ms · 558960B · 6149a | 3.409 ms · 2570670B · 34797a | — | 1.251 µs · 600B · 17a |
| tail-ping-pong | 29.39 µs · 0B · 0a | 29.55 µs · 0B · 0a | 480.6 ns · 0B · 0a | 61.2× | — | 891.1 ns · 0B · 0a | 108.4 µs · 106112B · 2001a | 36.09 µs · 21856B · 85a | 114 µs · 13984B · 1726a | 119.3 µs · 14280B · 1731a | — | 563 µs · 348831B · 9016a |
| tail-sum | 29.42 µs · 0B · 0a | 29.54 µs · 0B · 0a | 668.7 ns · 0B · 0a | 44× | — | 302.6 ns · 0B · 0a | 108 µs · 106112B · 2001a | 35.73 µs · 21856B · 85a | 113.5 µs · 13984B · 1726a | 119 µs · 14280B · 1731a | — | 84.75 µs · 4608B · 1017a |
| typed-array-sum | 7.351 µs · 0B · 0a | 7.373 µs · 0B · 0a | 252.4 ns · 0B · 0a | 29.1× | 268.2 ns · 8B · 1a | 1.869 ns · 0B · 0a | 45.41 µs · 94208B · 513a | 8.352 µs · 4000B · 15a | 27.77 µs · 2080B · 238a | 17.52 µs · 2496B · 246a | — | — |
| xorshift-i64 | 114.7 µs · 18424B · 2046a | 114.2 µs · 18424B · 2046a | 1.351 µs · 18424B · 2046a | 84.9× | — | 356.6 ns · 0B · 0a | 102.2 µs · 107608B · 2188a | — | 1.01 ms · 447260B · 12736a | 427.5 µs · 199938B · 7772a | — | 103.9 µs · 13545B · 1317a |

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
