# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.274 µs · 0B · 0a | 5.368 µs · 0B · 0a | 1.73× | — | 2.258 µs · 1024B · 128a | 38.42 µs · 96288B · 388a | 17.25 µs · 14376B · 256a | 69.78 µs · 78017B · 770a | 14.4 µs · 5712B · 266a | — | 30.7 µs · 2308B · 276a |
| binary-trees | 1.973 ms · 768B · 8a | 592.7 µs · 768B · 8a | 3.33× | — | 295.8 µs · 201936B · 8414a | 4.003 ms · 1468185B · 62877a | 10.6 ms · 6995668B · 92703a | 8.887 ms · 5198696B · 69052a | 27.83 ms · 19457633B · 280714a | — | 29.35 ms · 15128189B · 446106a |
| branch-tree | 1.351 µs · 0B · 0a | 154 ns · 0B · 0a | 8.77× | 626.8 ns · 16B · 1a | 160.7 ns · 0B · 0a | 46.08 µs · 95384B · 660a | 15.2 µs · 2464B · 9a | 26.47 µs · 1992B · 196a | 23.85 µs · 2168B · 203a | — | 20.59 µs · 1640B · 219a |
| closure-counter | 5.539 µs · 64B · 2a | 918.4 ns · 64B · 2a | 6.03× | — | 40.36 ns · 0B · 0a | 42.58 µs · 92272B · 261a | 12.78 µs · 151B · 3a | 19.22 µs · 1264B · 13a | 79.51 µs · 58312B · 659a | — | 90.27 µs · 35628B · 922a |
| coroutine-gen | 77.44 µs · 128B · 2a | 77.44 µs · 128B · 2a | 1× | — | 41.99 µs · 208B · 7a | — | 329.9 µs · 149936B · 649a | 608.5 µs · 330650B · 6719a | 200.5 µs · 91800B · 3646a | — | — |
| fannkuch | 819.5 µs · 34608B · 1442a | 228.6 µs · 34608B · 1442a | 3.59× | — | 35.48 µs · 17280B · 720a | 2.02 ms · 456578B · 25621a | 1.551 ms · 443136B · 1460a | 19.64 ms · 14005335B · 164946a | 3.369 ms · 1367677B · 16944a | — | 4.767 ms · 2501434B · 88439a |
| fnv1a64 | 122.9 µs · 16400B · 2049a | 1.167 µs · 16400B · 2049a | 105× | — | 902.4 ns · 0B · 0a | 165 µs · 120808B · 3838a | — | 1.483 ms · 608249B · 18475a | 400.2 µs · 162921B · 7429a | — | 153.7 µs · 17282B · 2074a |
| host-call | 726.8 µs · 196592B · 14073a | 722.6 µs · 196592B · 14073a | 1.01× | — | 3.312 µs · 0B · 0a | 178.9 µs · 139264B · 3073a | 131.1 µs · 22536B · 1079a | 793.4 µs · 158048B · 14585a | 107.3 µs · 14920B · 1799a | — | 594.1 µs · 340636B · 9231a |
| i64-wide-fib | 28.21 ms · 2913440B · 364178a | 679.7 µs · 16B · 1a | 41.5× | — | 239.1 µs · 0B · 0a | 24.14 µs · 90520B · 52a | — | 158.2 ms · 64193360B · 1942306a | 343.1 ms · 253472816B · 4248841a | — | 131 ms · 98686672B · 2138708a |
| indirect-recursive-fib | 1.187 ms · 0B · 0a | 60.48 µs · 0B · 0a | 19.6× | 100.3 µs · 8B · 1a | 35.48 µs · 0B · 0a | 2.071 ms · 319346B · 28655a | 1.95 ms · 703B · 2a | 2.542 ms · 4680B · 39a | 11.57 ms · 10158193B · 109494a | — | 24.18 ms · 13061309B · 394061a |
| iterative-fib | 1.123 µs · 0B · 0a | 83.65 ns · 0B · 0a | 13.4× | 85.13 ns · 8B · 1a | 12.45 ns · 0B · 0a | 23.45 µs · 90592B · 61a | 1.163 µs · 160B · 0a | 4.224 µs · 368B · 20a | 6.112 µs · 2448B · 88a | — | 6.191 µs · 944B · 81a |
| mandelbrot | 291.5 µs · 0B · 0a | 10.94 µs · 0B · 0a | 26.6× | — | 6.321 µs · 0B · 0a | 967.2 µs · 292345B · 25008a | 488.2 µs · 169408B · 661a | 8.147 ms · 5558651B · 104191a | 1.488 ms · 324978B · 23643a | — | 1.523 ms · 365398B · 22513a |
| map-ops | 73.19 µs · 9584B · 5a | 73.99 µs · 9584B · 5a | 0.989× | — | 10.85 µs · 4904B · 3a | — | 276.7 µs · 114327B · 198a | 379.6 µs · 66728B · 4078a | — | — | 1.138 ms · 501523B · 15813a |
| mat-mul | 371.8 µs · 6216B · 6a | 8.551 µs · 6216B · 6a | 43.5× | — | 5.461 µs · 6144B · 3a | 1.382 ms · 431714B · 34871a | 673.7 µs · 143168B · 478a | 1.204 ms · 61304B · 537a | 1.44 ms · 90712B · 9350a | — | 926 µs · 36614B · 6036a |
| n-body | 608.4 µs · 504B · 14a | 17.73 µs · 504B · 14a | 34.3× | — | 7.358 µs · 0B · 0a | 1.517 ms · 386898B · 35892a | 1.213 ms · 258913B · 2002a | 3.307 ms · 658435B · 81545a | 2.397 ms · 382714B · 34975a | — | 1.749 ms · 445472B · 20669a |
| n-queens | 432 µs · 120B · 6a | 31.55 µs · 120B · 6a | 13.7× | — | 6.018 µs · 0B · 0a | 951.6 µs · 234865B · 17784a | 571.5 µs · 14423B · 63a | 1.094 ms · 7424B · 35a | 1.461 ms · 410346B · 5147a | — | 2.011 ms · 766004B · 33371a |
| permutation-flips | 129.5 µs · 7680B · 128a | 17.52 µs · 7680B · 128a | 7.39× | — | 1.874 ns · 0B · 0a | 567 µs · 292857B · 9856a | 236.7 µs · 78808B · 451a | 530.1 µs · 122504B · 765a | 494.5 µs · 111976B · 2368a | — | 1.058 µs · 528B · 13a |
| recursive-fib-20 | 720.9 µs · 0B · 0a | 47.07 µs · 0B · 0a | 15.3× | 42.74 µs · 8B · 1a | 21.54 µs · 0B · 0a | 1.941 ms · 319345B · 28655a | 2.177 ms · 704B · 2a | 2.956 ms · 4680B · 39a | 11.5 ms · 9807926B · 109494a | — | 10.92 ms · 8302720B · 192848a |
| recursive-fib-35 | 974.7 ms · 0B · 0a | 64.26 ms · 0B · 0a | 15.2× | 58.24 ms · 8B · 1a | 29.14 ms · 0B · 0a | 2.564 s · 312797168B · 39088167a | 2.993 s · 971008B · 3793a | 4.028 s · 375360B · 46373a | 15.58 s · 13378014176B · 149349946a | — | — |
| sieve | 20.1 µs · 1048B · 2a | 934.8 ns · 1048B · 2a | 21.5× | 1.024 µs · 8B · 1a | 371.9 ns · 0B · 0a | 137.5 µs · 122504B · 1611a | 50.62 µs · 18415B · 44a | 84.26 µs · 1872B · 25a | 76.97 µs · 5704B · 30a | — | 80.55 µs · 4368B · 795a |
| sort-stress | 508.6 µs · 5136B · 512a | 12.61 µs · 5136B · 512a | 40.3× | — | 5.356 µs · 1024B · 2a | 1.298 ms · 270945B · 19965a | 851.6 µs · 36568B · 365a | 1.782 ms · 49104B · 3621a | 1.783 ms · 23448B · 2034a | — | 920.6 µs · 17476B · 1708a |
| spectral-norm | 537 µs · 648B · 6a | 8.326 µs · 648B · 6a | 64.5× | — | 7.033 µs · 576B · 3a | 2.07 ms · 571372B · 54822a | 1.083 ms · 234784B · 925a | 2.449 ms · 275338B · 33729a | 4.613 ms · 2457139B · 52718a | — | 4.204 ms · 2448030B · 74233a |
| string-build | 733 µs · 85408B · 4107a | 602.9 µs · 85408B · 4107a | 1.22× | — | 377 µs · 855889B · 5001a | 2.424 ms · 2211273B · 36101a | 2.312 ms · 1078112B · 13859a | 3.155 ms · 1147384B · 26883a | 2.45 ms · 1326242B · 24469a | — | 3.128 ms · 2372780B · 41432a |
| string-compare | 1.083 ms · 21136B · 1284a | 235.6 µs · 21136B · 1284a | 4.6× | — | 64.71 µs · 9632B · 1585a | 1.696 ms · 296281B · 20528a | 1.129 ms · 59712B · 3431a | 2.329 ms · 50744B · 3848a | 1.819 ms · 209522B · 5520a | — | 2.367 ms · 503608B · 31846a |
| struct-tree-walk | 284 µs · 768B · 8a | 107.3 µs · 768B · 8a | 2.65× | — | 1.878 ns · 0B · 0a | 657.2 µs · 458379B · 5114a | 1.485 ms · 818512B · 11253a | 1.11 ms · 558960B · 6149a | 3.545 ms · 2570664B · 34797a | — | 1.217 µs · 600B · 17a |
| tail-ping-pong | 28.87 µs · 0B · 0a | 491.1 ns · 0B · 0a | 58.8× | — | 890.9 ns · 0B · 0a | 108.1 µs · 106112B · 2001a | 36.28 µs · 21856B · 85a | 113.5 µs · 13984B · 1726a | 116.7 µs · 14280B · 1731a | — | 565.1 µs · 348833B · 9016a |
| tail-sum | 28.84 µs · 0B · 0a | 685.8 ns · 0B · 0a | 42.1× | — | 302.5 ns · 0B · 0a | 107.6 µs · 106112B · 2001a | 36.22 µs · 21856B · 85a | 112.5 µs · 13984B · 1726a | 115.5 µs · 14280B · 1731a | — | 83.99 µs · 4608B · 1017a |
| typed-array-sum | 7.238 µs · 0B · 0a | 264.7 ns · 0B · 0a | 27.3× | 265.1 ns · 8B · 1a | 1.875 ns · 0B · 0a | 45.45 µs · 94208B · 513a | 8.316 µs · 4000B · 15a | 27.52 µs · 2080B · 238a | 17.37 µs · 2496B · 246a | — | — |
| xorshift-i64 | 112.8 µs · 18424B · 2046a | 1.367 µs · 18424B · 2046a | 82.5× | — | 356.2 ns · 0B · 0a | 103.5 µs · 107608B · 2188a | — | 988.3 µs · 447247B · 12735a | 431.1 µs · 199937B · 7772a | — | 102 µs · 13545B · 1317a |

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
