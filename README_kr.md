# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**어디에나 손쉽게 내장하는 빠른 바이트코드 VM.**

minivm은 Go 프로그램 안에서 작은 바이트코드 프로그램을 실행하고, 호스트 함수를 호출하며, 스택/힙/fuel/hook 제한 아래에서 동작합니다. 시작은 빠른 스레디드 인터프리터, 뜨거운 ARM64 경로는 네이티브 코드로 자동 승격됩니다.

```bash
go get github.com/siyul-park/minivm
```

> Go 1.26.2 이상. VM 코어는 Go 표준 라이브러리만 사용합니다.

## 왜 minivm인가

| 필요한 것 | minivm이 주는 것 |
|---|---|
| 런타임 로직 내장 | 일급 함수, 로컬/글로벌, ref, 배열, 구조체, 문자열을 갖춘 바이트코드 |
| Go 코드 호출 | 리플렉션 없는 `HostFunction`, 일반 Go 값용 `Marshal` / `Unmarshal` |
| 실행 경계 제어 | stack, heap, frame, fuel, context, hook 제한 |
| JIT 전에도 빠른 실행 | 클로저 기반 스레디드 디스패치와 재귀 워크로드에서 거의 0에 가까운 할당 |
| 필요한 곳만 네이티브 속도 | 핫 숫자 세그먼트를 자동 ARM64 JIT로 승격 |

## 만들 수 있는 것

- **스크립팅 엔진** — 사용자 정의 로직을 호스트 정책 아래에서 실행
- **룰 엔진** — 재배포 없이 런타임에 복잡한 조건을 평가
- **DSL 런타임** — 검증된 VM 위에 도메인 특화 명령어 셋을 정의
- **플러그인 시스템** — GC가 관리하는 격리 환경에서 바이트코드를 실행

## 성능

재귀 `fib(35)` — linux/amd64, Intel Xeon @ 2.80 GHz, Go 1.26.2:

| 런타임 | ns/op | B/op | allocs/op | vs native Go | 실행 모델 |
|---|---|---|---|---|---|
| native Go | 51,947,220 | 0 | 0 | 1× | 컴파일 |
| wazero | 84,807,148 | 16 | 2 | 1.6× | WASM JIT |
| **minivm** | **1,672,707,295** | **288** | **1** | **32×** | **스레디드 인터프리터** |
| tengo | 2,665,298,176 | 312,800,180 | 39,088,180 | 51× | 바이트코드 VM |
| gopher-lua | 4,081,167,978 | 971,008 | 3,793 | 79× | 레지스터 VM |
| goja | 5,427,175,850 | 383,488 | 46,384 | 105× | 바이트코드 VM |

JIT 없는 인터프리터 중 이 벤치마크에서는 minivm이 가장 빠릅니다: **tengo 대비 1.6×, gopher-lua 대비 2.4×, goja 대비 3.2×**. 재귀 깊이에 관계없이 할당 수가 거의 0에 가깝고, tengo는 fib(35)에서 3,900만 번 할당합니다.

wazero가 앞서는 이유는 모듈 로드 시점에 WebAssembly를 x86-64 네이티브 코드로 JIT 컴파일하기 때문입니다. ARM64에서는 minivm도 핫 세그먼트를 네이티브 코드로 승격하므로 이 격차가 좁혀집니다.

단일 명령어 처리량 (스레디드 인터프리터):

| 워크로드 | ns/op |
|---|---|
| i32/i64/f32/f64 산술 | ~20–22 |
| 분기 (`br`, `br_if`) | ~20–24 |
| 바이트코드 함수 호출 | ~26–29 |
| 호스트 함수 호출 | ~36 |
| 배열 / 구조체 연산 | ~90–140 |

전체 측정 결과: [`docs/benchmarks.md`](docs/benchmarks.md)

## 사용법

### 바이트코드 실행

```go
prog := program.New([]instr.Instruction{
    instr.New(instr.I32_CONST, 6),
    instr.New(instr.I32_CONST, 7),
    instr.New(instr.I32_MUL),
})

vm := interp.New(prog)
defer vm.Close()

if err := vm.Run(context.Background()); err != nil {
    log.Fatal(err)
}

result, _ := vm.Pop() // types.I32(42)
```

### 바이트코드에서 Go 함수 호출

Go 코드를 바이트코드에서 호출 가능한 함수로 노출합니다:

```go
lookup := interp.NewHostFunction(
    &types.FunctionType{
        Params:  []types.Type{types.TypeI32},
        Returns: []types.Type{types.TypeI32},
    },
    func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
        id := params[0].I32()
        price := db.GetPrice(int(id))
        return []types.Boxed{types.BoxI32(price)}, nil
    },
)

prog := program.New(
    []instr.Instruction{
        instr.New(instr.I32_CONST, 42), // 상품 ID
        instr.New(instr.CONST_GET, 0),  // 함수 참조
        instr.New(instr.CALL),
    },
    program.WithConstants(lookup),
)
```

파라미터는 타입 안전한 `[]Boxed`로 전달됩니다. 리플렉션이나 `interface{}` 박싱은 없습니다.

### 함수 정의

함수는 `FunctionBuilder`로 작성하는 일급 상수입니다:

```go
factorial := types.NewFunctionBuilder(&types.FunctionType{
    Params:  []types.Type{types.TypeI32},
    Returns: []types.Type{types.TypeI32},
}).WithLocals(types.TypeI32).Emit(
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.I32_LT_S),
    instr.New(instr.BR_IF, 14),     // n < 1이면 1 반환
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.I32_SUB),
    instr.New(instr.CONST_GET, 0),
    instr.New(instr.CALL),          // factorial(n-1)
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_MUL),       // n * factorial(n-1)
    instr.New(instr.RETURN),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.RETURN),
).Build()
```

### AOT 최적화

VM에 넘기기 전에 상수 연산을 접고 불필요한 분기를 지웁니다:

```go
prog, err := optimize.NewOptimizer(optimize.O1).Optimize(prog)
```

`O1`이 모든 함수에 적용하는 세 가지 패스:

- **상수 폴딩** — `I32_CONST 3, I32_CONST 4, I32_ADD` → `I32_CONST 7`
- **상수 중복 제거** — 동일한 값은 하나의 슬롯으로 통합
- **데드 코드 제거** — 도달 불가능한 기본 블록 삭제

## JIT 동작 방식

기본값으로 두 단계 파이프라인을 실행하며, 임계값과 샘플링 주기는 설정할 수 있습니다:

```
         시작 시
바이트코드 ──────────► 스레디드 인터프리터
                             │
                       128개 명령어마다
                       함수 + IP 샘플 기록
                             │
                       임계값 도달
                       (기본값: 4096 틱)
                             │
                             ▼
                       JIT가 핫 세그먼트 컴파일
                       네이티브 ARM64 생성
                       클로저를 네이티브 코드로 교체
```

JIT는 i32/i64/f32/f64 산술, 비트 연산, 비교, 타입 변환을 네이티브 코드로 컴파일합니다. 현재 스택 형태가 네이티브 세그먼트 시그니처로 표현 가능하면 스택 연산, 로컬, `select`, 분기도 컴파일합니다. 함수 호출, 글로벌, 레퍼런스, 힙 객체 연산은 스레디드 티어에서 처리합니다. 스레디드 인터프리터는 스위치 테이블 대신 클로저 디스패치를 써서 JIT 전에도 충분히 빠릅니다.

## 명령어 셋

WebAssembly를 참고한 커스텀 명령어 셋입니다. opcode는 1바이트, 피연산자는 고정 폭 또는 길이 접두사 형식입니다.

| 분류 | 명령어 |
|---|---|
| 스택 | `NOP` `DROP` `DUP` `SWAP` `SELECT` |
| 제어 흐름 | `BR` `BR_IF` `BR_TABLE` `CALL` `RETURN` `UNREACHABLE` |
| 변수 | `LOCAL_GET/SET/TEE` &nbsp; `GLOBAL_GET/SET/TEE` &nbsp; `CONST_GET` |
| 정수 | `I32_CONST` `I64_CONST` — 산술, 비트 연산, 비교, 변환 |
| 부동소수점 | `F32_CONST` `F64_CONST` — 산술, 비교, 변환 |
| 레퍼런스 | `REF_NULL` `REF_TEST` `REF_CAST` `REF_IS_NULL` `REF_EQ` `REF_NE` |
| 문자열 | `STRING_NEW_UTF32` `STRING_LEN` `STRING_CONCAT` 및 비교 |
| 배열 | `ARRAY_NEW` `ARRAY_NEW_DEFAULT` `ARRAY_LEN` `ARRAY_GET/SET` `ARRAY_FILL/COPY` |
| 구조체 | `STRUCT_NEW` `STRUCT_NEW_DEFAULT` `STRUCT_GET/SET` |

## 옵션

```go
vm := interp.New(prog,
    interp.WithStack(4096),     // 값 스택 용량         (기본값: 1024)
    interp.WithHeap(512),       // 초기 힙 용량         (기본값: 128)
    interp.WithFrame(256),      // 최대 호출 깊이       (기본값: 128)
    interp.WithThreshold(4096), // JIT 트리거 틱; 0=첫 샘플, 음수=비활성화
    interp.WithTick(128),       // 샘플/폴링 주기       (기본값: 128)
    interp.WithFuel(10_000),    // 명령어 예산          (기본값: 무제한)
    interp.WithHook(func(vm *interp.Interpreter) error {
        return nil // 매 틱 호출 — 상태 확인 또는 정책 적용
    }),
    interp.WithCutoff(4),       // 최소 JIT 세그먼트 명령어 수 (기본값: 4)
)
```

`WithTick`은 프로파일 샘플, context 취소 확인, hook 호출 주기, fuel 소비를 함께 제어합니다. `WithFuel(0)`은 무제한이며, 0이 아닌 값은 내부에서 가장 가까운 tick 간격으로 올림합니다. Hook은 `Run` 고루틴에서 동기적으로 실행됩니다.

바이트코드 단위 디버깅(중단점, `Step`, `Next`, `Finish`)은 `NewDebugger` + `WithDebugger`를 사용하세요. JIT는 비활성화됩니다. 자세한 내용: [`docs/debugging.md`](docs/debugging.md).

## 구현 현황

| 기능 | |
|---|---|
| 스레디드 인터프리터 | ✅ |
| AOT 최적화 (O1) | ✅ |
| ARM64 JIT — 숫자 연산, 로컬, 분기 | ✅ |
| ARM64 JIT — 함수 호출, 글로벌, 레퍼런스 | 🔲 계획 중 |
| x86-64 JIT | 🔲 계획 중 |

로드맵: [docs/roadmap.md](docs/roadmap.md)

## 라이선스

[MIT](LICENSE)
