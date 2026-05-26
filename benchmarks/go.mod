module github.com/siyul-park/minivm/benchmarks

go 1.26.2

require (
	github.com/d5/tengo/v2 v2.17.0
	github.com/dop251/goja v0.0.0-20260311135729-065cd970411c
	github.com/siyul-park/minivm v0.0.0
	github.com/stretchr/testify v1.11.1
	github.com/tetratelabs/wazero v1.11.0
	github.com/yuin/gopher-lua v1.1.2
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dlclark/regexp2 v1.11.4 // indirect
	github.com/go-sourcemap/sourcemap v2.1.3+incompatible // indirect
	github.com/google/pprof v0.0.0-20230207041349-798e818bf904 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	golang.org/x/sys v0.38.0 // indirect
	golang.org/x/text v0.3.8 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/siyul-park/minivm => ../
