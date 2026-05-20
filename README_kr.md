# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**컴파일러를 만들지 않고 스크립팅 엔진을 출시하세요.**

minivm은 Go 서비스에 가벼운 실행 엔진을 내장합니다. 바이트코드를 조립하고, Go 함수를 연결하고, 실행하면 됩니다. 핫 경로는 스레디드 인터프리터에서 ARM64 네이티브 코드로 자동 승격됩니다 — 플래그도, 워밍업도, 설정도 없습니다.

```bash
go get github.com/siyul-park/minivm
```

> Go 1.26.2 이상. VM 코어는 Go 표준 라이브러리만 사용하며, CLI와 테스트에는 작은 Go 모듈 의존성이 있습니다.

---

## 어떤 걸 만들 수 있나요

- **스크립팅 엔진** — 사용자가 작성한 로직을 호스트 정책 아래에서 실행
- **룰 엔진** — 재배포 없이 런타임에 복잡한 조건을 평가
- **DSL 런타임** — 검증된 VM 위에 도메인에 특화된 명령어 셋을 정의
- **플러그인 시스템** — GC가 관리하는 격리 환경에서 애플리케이션 정의 바이트코드 실행

## Go 임베딩에 맞춘 설계

minivm은 Go 서비스에 자연스럽게 들어가도록 설계되었습니다:

- **간단한 임베딩** — Go API로 프로그램을 만들고 같은 프로세스 안에서 실행
- **타입드 호스트 호출** — 리플렉션 없이 `[]Boxed` 브리지로 Go 함수 연결
- **작은 런타임 모델** — GC가 관리하는 힙과 커스텀 바이트코드 형식 사용
- **자동 티어링** — 스레디드 인터프리터로 시작하고 ARM64 숫자 핫 경로를 네이티브 코드로 승격

명령어 셋은 WebAssembly의 익숙한 아이디어를 참고하면서도 Go 네이티브 스크립팅, 룰, DSL 실행에 집중합니다. 현재 방향은 [docs/roadmap.md](docs/roadmap.md)에 정리되어 있습니다.

---

## 사용법

### 바이트코드 실행

스택의 모든 값은 `uint64`입니다. 메모리 관리는 VM이, 바이트코드 설계는 여러분이 담당합니다.

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

애플리케이션과 게스트 코드를 연결하는 핵심 기능입니다. Go 함수를 바이트코드에서 그대로 호출할 수 있습니다:

```go
lookup := interp.NewHostFunction(
    &types.FunctionType{
        Params:  []types.Type{types.TypeI32},
        Returns: []types.Type{types.TypeI32},
    },
    func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
        id := params[0].I32()
        price := db.GetPrice(int(id)) // Go 코드 직접 호출
        return []types.Boxed{types.BoxI32(price)}, nil
    },
)

prog := program.New(
    []instr.Instruction{
        instr.New(instr.I32_CONST, 42), // 상품 ID
        instr.New(instr.CONST_GET, 0),  // lookup 함수 참조
        instr.New(instr.CALL),          // 호출
    },
    program.WithConstants(lookup),
)
```

파라미터는 `[]Boxed`로 타입 안전하게 전달됩니다. 리플렉션도, `interface{}` 박싱도 없습니다.

### 함수 정의

함수는 일급 상수입니다. `FunctionBuilder`로 선언적으로 작성하고 상수 테이블에 등록합니다:

```go
factorial := types.NewFunctionBuilder(&types.FunctionType{
    Params:  []types.Type{types.TypeI32},
    Returns: []types.Type{types.TypeI32},
}).WithLocals(types.TypeI32).Emit(
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.I32_LT_S),
    instr.New(instr.BR_IF, 14),        // n < 1이면 1 반환
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_CONST, 1),
    instr.New(instr.I32_SUB),
    instr.New(instr.CONST_GET, 0),
    instr.New(instr.CALL),             // factorial(n-1)
    instr.New(instr.LOCAL_GET, 0),
    instr.New(instr.I32_MUL),          // n * factorial(n-1)
    instr.New(instr.RETURN),
    instr.New(instr.I32_CONST, 1),     // 기저 사례
    instr.New(instr.RETURN),
).Build()

prog := program.New(
    []instr.Instruction{
        instr.New(instr.I32_CONST, 10),
        instr.New(instr.CONST_GET, 0),
        instr.New(instr.CALL),
    },
    program.WithConstants(factorial),
)
```

### 실행 전 AOT 최적화

VM에 넘기기 전에 컴파일 타임에 결정 가능한 연산을 접고, 도달 불가능한 분기를 제거합니다:

```go
prog, err := optimize.NewOptimizer(optimize.O1).Optimize(prog)
```

`O1`이 모든 함수에 적용하는 패스:
- **상수 폴딩** — `I32_CONST 3, I32_CONST 4, I32_ADD` → `I32_CONST 7`
- **상수 중복 제거** — 동일한 상수값은 하나의 슬롯으로 통합
- **데드 코드 제거** — 도달 불가능한 기본 블록 삭제

---

## JIT는 어떻게 작동하나요

minivm은 **아무것도 결정하지 않아도 되는 2단계 파이프라인**으로 실행됩니다:

```
             시작 시
바이트코드 ─────────────► 스레디드 클로저
                                │
                        128개 명령어마다:
                        함수/IP 실행 샘플 기록
                                │
                  샘플이 임계값에 도달 (기본값: 4096틱)
                                │
                                ▼
                          JIT 컴파일러 실행
                          네이티브 ARM64 생성
                          클로저를 네이티브 코드로 교체
```

JIT는 i32/i64/f32/f64의 산술, 비트 연산, 비교, 타입 변환을 네이티브 코드로 컴파일합니다. 현재 스택 형태를 네이티브 세그먼트 시그니처로 표현할 수 있으면 일부 스택 연산, 로컬, 상수, `select`, 분기 명령도 처리합니다. 함수 호출, 글로벌, 레퍼런스, 힙 객체 연산은 스레디드 티어에서 처리됩니다. 스레디드 인터프리터는 switch 기반이 아닌 클로저 디스패치를 사용해, JIT가 활성화되기 전에도 충분히 빠릅니다.

**실제로 의미하는 것:** 연산 집약적 루프는 대략 처음 4096개 실행 명령어 동안 인터프리터로 실행되고, 이후 핫 네이티브 세그먼트가 이어받습니다. 별도로 튜닝할 필요가 없습니다.

---

## 명령어 셋

WebAssembly에서 영감을 받았지만 의도적으로 커스텀 명령어 셋을 사용합니다. 모든 opcode는 1바이트이며 피연산자는 고정 폭 또는 길이 접두사 방식으로 인코딩됩니다.

| 분류 | |
|---|---|
| 스택 | `NOP` `DROP` `DUP` `SWAP` `SELECT` |
| 제어 흐름 | `BR` `BR_IF` `BR_TABLE` `CALL` `RETURN` `UNREACHABLE` |
| 변수 | `LOCAL_GET/SET/TEE` &nbsp; `GLOBAL_GET/SET/TEE` &nbsp; `CONST_GET` |
| 정수 | `I32_CONST` `I64_CONST` — 사칙연산, 시프트, 비트 연산, 비교, 변환 |
| 부동소수점 | `F32_CONST` `F64_CONST` — 사칙연산, 비교, 변환 |
| 레퍼런스 | `REF_NULL` `REF_TEST` `REF_CAST` `REF_IS_NULL` `REF_EQ` `REF_NE` |
| 문자열 | `STRING_NEW_UTF32` `STRING_LEN` `STRING_CONCAT` 및 비교 |
| 배열 | `ARRAY_NEW` `ARRAY_NEW_DEFAULT` `ARRAY_LEN` `ARRAY_GET/SET` `ARRAY_FILL/COPY` |
| 구조체 | `STRUCT_NEW` `STRUCT_NEW_DEFAULT` `STRUCT_GET/SET` |

---

## 옵션

```go
vm := interp.New(prog,
    interp.WithStack(4096),      // 값 스택 슬롯 수    (기본값: 1024)
    interp.WithHeap(512),        // 초기 힙 용량       (기본값: 128)
    interp.WithFrame(256),       // 최대 호출 깊이     (기본값: 128)
    interp.WithThreshold(4096),  // JIT 트리거 틱 수; 0은 첫 샘플, 음수이면 JIT 비활성화
    interp.WithTick(128),        // 샘플/폴링 주기     (기본값: 128)
    interp.WithFuel(10_000),     // 명령어 예산       (기본값: 무제한)
    interp.WithHook(func(vm *interp.Interpreter) error {
        return nil              // 상태 확인 또는 호스트 정책 적용
    }),
    interp.WithCutoff(4),          // 최소 JIT 세그먼트  (기본값: 4)
)
```

`WithTick`은 프로파일 샘플, context 취소 확인, hook 호출 주기, fuel 소비를 함께 제어합니다. `WithFuel`은 기대 명령어 예산을 받고 내부에서 가장 가까운 tick 간격으로 올림 변환합니다. `WithFuel(0)`은 무제한입니다. Hook은 `Run` 고루틴에서 동기적으로 실행되며 실행 중인 인터프리터를 받습니다. 인터프리터에 대한 동시 접근은 피하고, 상태를 변경할 때는 VM 불변식을 유지해야 합니다.

바이트코드 단위 디버깅은 `NewDebugger`를 `WithDebugger`와 함께 사용합니다.
디버거는 breakpoint와 `Step`, `Next`, `Finish`를 제공하며, `WithDebugger`는
명령어 단위 hook을 설정하고 JIT를 비활성화합니다. 자세한 내용은
[`docs/debugging.md`](docs/debugging.md)를 참고하세요.

---

## 구현 현황

| | |
|---|---|
| 스레디드 인터프리터 | ✅ |
| AOT 최적화 (O1) | ✅ |
| ARM64 JIT — 숫자 연산, 로컬, 분기 | ✅ |
| ARM64 JIT — 함수 호출, 글로벌, 레퍼런스 | 🔲 계획 중 |
| x86-64 JIT | 🔲 계획 중 |

로드맵 우선순위와 향후 방향은 [docs/roadmap.md](docs/roadmap.md)를 참고하세요.

---

## 라이선스

[MIT](LICENSE)
