# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 4.846 µs · 0B · 0a | 4.326 µs · 0B · 0a | 1.12× | — | 921.4 ns · 1024B · 128a | 15.53 µs · 96289B · 388a | 6.808 µs · 14376B · 256a | 26.75 µs · 78016B · 770a | 5.605 µs · 5712B · 266a | 3.002 µs · 0B · 0a | 12.12 µs · 1744B · 148a |
| binary-trees | 974.5 µs · 768B · 8a | 607.9 µs · 768B · 8a | 1.6× | — | 118.9 µs · 201936B · 8414a | 1.803 ms · 1468190B · 62877a | 4.251 ms · 6995690B · 92703a | 3.795 ms · 5198710B · 69052a | 10.24 ms · 19457620B · 280714a | 986.2 µs · 71B · 0a | 12.37 ms · 15128596B · 446011a |
| branch-tree | 491.3 ns · 0B · 0a | 61.03 ns · 0B · 0a | 8.05× | 156.2 ns · 16B · 1a | 78.68 ns · 0B · 0a | 22.14 µs · 95385B · 660a | 8.681 µs · 2464B · 9a | 14.31 µs · 1992B · 196a | 12.23 µs · 2168B · 203a | 4.866 µs · 0B · 0a | 7.51 µs · 1252B · 122a |
| closure-counter | 2.555 µs · 64B · 2a | 480.8 ns · 64B · 2a | 5.31× | — | 34.58 ns · 0B · 0a | 17.25 µs · 92273B · 261a | 5.699 µs · 152B · 3a | 9.832 µs · 1264B · 13a | 30.14 µs · 58312B · 659a | 3.89 µs · 0B · 0a | 33.35 µs · 35057B · 793a |
| fannkuch | 402.5 µs · 34608B · 1442a | 129 µs · 34608B · 1442a | 3.12× | — | 18.19 µs · 17280B · 720a | 1.001 ms · 456579B · 25621a | 747.5 µs · 443137B · 1460a | 7.234 ms · 14005445B · 164946a | 1.507 ms · 1367685B · 16944a | 420.8 µs · 31B · 0a | 2.196 ms · 2491961B · 85959a |
| fnv1a64 | 55.2 µs · 16400B · 2049a | 1.139 µs · 16400B · 2049a | 48.5× | — | 960.3 ns · 0B · 0a | 71.38 µs · 120809B · 3838a | — | 559.7 µs · 608623B · 18475a | 155.4 µs · 162922B · 7429a | 61.36 µs · 4B · 0a | 50.18 µs · 9107B · 1050a |
| i64-wide-fib | 13.99 ms · 2913456B · 364178a | 457.5 µs · 16B · 1a | 30.6× | — | 157.3 µs · 0B · 0a | 11.18 µs · 90520B · 52a | — | 62.02 ms · 64194632B · 1942305a | 131.2 ms · 253473728B · 4248844a | 16.91 ms · 1460B · 9a | 46.22 ms · 98688026B · 2138709a |
| indirect-recursive-fib | 614.3 µs · 0B · 0a | 40.92 µs · 0B · 0a | 15× | 41.81 µs · 8B · 1a | 16.86 µs · 0B · 0a | 930.4 µs · 319345B · 28655a | 948.9 µs · 704B · 2a | 1.368 ms · 4680B · 39a | 4.465 ms · 10158245B · 109494a | 557.3 µs · 41B · 0a | 10.74 ms · 13062628B · 394062a |
| iterative-fib | 485.6 ns · 0B · 0a | 36.83 ns · 0B · 0a | 13.2× | 51.1 ns · 8B · 1a | 8.675 ns · 0B · 0a | 11.83 µs · 90592B · 61a | 509.3 ns · 160B · 0a | 2.16 µs · 368B · 20a | 2.585 µs · 2448B · 88a | 367.1 ns · 0B · 0a | 2.052 µs · 784B · 50a |
| mandelbrot | 137.7 µs · 0B · 0a | 5.739 µs · 0B · 0a | 24× | — | 2.827 µs · 0B · 0a | 433.5 µs · 292346B · 25008a | 235.9 µs · 169408B · 661a | 3.265 ms · 5558661B · 104191a | 693.9 µs · 324979B · 23643a | 179.6 µs · 13B · 0a | 573.8 µs · 324144B · 19107a |
| mat-mul | 169.4 µs · 6216B · 6a | 3.853 µs · 6216B · 6a | 44× | — | 2.768 µs · 6144B · 3a | 591.3 µs · 431715B · 34871a | 339.2 µs · 143170B · 478a | 657.3 µs · 61304B · 537a | 665.2 µs · 90712B · 9350a | 205.5 µs · 16B · 0a | 310.4 µs · 16877B · 1134a |
| n-body | 307.8 µs · 504B · 14a | 7.779 µs · 504B · 14a | 39.6× | — | 3.334 µs · 0B · 0a | 695 µs · 386897B · 35892a | 588.4 µs · 258915B · 2002a | 1.681 ms · 658438B · 81545a | 1.179 ms · 382716B · 34975a | 225.2 µs · 20B · 0a | 755 µs · 398833B · 18046a |
| n-queens | 201.6 µs · 120B · 6a | 15.58 µs · 120B · 6a | 12.9× | — | 4.115 µs · 0B · 0a | 460.9 µs · 234866B · 17784a | 282.7 µs · 14423B · 63a | 627.5 µs · 7424B · 35a | 681.6 µs · 410346B · 5147a | 172.1 µs · 15B · 0a | 834.3 µs · 749600B · 29235a |
| permutation-flips | 54.92 µs · 7680B · 128a | 9.48 µs · 7680B · 128a | 5.79× | — | 1.697 ns · 0B · 0a | 245.6 µs · 292859B · 9856a | 101.3 µs · 78808B · 451a | 256.3 µs · 122504B · 765a | 232.7 µs · 111976B · 2368a | 44.18 µs · 3B · 0a | 383.4 ns · 528B · 13a |
| recursive-fib-20 | 354.6 µs · 0B · 0a | 30.23 µs · 0B · 0a | 11.7× | 32.2 µs · 8B · 1a | 14.06 µs · 0B · 0a | 830.1 µs · 319347B · 28655a | 1.109 ms · 704B · 2a | 1.507 ms · 4680B · 39a | 4.635 ms · 9807938B · 109494a | 557 µs · 41B · 0a | 4.074 ms · 8303804B · 192848a |
| recursive-fib-35 | 490.9 ms · 0B · 0a | 40.74 ms · 0B · 0a | 12.1× | 44.19 ms · 10B · 1a | 19.12 ms · 0B · 0a | 1.144 s · 312798352B · 39088176a | 1.452 s · 971008B · 3793a | 2.051 s · 375360B · 46373a | 6.142 s · 13378055200B · 149350304a | 739.2 ms · 7288B · 47a | — |
| sieve | 7.47 µs · 1048B · 2a | 532.3 ns · 1048B · 2a | 14× | 675.2 ns · 8B · 1a | 242.7 ns · 0B · 0a | 63.63 µs · 122505B · 1611a | 24.23 µs · 18416B · 44a | 42.97 µs · 1872B · 25a | 35.13 µs · 5704B · 30a | 12.6 µs · 1B · 0a | 20.66 µs · 1256B · 43a |
| sort-stress | 186.7 µs · 5136B · 512a | 7.995 µs · 5136B · 512a | 23.4× | — | 4.2 µs · 1024B · 2a | 591 µs · 270946B · 19965a | 420.4 µs · 36567B · 365a | 888.4 µs · 49104B · 3621a | 886.6 µs · 23448B · 2034a | 334.9 µs · 27B · 0a | 445.2 µs · 13120B · 935a |
| spectral-norm | 278.7 µs · 648B · 6a | 17.74 µs · 648B · 6a | 15.7× | — | 2.802 µs · 576B · 3a | 916.8 µs · 571374B · 54822a | 534.1 µs · 234784B · 925a | 1.242 ms · 275337B · 33729a | 1.934 ms · 2457143B · 52718a | 416.3 µs · 36B · 0a | 1.715 ms · 2444373B · 69364a |
| string-build | 356.5 µs · 85408B · 4107a | 277.8 µs · 85408B · 4107a | 1.28× | — | 154.1 µs · 855897B · 5001a | 943 µs · 2211281B · 36101a | 1.114 ms · 1078113B · 13859a | 1.443 ms · 1147387B · 26883a | 1.044 ms · 1326248B · 24469a | 285.4 µs · 22B · 0a | 1.26 ms · 2349028B · 38417a |
| struct-tree-walk | 143.6 µs · 768B · 8a | 90.05 µs · 768B · 8a | 1.59× | — | 1.72 ns · 0B · 0a | 289.9 µs · 458383B · 5114a | 551 µs · 818514B · 11253a | 466 µs · 558965B · 6149a | 1.291 ms · 2570677B · 34797a | 113.4 µs · 8B · 0a | 460.7 ns · 608B · 17a |
| tail-ping-pong | 10.22 µs · 0B · 0a | 10.13 µs · 0B · 0a | 1.01× | — | 2.133 µs · 0B · 0a | 46.9 µs · 106112B · 2001a | 15.34 µs · 21856B · 85a | 53.52 µs · 13984B · 1726a | 55.01 µs · 14280B · 1731a | 21.25 µs · 1B · 0a | 213.1 µs · 348901B · 9016a |
| tail-sum | 9.92 µs · 0B · 0a | 519.3 ns · 0B · 0a | 19.1× | — | 230.8 ns · 0B · 0a | 46.39 µs · 106112B · 2001a | 15.25 µs · 21856B · 85a | 53.23 µs · 13984B · 1726a | 56.51 µs · 14280B · 1731a | 20.99 µs · 1B · 0a | 16.11 µs · 584B · 16a |
| typed-array-sum | 2.826 µs · 0B · 0a | 147.1 ns · 0B · 0a | 19.2× | 153.9 ns · 8B · 1a | 1.723 ns · 0B · 0a | 19.22 µs · 94208B · 513a | 3.517 µs · 4000B · 15a | 13.49 µs · 2080B · 238a | 7.912 µs · 2496B · 246a | 2.623 µs · 0B · 0a | — |
| xorshift-i64 | 53.25 µs · 18424B · 2046a | 779.3 ns · 18424B · 2046a | 68.3× | — | 420.6 ns · 0B · 0a | 46.32 µs · 107608B · 2188a | — | 402.1 µs · 447535B · 12736a | 170.4 µs · 199938B · 7772a | 49.54 µs · 4B · 0a | 31.38 µs · 7362B · 547a |


<!-- benchmark-table:end -->

## Method

The timed loop excludes fixture loading, compilation, module construction, function lookup, and correctness checks where the runtime permits it. `quick`, `standard`, and `deep` use 100 ms, 300 ms, and 1 s target times; `—` denotes an unavailable or intentionally inapplicable runtime.

## Ownership

- `benchmarks/registry` owns benchmark metadata and runtime source attachment.
- `benchmarks/fixtures` owns benchmark source and native implementations.
- `benchmarks/cmd/benchreport` owns this generated table.

## Related

- `../benchmarks/README.md` — fixture, registry, runtime, and measurement contract
- `writing.md` — Markdown document rules
- `testing.md` — test structure and validation
