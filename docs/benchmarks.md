# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | Optimized | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 8.9 µs · 0B · 0a | 8.886 µs · 0B · 0a | 5.36 µs · 0B · 0a | 1.66× | — | 2.262 µs · 1024B · 128a | 41.32 µs · 96288B · 388a | 21.2 µs · 14376B · 256a | 77.22 µs · 78016B · 770a | 14.68 µs · 5712B · 266a | — | 30.8 µs · 2308B · 276a |
| binary-trees | 1.969 ms · 768B · 8a | 1.963 ms · 768B · 8a | 588.7 µs · 768B · 8a | 3.34× | — | 294.8 µs · 201936B · 8414a | 3.902 ms · 1468183B · 62877a | 10.6 ms · 6995683B · 92703a | 9.089 ms · 5198702B · 69052a | 27.75 ms · 19457633B · 280714a | — | 28.71 ms · 15128200B · 446106a |
| branch-tree | 1.341 µs · 0B · 0a | 18.63 ns · 0B · 0a | 92.17 ns · 0B · 0a | 14.5× | 628.9 ns · 16B · 1a | 160.3 ns · 0B · 0a | 46.44 µs · 95385B · 660a | 15.31 µs · 2463B · 9a | 26.61 µs · 1992B · 196a | 24.03 µs · 2168B · 203a | — | 20.55 µs · 1640B · 219a |
| closure-counter | 5.539 µs · 64B · 2a | 5.501 µs · 64B · 2a | 923.4 ns · 64B · 2a | 6× | — | 40.11 ns · 0B · 0a | 42.91 µs · 92272B · 261a | 12.88 µs · 151B · 3a | 19.2 µs · 1264B · 13a | 79.32 µs · 58312B · 659a | — | 90.28 µs · 35628B · 922a |
| coroutine-gen | 78.71 µs · 128B · 2a | 78.62 µs · 128B · 2a | 78.73 µs · 128B · 2a | 1× | — | 42.04 µs · 208B · 7a | — | 336.9 µs · 149936B · 649a | 610 µs · 330650B · 6719a | 205.5 µs · 91800B · 3646a | — | — |
| fannkuch | 830.3 µs · 34608B · 1442a | 814.6 µs · 34608B · 1442a | 216.3 µs · 34608B · 1442a | 3.84× | — | 36.22 µs · 17280B · 720a | 2.025 ms · 456580B · 25621a | 1.565 ms · 443137B · 1460a | 19.46 ms · 14005353B · 164946a | 3.425 ms · 1367677B · 16944a | — | 4.87 ms · 2501449B · 88439a |
| fnv1a64 | 122.1 µs · 16400B · 2049a | 121.7 µs · 16400B · 2049a | 1.167 µs · 16400B · 2049a | 105× | — | 902.9 ns · 0B · 0a | 164.4 µs · 120808B · 3838a | — | 1.486 ms · 608254B · 18475a | 404.8 µs · 162921B · 7429a | — | 155.3 µs · 17282B · 2074a |
| host-call | 470.2 µs · 41752B · 3080a | 473.6 µs · 41752B · 3080a | 470.6 µs · 41752B · 3080a | 0.999× | — | 3.313 µs · 0B · 0a | 181.3 µs · 139264B · 3073a | 131.3 µs · 22536B · 1079a | 791.8 µs · 158048B · 14585a | 106.7 µs · 14920B · 1799a | — | 589.6 µs · 340637B · 9231a |
| i64-wide-fib | 28.19 ms · 2913456B · 364178a | 20.78 ms · 1942304B · 242785a | 679.7 µs · 16B · 1a | 41.5× | — | 239.5 µs · 0B · 0a | 23.05 µs · 90520B · 52a | — | 158.5 ms · 64193288B · 1942305a | 344.1 ms · 253472896B · 4248841a | — | 128 ms · 98686565B · 2138708a |
| indirect-recursive-fib | 1.19 ms · 0B · 0a | 1.186 ms · 0B · 0a | 60.47 µs · 0B · 0a | 19.7× | 99.2 µs · 8B · 1a | 34.78 µs · 0B · 0a | 2.066 ms · 319345B · 28655a | 1.948 ms · 704B · 2a | 2.59 ms · 4680B · 39a | 11.5 ms · 10158185B · 109494a | — | 24.16 ms · 13061286B · 394061a |
| iterative-fib | 1.131 µs · 0B · 0a | 1.072 µs · 0B · 0a | 80.57 ns · 0B · 0a | 14× | 85.81 ns · 8B · 1a | 11.91 ns · 0B · 0a | 24.03 µs · 90592B · 61a | 1.207 µs · 160B · 0a | 4.224 µs · 368B · 20a | 6.265 µs · 2448B · 88a | — | 6.184 µs · 944B · 81a |
| mandelbrot | 293.1 µs · 0B · 0a | 249.4 µs · 0B · 0a | 11.26 µs · 0B · 0a | 26× | — | 6.331 µs · 0B · 0a | 971 µs · 292345B · 25008a | 497.9 µs · 169408B · 661a | 8.307 ms · 5558654B · 104191a | 1.488 ms · 324978B · 23643a | — | 1.523 ms · 365400B · 22513a |
| map-ops | 70.83 µs · 8816B · 3a | 70.64 µs · 8816B · 3a | 11.41 µs · 8816B · 3a | 6.21× | — | 11.29 µs · 4904B · 3a | — | 283.7 µs · 114328B · 198a | 388.1 µs · 66728B · 4078a | — | — | 1.125 ms · 501519B · 15813a |
| mat-mul | 374.6 µs · 6216B · 6a | 375 µs · 6216B · 6a | 8.885 µs · 6216B · 6a | 42.2× | — | 5.611 µs · 6144B · 3a | 1.377 ms · 431714B · 34871a | 688.5 µs · 143169B · 478a | 1.222 ms · 61304B · 537a | 1.443 ms · 90712B · 9350a | — | 930.9 µs · 36615B · 6036a |
| n-body | 612.9 µs · 504B · 14a | 581.7 µs · 504B · 14a | 19 µs · 504B · 14a | 32.3× | — | 7.346 µs · 0B · 0a | 1.509 ms · 386898B · 35892a | 1.226 ms · 258913B · 2002a | 3.366 ms · 658436B · 81545a | 2.411 ms · 382714B · 34975a | — | 1.745 ms · 445474B · 20669a |
| n-queens | 429.9 µs · 120B · 6a | 433.2 µs · 120B · 6a | 31.61 µs · 120B · 6a | 13.6× | — | 6.004 µs · 0B · 0a | 952.3 µs · 234865B · 17784a | 573.2 µs · 14423B · 63a | 1.105 ms · 7424B · 35a | 1.447 ms · 410345B · 5147a | — | 2.019 ms · 766009B · 33371a |
| naive-checksum | 253.7 µs · 0B · 0a | 142 µs · 0B · 0a | 8.777 µs · 0B · 0a | 28.9× | — | 2.758 µs · 0B · 0a | 627.3 µs · 235753B · 18206a | 504.1 µs · 174768B · 682a | 874.7 µs · 155640B · 19402a | 797.2 µs · 163816B · 20409a | — | 313.8 µs · 8977B · 2071a |
| permutation-flips | 129.3 µs · 7680B · 128a | 129.2 µs · 7680B · 128a | 17.27 µs · 7680B · 128a | 7.48× | — | 1.869 ns · 0B · 0a | 570.1 µs · 292858B · 9856a | 238.1 µs · 78808B · 451a | 530.6 µs · 122504B · 765a | 499.5 µs · 111976B · 2368a | — | 1.027 µs · 528B · 13a |
| recursive-fib-20 | 717.4 µs · 0B · 0a | 718.6 µs · 0B · 0a | 47.14 µs · 0B · 0a | 15.2× | 42.78 µs · 8B · 1a | 21.55 µs · 0B · 0a | 1.947 ms · 319346B · 28655a | 2.173 ms · 704B · 2a | 2.96 ms · 4680B · 39a | 11.75 ms · 9807920B · 109494a | — | 10.95 ms · 8302662B · 192847a |
| recursive-fib-35 | 977.6 ms · 0B · 0a | 977.5 ms · 0B · 0a | 63.95 ms · 0B · 0a | 15.3× | 58.23 ms · 8B · 1a | 29.15 ms · 0B · 0a | 2.569 s · 312797424B · 39088167a | 3.002 s · 971008B · 3793a | 4.027 s · 375360B · 46373a | 15.67 s · 13378016976B · 149349965a | — | — |
| sieve | 20.36 µs · 1048B · 2a | 20.35 µs · 1048B · 2a | 941.9 ns · 1048B · 2a | 21.6× | 1.028 µs · 8B · 1a | 350.8 ns · 0B · 0a | 139.7 µs · 122504B · 1611a | 52.53 µs · 18416B · 44a | 84.4 µs · 1872B · 25a | 76.59 µs · 5704B · 30a | — | 81.36 µs · 4368B · 795a |
| sort-stress | 505.7 µs · 5136B · 512a | 509 µs · 5136B · 512a | 12.88 µs · 5136B · 512a | 39.3× | — | 5.795 µs · 1024B · 2a | 1.288 ms · 270945B · 19965a | 855.9 µs · 36567B · 365a | 1.798 ms · 49104B · 3621a | 1.785 ms · 23448B · 2034a | — | 927.2 µs · 17474B · 1708a |
| spectral-norm | 533.4 µs · 648B · 6a | 459.5 µs · 648B · 6a | 8.393 µs · 648B · 6a | 63.6× | — | 7.065 µs · 576B · 3a | 2.048 ms · 571371B · 54822a | 1.088 ms · 234784B · 925a | 2.498 ms · 275337B · 33729a | 4.719 ms · 2457133B · 52718a | — | 4.252 ms · 2448031B · 74233a |
| string-build | 725.7 µs · 85408B · 4107a | 718.2 µs · 85408B · 4107a | 595 µs · 85408B · 4107a | 1.22× | — | 380 µs · 855889B · 5001a | 2.423 ms · 2211272B · 36101a | 2.366 ms · 1078110B · 13859a | 3.127 ms · 1147382B · 26883a | 2.475 ms · 1326242B · 24469a | — | 3.143 ms · 2372801B · 41432a |
| string-compare | 1.086 ms · 21136B · 1284a | 1.086 ms · 21136B · 1284a | 219.7 µs · 21136B · 1284a | 4.94× | — | 63.3 µs · 9632B · 1585a | 1.67 ms · 296281B · 20528a | 1.143 ms · 59712B · 3431a | 2.321 ms · 50744B · 3848a | 1.805 ms · 209522B · 5520a | — | 2.367 ms · 503619B · 31846a |
| struct-tree-walk | 282.1 µs · 768B · 8a | 282.7 µs · 768B · 8a | 107.6 µs · 768B · 8a | 2.62× | — | 1.895 ns · 0B · 0a | 668.4 µs · 458379B · 5114a | 1.393 ms · 818512B · 11253a | 1.1 ms · 558961B · 6149a | 3.619 ms · 2570671B · 34797a | — | 1.223 µs · 600B · 17a |
| tail-ping-pong | 29.35 µs · 0B · 0a | 29.43 µs · 0B · 0a | 482.7 ns · 0B · 0a | 60.8× | — | 891.5 ns · 0B · 0a | 111.3 µs · 106112B · 2001a | 37.65 µs · 21856B · 85a | 112 µs · 13984B · 1726a | 117.1 µs · 14280B · 1731a | — | 563.2 µs · 348831B · 9016a |
| tail-sum | 29.24 µs · 0B · 0a | 29.3 µs · 0B · 0a | 680.3 ns · 0B · 0a | 43× | — | 301.5 ns · 0B · 0a | 114.4 µs · 106112B · 2001a | 36.54 µs · 21856B · 85a | 111.7 µs · 13984B · 1726a | 117.2 µs · 14280B · 1731a | — | 84.91 µs · 4608B · 1017a |
| typed-array-sum | 7.358 µs · 0B · 0a | 7.371 µs · 0B · 0a | 266.3 ns · 0B · 0a | 27.6× | 267.8 ns · 8B · 1a | 1.864 ns · 0B · 0a | 46.86 µs · 94208B · 513a | 8.505 µs · 4000B · 15a | 27.38 µs · 2080B · 238a | 17.35 µs · 2496B · 246a | — | — |
| xorshift-i64 | 111.3 µs · 18424B · 2046a | 111.2 µs · 18424B · 2046a | 1.324 µs · 18424B · 2046a | 84.1× | — | 361.2 ns · 0B · 0a | 103.6 µs · 107608B · 2188a | — | 995.2 µs · 447250B · 12735a | 426.9 µs · 199938B · 7772a | — | 101.3 µs · 13545B · 1317a |

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
