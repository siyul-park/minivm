# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.897 µs · 0B · 0a | 8.869 µs · 0B · 0a | 5.3 µs · 0B · 0a | 1.68× | — | 2.173 µs · 1024B · 128a | 44.11 µs · 96289B · 388a | 18.53 µs · 14376B · 256a | 71.75 µs · 78016B · 770a | 13.49 µs · 5712B · 266a | — | 30.92 µs · 2308B · 276a |
| binary-trees | 1.962 ms · 768B · 8a | 1.963 ms · 768B · 8a | 591.4 µs · 768B · 8a | 3.32× | — | 277 µs · 201936B · 8414a | 3.658 ms · 1468185B · 62877a | 10.31 ms · 6995672B · 92703a | 8.604 ms · 5198697B · 69052a | 27.04 ms · 19457585B · 280714a | — | 28.18 ms · 15128170B · 446106a |
| branch-tree | 1.333 µs · 0B · 0a | 19.03 ns · 0B · 0a | 89.65 ns · 0B · 0a | 14.9× | 626.1 ns · 16B · 1a | 161.4 ns · 0B · 0a | 48.9 µs · 95385B · 660a | 15.48 µs · 2464B · 9a | 26.74 µs · 1992B · 196a | 24.5 µs · 2168B · 203a | — | 20.71 µs · 1640B · 219a |
| closure-counter | 5.542 µs · 64B · 2a | 5.493 µs · 64B · 2a | 919.5 ns · 64B · 2a | 6.03× | — | 40.45 ns · 0B · 0a | 42.4 µs · 92272B · 261a | 13.14 µs · 151B · 3a | 18.9 µs · 1264B · 13a | 77.03 µs · 58312B · 659a | — | 87.69 µs · 35628B · 922a |
| coroutine-gen | 79.31 µs · 128B · 2a | 79.41 µs · 128B · 2a | 79.31 µs · 128B · 2a | 1× | — | 41.81 µs · 208B · 7a | — | 328.6 µs · 149936B · 649a | 570.8 µs · 330650B · 6719a | 192.8 µs · 91800B · 3646a | — | — |
| fannkuch | 830.8 µs · 34608B · 1442a | 813.3 µs · 34608B · 1442a | 214.4 µs · 34608B · 1442a | 3.88× | — | 35.41 µs · 17280B · 720a | 1.98 ms · 456579B · 25621a | 1.564 ms · 443136B · 1460a | 18.77 ms · 14005336B · 164946a | 3.239 ms · 1367677B · 16944a | — | 4.73 ms · 2501451B · 88439a |
| fnv1a64 | 122.5 µs · 16400B · 2049a | 122.4 µs · 16400B · 2049a | 1.154 µs · 16400B · 2049a | 106× | — | 902.7 ns · 0B · 0a | 159.2 µs · 120808B · 3838a | — | 1.328 ms · 608257B · 18475a | 364.6 µs · 162921B · 7429a | — | 153.2 µs · 17282B · 2074a |
| host-call | 468 µs · 41752B · 3080a | 477 µs · 41752B · 3080a | 475.8 µs · 41752B · 3080a | 0.984× | — | 3.312 µs · 0B · 0a | 177.4 µs · 139264B · 3073a | 127.5 µs · 22536B · 1079a | 768.5 µs · 158049B · 14585a | 105.4 µs · 14920B · 1799a | — | 546.4 µs · 340639B · 9231a |
| i64-wide-fib | 27.82 ms · 2913456B · 364178a | 20.81 ms · 1942304B · 242785a | 679.7 µs · 16B · 1a | 40.9× | — | 239.3 µs · 0B · 0a | 23.7 µs · 90520B · 52a | — | 149.7 ms · 64193216B · 1942305a | 347.1 ms · 253472480B · 4248839a | — | 120.5 ms · 98686677B · 2138708a |
| indirect-recursive-fib | 1.192 ms · 0B · 0a | 1.184 ms · 0B · 0a | 60.55 µs · 0B · 0a | 19.7× | 100 µs · 8B · 1a | 35.03 µs · 0B · 0a | 2.062 ms · 319346B · 28655a | 1.951 ms · 703B · 2a | 2.532 ms · 4680B · 39a | 11.42 ms · 10158186B · 109494a | — | 24.7 ms · 13061254B · 394061a |
| iterative-fib | 1.131 µs · 0B · 0a | 1.068 µs · 0B · 0a | 82.65 ns · 0B · 0a | 13.7× | 83.9 ns · 8B · 1a | 12.42 ns · 0B · 0a | 23.83 µs · 90592B · 61a | 1.145 µs · 160B · 0a | 4.238 µs · 368B · 20a | 5.724 µs · 2448B · 88a | — | 6.218 µs · 944B · 81a |
| mandelbrot | 290.7 µs · 0B · 0a | 248.3 µs · 0B · 0a | 11.25 µs · 0B · 0a | 25.8× | — | 6.322 µs · 0B · 0a | 914.5 µs · 292346B · 25008a | 484.1 µs · 169408B · 661a | 7.677 ms · 5558654B · 104191a | 1.462 ms · 324978B · 23643a | — | 1.529 ms · 365399B · 22513a |
| map-ops | 70.76 µs · 8816B · 3a | 70.5 µs · 8816B · 3a | 10.99 µs · 8816B · 3a | 6.44× | — | 10.96 µs · 4904B · 3a | — | 282.2 µs · 114328B · 198a | 373.9 µs · 66728B · 4078a | — | — | 1.102 ms · 501525B · 15813a |
| mat-mul | 376.1 µs · 6216B · 6a | 376.2 µs · 6216B · 6a | 8.896 µs · 6216B · 6a | 42.3× | — | 5.676 µs · 6144B · 3a | 1.314 ms · 431715B · 34871a | 687.5 µs · 143169B · 478a | 1.21 ms · 61305B · 537a | 1.451 ms · 90712B · 9350a | — | 927.9 µs · 36614B · 6036a |
| n-body | 614.6 µs · 504B · 14a | 582.9 µs · 504B · 14a | 19.08 µs · 504B · 14a | 32.2× | — | 7.359 µs · 0B · 0a | 1.457 ms · 386898B · 35892a | 1.21 ms · 258913B · 2002a | 3.248 ms · 658434B · 81545a | 2.38 ms · 382714B · 34975a | — | 1.747 ms · 445463B · 20669a |
| n-queens | 429.4 µs · 120B · 6a | 429.8 µs · 120B · 6a | 31.58 µs · 120B · 6a | 13.6× | — | 6.028 µs · 0B · 0a | 937.9 µs · 234865B · 17784a | 571.1 µs · 14423B · 63a | 1.103 ms · 7424B · 35a | 1.388 ms · 410346B · 5147a | — | 2.027 ms · 766006B · 33371a |
| naive-checksum | 251.4 µs · 0B · 0a | 142.3 µs · 0B · 0a | 8.78 µs · 0B · 0a | 28.6× | — | 2.758 µs · 0B · 0a | 602.6 µs · 235753B · 18206a | 483.5 µs · 174767B · 682a | 855.1 µs · 155640B · 19402a | 761 µs · 163816B · 20409a | — | 311.5 µs · 8976B · 2071a |
| permutation-flips | 128.9 µs · 7680B · 128a | 129 µs · 7680B · 128a | 16.65 µs · 7680B · 128a | 7.74× | — | 1.88 ns · 0B · 0a | 564.9 µs · 292857B · 9856a | 235.4 µs · 78808B · 451a | 527.3 µs · 122504B · 765a | 485.7 µs · 111976B · 2368a | — | 989.9 ns · 528B · 13a |
| recursive-fib-20 | 709.8 µs · 0B · 0a | 725.8 µs · 0B · 0a | 47.12 µs · 0B · 0a | 15.1× | 42.75 µs · 8B · 1a | 21.54 µs · 0B · 0a | 1.917 ms · 319345B · 28655a | 2.174 ms · 704B · 2a | 2.98 ms · 4680B · 39a | 11.6 ms · 9807925B · 109494a | — | 10.48 ms · 8302702B · 192848a |
| recursive-fib-35 | 969.2 ms · 0B · 0a | 980 ms · 0B · 0a | 63.9 ms · 0B · 0a | 15.2× | 58.22 ms · 8B · 1a | 29.18 ms · 0B · 0a | 2.559 s · 312797344B · 39088167a | 3.026 s · 971008B · 3793a | 4.063 s · 375360B · 46373a | 15.52 s · 13378018672B · 149349975a | — | — |
| sieve | 20.36 µs · 1048B · 2a | 20.45 µs · 1048B · 2a | 937.5 ns · 1048B · 2a | 21.7× | 1.016 µs · 8B · 1a | 372.1 ns · 0B · 0a | 140.5 µs · 122504B · 1611a | 50.77 µs · 18416B · 44a | 83.67 µs · 1872B · 25a | 76.89 µs · 5704B · 30a | — | 81.97 µs · 4368B · 795a |
| sort-stress | 503.2 µs · 5136B · 512a | 508.9 µs · 5136B · 512a | 12.85 µs · 5136B · 512a | 39.2× | — | 5.382 µs · 1024B · 2a | 1.252 ms · 270945B · 19965a | 853.1 µs · 36567B · 365a | 1.798 ms · 49104B · 3621a | 1.77 ms · 23448B · 2034a | — | 927.2 µs · 17474B · 1708a |
| spectral-norm | 532.3 µs · 648B · 6a | 461.2 µs · 648B · 6a | 8.393 µs · 648B · 6a | 63.4× | — | 7.032 µs · 576B · 3a | 1.987 ms · 571370B · 54822a | 1.074 ms · 234784B · 925a | 2.462 ms · 275337B · 33729a | 4.575 ms · 2457135B · 52718a | — | 4.014 ms · 2448010B · 74233a |
| string-build | 720.3 µs · 85408B · 4107a | 714.7 µs · 85408B · 4107a | 469.5 µs · 85408B · 4107a | 1.53× | — | 378.7 µs · 855889B · 5001a | 2.484 ms · 2211272B · 36101a | 2.429 ms · 1078110B · 13859a | 3.104 ms · 1147386B · 26883a | 2.445 ms · 1326242B · 24469a | — | 3.074 ms · 2372776B · 41432a |
| string-compare | 1.083 ms · 21136B · 1284a | 1.085 ms · 21136B · 1284a | 218.1 µs · 21136B · 1284a | 4.97× | — | 63.1 µs · 9632B · 1585a | 1.654 ms · 296281B · 20528a | 1.144 ms · 59712B · 3431a | 2.311 ms · 50744B · 3848a | 1.819 ms · 209521B · 5520a | — | 2.372 ms · 503620B · 31846a |
| struct-tree-walk | 282.5 µs · 768B · 8a | 282.1 µs · 768B · 8a | 107.6 µs · 768B · 8a | 2.62× | — | 1.899 ns · 0B · 0a | 656.1 µs · 458379B · 5114a | 1.318 ms · 818512B · 11253a | 1.055 ms · 558960B · 6149a | 3.376 ms · 2570664B · 34797a | — | 1.18 µs · 600B · 17a |
| tail-ping-pong | 29.36 µs · 0B · 0a | 29.39 µs · 0B · 0a | 483.3 ns · 0B · 0a | 60.7× | — | 890.7 ns · 0B · 0a | 108.8 µs · 106112B · 2001a | 35.47 µs · 21856B · 85a | 112 µs · 13984B · 1726a | 116.5 µs · 14280B · 1731a | — | 530.4 µs · 348832B · 9016a |
| tail-sum | 29.21 µs · 0B · 0a | 29.34 µs · 0B · 0a | 678.2 ns · 0B · 0a | 43.1× | — | 302.5 ns · 0B · 0a | 107.3 µs · 106112B · 2001a | 35.57 µs · 21856B · 85a | 112 µs · 13984B · 1726a | 116.2 µs · 14280B · 1731a | — | 84.55 µs · 4608B · 1017a |
| typed-array-sum | 7.358 µs · 0B · 0a | 7.328 µs · 0B · 0a | 263.7 ns · 0B · 0a | 27.9× | 259.7 ns · 8B · 1a | 1.877 ns · 0B · 0a | 47.85 µs · 94208B · 513a | 8.499 µs · 4000B · 15a | 27.44 µs · 2080B · 238a | 17.24 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.6 µs · 18424B · 2046a | 111.5 µs · 18424B · 2046a | 1.426 µs · 18424B · 2046a | 78.3× | — | 356.1 ns · 0B · 0a | 105.7 µs · 107608B · 2188a | — | 926.4 µs · 447252B · 12735a | 407.7 µs · 199938B · 7772a | — | 101.7 µs · 13545B · 1317a |

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
