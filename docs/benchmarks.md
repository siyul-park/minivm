# Benchmarks

Compares deterministic workloads across minivm's threaded interpreter, JIT, Wazero, Native Go, and supported runtime fixtures for contributors reviewing performance behavior. The benchmark registry is the single execution index, and `make benchmark` regenerates the result table directly from the executable suite. Go fixtures are also the Yaegi input, so the same `registry.Register` boundary is exercised without AST rewriting.

## Results

<!-- benchmark-table:start -->

| Kernel | Threaded | JIT | JIT speedup | Wazero | Native Go | Tengo | GopherLua | Goja | gpython | CPython | Yaegi |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| allocation-graph | 9.177 µs · 0B · 0a | 5.364 µs · 0B · 0a | 1.71× | — | 2.111 µs · 1024B · 128a | 37.89 µs · 96288B · 388a | 16.74 µs · 14376B · 256a | 70.19 µs · 78016B · 770a | 13.89 µs · 5712B · 266a | — | 24.01 µs · 1744B · 148a |
| binary-trees | 1.965 ms · 768B · 8a | 610.4 µs · 768B · 8a | 3.22× | — | 285.1 µs · 201936B · 8414a | 3.864 ms · 1468181B · 62877a | 10.57 ms · 6995684B · 92703a | 8.94 ms · 5198708B · 69052a | 28.29 ms · 19457597B · 280714a | — | 28.91 ms · 15127356B · 446011a |
| branch-tree | 1.339 µs · 0B · 0a | 139 ns · 0B · 0a | 9.63× | 629 ns · 16B · 1a | 160.8 ns · 0B · 0a | 48.97 µs · 95385B · 660a | 15.19 µs · 2463B · 9a | 26.59 µs · 1992B · 196a | 24.19 µs · 2168B · 203a | — | 14.97 µs · 1252B · 122a |
| closure-counter | 5.5 µs · 64B · 2a | 936.5 ns · 64B · 2a | 5.87× | — | 39.79 ns · 0B · 0a | 52.14 µs · 92272B · 261a | 12.84 µs · 151B · 3a | 19.1 µs · 1264B · 13a | 78.77 µs · 58312B · 659a | — | 82.28 µs · 35048B · 793a |
| fannkuch | 828.2 µs · 34608B · 1442a | 265.6 µs · 34608B · 1442a | 3.12× | — | 36 µs · 17280B · 720a | 1.99 ms · 456579B · 25621a | 1.569 ms · 443135B · 1460a | 19.37 ms · 14005343B · 164946a | 3.257 ms · 1367677B · 16944a | — | 4.587 ms · 2491460B · 85959a |
| fnv1a64 | 122.6 µs · 16400B · 2049a | 1.202 µs · 16400B · 2049a | 102× | — | 902.9 ns · 0B · 0a | 162.9 µs · 120808B · 3838a | — | 1.419 ms · 608257B · 18475a | 391 µs · 162921B · 7429a | — | 102.4 µs · 9105B · 1050a |
| i64-wide-fib | 28.56 ms · 2913456B · 364178a | 861.5 µs · 16B · 1a | 33.1× | — | 239.4 µs · 0B · 0a | 24.82 µs · 90520B · 52a | — | 148.1 ms · 64193402B · 1942306a | 343.7 ms · 253472560B · 4248839a | — | 116.8 ms · 98686704B · 2138709a |
| indirect-recursive-fib | 1.186 ms · 0B · 0a | 76.35 µs · 0B · 0a | 15.5× | 100.6 µs · 8B · 1a | 35.11 µs · 0B · 0a | 2.059 ms · 319346B · 28655a | 1.927 ms · 704B · 2a | 2.565 ms · 4680B · 39a | 11.36 ms · 10158186B · 109494a | — | 23.86 ms · 13061326B · 394061a |
| iterative-fib | 1.1 µs · 0B · 0a | 72.55 ns · 0B · 0a | 15.2× | 85.02 ns · 8B · 1a | 11.9 ns · 0B · 0a | 23.58 µs · 90592B · 61a | 1.142 µs · 160B · 0a | 4.218 µs · 368B · 20a | 5.865 µs · 2448B · 88a | — | 4.452 µs · 784B · 50a |
| mandelbrot | 290.7 µs · 0B · 0a | 11.06 µs · 0B · 0a | 26.3× | — | 6.343 µs · 0B · 0a | 915.4 µs · 292345B · 25008a | 480.7 µs · 169408B · 661a | 7.879 ms · 5558651B · 104191a | 1.454 ms · 324979B · 23643a | — | 1.266 ms · 324077B · 19107a |
| mat-mul | 370.3 µs · 6216B · 6a | 8.775 µs · 6216B · 6a | 42.2× | — | 5.503 µs · 6144B · 3a | 1.33 ms · 431714B · 34871a | 660.4 µs · 143168B · 478a | 1.207 ms · 61304B · 537a | 1.461 ms · 90712B · 9350a | — | 614 µs · 16875B · 1134a |
| n-body | 606.5 µs · 504B · 14a | 18.89 µs · 504B · 14a | 32.1× | — | 7.347 µs · 0B · 0a | 1.463 ms · 386898B · 35892a | 1.211 ms · 258913B · 2002a | 3.205 ms · 658437B · 81545a | 2.401 ms · 382715B · 34975a | — | 1.597 ms · 400499B · 18248a |
| n-queens | 431.9 µs · 120B · 6a | 32.84 µs · 120B · 6a | 13.2× | — | 6.01 µs · 0B · 0a | 927.2 µs · 234865B · 17784a | 566.8 µs · 14423B · 63a | 1.098 ms · 7424B · 35a | 1.376 ms · 410345B · 5147a | — | 1.761 ms · 749458B · 29235a |
| permutation-flips | 127.9 µs · 7680B · 128a | 17.08 µs · 7680B · 128a | 7.49× | — | 1.875 ns · 0B · 0a | 548.5 µs · 292858B · 9856a | 228.3 µs · 78808B · 451a | 524.8 µs · 122504B · 765a | 480.5 µs · 111976B · 2368a | — | 979.4 ns · 528B · 13a |
| recursive-fib-20 | 711.3 µs · 0B · 0a | 60.94 µs · 0B · 0a | 11.7× | 42.72 µs · 8B · 1a | 21.48 µs · 0B · 0a | 1.895 ms · 319345B · 28655a | 2.146 ms · 704B · 2a | 2.959 ms · 4680B · 39a | 11.24 ms · 9807922B · 109494a | — | 10.02 ms · 8302705B · 192848a |
| recursive-fib-35 | 970.7 ms · 0B · 0a | 82.84 ms · 0B · 0a | 11.7× | 58.21 ms · 8B · 1a | 29.14 ms · 0B · 0a | 2.554 s · 312797376B · 39088168a | 2.934 s · 971008B · 3793a | 4.028 s · 375360B · 46373a | 15.12 s · 13378019760B · 149349980a | — | — |
| sieve | 19.88 µs · 1048B · 2a | 907.4 ns · 1048B · 2a | 21.9× | 1.025 µs · 8B · 1a | 350.9 ns · 0B · 0a | 137.6 µs · 122504B · 1611a | 50.55 µs · 18415B · 44a | 84.53 µs · 1872B · 25a | 77.09 µs · 5704B · 30a | — | 40.48 µs · 1256B · 43a |
| sort-stress | 505 µs · 5136B · 512a | 12.66 µs · 5136B · 512a | 39.9× | — | 5.808 µs · 1024B · 2a | 1.243 ms · 270945B · 19965a | 839.1 µs · 36568B · 365a | 1.79 ms · 49104B · 3621a | 1.798 ms · 23448B · 2034a | — | 873.9 µs · 13121B · 935a |
| spectral-norm | 534.2 µs · 648B · 6a | 34.77 µs · 648B · 6a | 15.4× | — | 7.047 µs · 576B · 3a | 1.969 ms · 571371B · 54822a | 1.073 ms · 234784B · 925a | 2.408 ms · 275337B · 33729a | 4.465 ms · 2457134B · 52718a | — | 3.698 ms · 2443881B · 69364a |
| string-build | 721.1 µs · 85408B · 4107a | 614.6 µs · 85408B · 4107a | 1.17× | — | 385 µs · 855891B · 5001a | 2.433 ms · 2211275B · 36101a | 2.283 ms · 1078112B · 13859a | 3.105 ms · 1147384B · 26883a | 2.432 ms · 1326243B · 24469a | — | 2.92 ms · 2348545B · 38417a |
| string-compare | 1.081 ms · 21136B · 1284a | 273.1 µs · 21136B · 1284a | 3.96× | — | 62.25 µs · 9632B · 1585a | 1.65 ms · 296281B · 20528a | 1.121 ms · 59712B · 3431a | 2.324 ms · 50744B · 3848a | 1.767 ms · 209521B · 5520a | — | 1.448 ms · 369683B · 15139a |
| struct-tree-walk | 283.6 µs · 768B · 8a | 110.6 µs · 768B · 8a | 2.56× | — | 1.875 ns · 0B · 0a | 642.3 µs · 458378B · 5114a | 1.318 ms · 818513B · 11253a | 1.068 ms · 558961B · 6149a | 3.428 ms · 2570666B · 34797a | — | 1.135 µs · 608B · 17a |
| tail-ping-pong | 29.21 µs · 0B · 0a | 518 ns · 0B · 0a | 56.4× | — | 890.8 ns · 0B · 0a | 106.5 µs · 106112B · 2001a | 35.35 µs · 21856B · 85a | 107.5 µs · 13984B · 1726a | 117.4 µs · 14280B · 1731a | — | 515.7 µs · 348835B · 9016a |
| tail-sum | 29.09 µs · 0B · 0a | 650.4 ns · 0B · 0a | 44.7× | — | 301.6 ns · 0B · 0a | 108.6 µs · 106112B · 2001a | 35.33 µs · 21856B · 85a | 107.2 µs · 13984B · 1726a | 117 µs · 14280B · 1731a | — | 30.12 µs · 584B · 16a |
| typed-array-sum | 7.206 µs · 0B · 0a | 232.6 ns · 0B · 0a | 31× | 266.7 ns · 8B · 1a | 1.888 ns · 0B · 0a | 46.49 µs · 94208B · 513a | 8.342 µs · 4000B · 15a | 27.1 µs · 2080B · 238a | 17.12 µs · 2496B · 246a | — | — |
| xorshift-i64 | 110.2 µs · 18424B · 2046a | 1.44 µs · 18424B · 2046a | 76.5× | — | 360.3 ns · 0B · 0a | 103.5 µs · 107608B · 2188a | — | 894.3 µs · 447254B · 12736a | 389.7 µs · 199937B · 7772a | — | 62.19 µs · 7360B · 547a |

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
