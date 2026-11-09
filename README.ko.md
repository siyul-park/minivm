# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) · **한국어**

## Go를 위한 작고 내장하기 쉬운 바이트코드 VM

Go 안에서 자원 제한, 타입이 지정된 호스트 호출, 스레디드 기준 실행을 명시적으로 제어하며 동적 로직을 실행합니다.

- **제한된 실행** — 스택, 힙, 프레임 깊이, fuel, hook, context.
- **Go 연동** — Go 값과 함수를 양방향으로 변환하고, 성능이 중요한 호출에는 타입이 지정된 `HostFunction` 경로를 씁니다.
- **네이티브 티어** — 기본으로 켜진 ARM64 실행과 스레디드 폴백.

```bash
go get github.com/siyul-park/minivm
```

> Go 1.26.2 이상이 필요합니다. VM 코어는 Go 표준 라이브러리만 사용합니다.

## 빠른 시작

`6 × 7`을 계산하는 바이트코드 프로그램을 실행합니다.

```go
prog, err := program.NewBuilder().
    Emit(instr.I32_CONST, 6).
    Emit(instr.I32_CONST, 7).
    Emit(instr.I32_MUL).
    Build()
if err != nil {
    log.Fatal(err)
}

vm, err := interp.New(prog) // verifies prog before it can run
if err != nil {
    log.Fatal(err)
}
defer vm.Close()

if err := vm.Run(context.Background()); err != nil {
    log.Fatal(err)
}

result, err := vm.Pop()
if err != nil {
    log.Fatal(err)
}
fmt.Println(result) // 42
```

minivm의 실행 모델은 명확합니다. 바이트코드를 입력하고, 검증된 런타임에서 실행한 뒤, 타입이 지정된 값을 꺼냅니다. 연료와 힙 한도는 기본적으로 무제한이며 `interp.WithFuel`과 `interp.WithHeapLimit`로 설정합니다. 신뢰할 수 없는 바이트코드에는 이 한도와 취소 가능한 컨텍스트를 함께 지정하세요. 여기 나온 모든 예제의 실행 가능한 버전은 [`interp/example_test.go`](interp/example_test.go)에 있습니다.

## minivm을 선택하는 이유

| 기능 | 제공하는 가치 |
|---|---|
| 내장형 런타임 | 일급 함수, 로컬, 글로벌, 클로저, ref, 문자열, 배열, 구조체, 맵, 코루틴, 구조화된 에러 |
| 호스트 연동 | 타입이 지정된 `HostFunction`과 일반 Go 값용 `Marshal`, `Unmarshal` |
| 자원 제어 | 스택, 힙, 프레임, fuel, context, hook, 디버거 제어 |
| 빠른 기본 실행 | 낮은 정상 상태 할당을 유지하는 클로저 기반 스레디드 디스패치 |
| 의미론적 기준 | 명시적인 자원 제어를 갖는 스레디드 인터프리터 |
| 안전한 실행 허용 | 실행 전 정적 바이트코드 검증 |

### 활용 분야

- 호스트 정책 아래에서 사용자 동작을 실행하는 **스크립팅 엔진**
- 재배포 없이 런타임 결정을 바꾸는 **룰 엔진**
- 작고 독립적인 실행 계층이 필요한 **DSL 런타임**
- 확장 로직을 호스트 애플리케이션과 분리하는 **플러그인 시스템**

## 바이트코드에서 Go 호출

`Marshal`은 일반 Go 함수를 VM 함수로 바꿉니다. 첫 파라미터 `context.Context`는 실행 중인 context를 받고, 마지막 `error` 결과는 VM 에러가 됩니다.

```go
add := func(ctx context.Context, a, b int64) (int64, error) { return a + b, nil }
typ := &types.FunctionType{
    Params:  []types.Type{types.TypeI64(), types.TypeI64()},
    Returns: []types.Type{types.TypeI64()},
}
prog, err := program.NewBuilder().
    Globals(typ).
    Emit(instr.I64_CONST, 2).
    Emit(instr.I64_CONST, 3).
    Emit(instr.GLOBAL_GET, 0).
    Emit(instr.CALL).
    Build()
if err != nil {
    log.Fatal(err)
}

vm, err := interp.New(prog)
if err != nil {
    log.Fatal(err)
}
defer vm.Close()

fn, err := vm.Marshal(add)
if err != nil {
    log.Fatal(err)
}
addr, err := vm.Alloc(fn) // the caller owns this reference
if err != nil {
    log.Fatal(err)
}
if err := vm.SetGlobal(0, types.BoxRef(addr)); err != nil { // the global takes it
    log.Fatal(err)
}
if err := vm.Run(context.Background()); err != nil {
    log.Fatal(err)
}
result, err := vm.Pop()
if err != nil {
    log.Fatal(err)
}
fmt.Println(result) // 5
```

성능이 중요한 호출에는 `interp.NewHostFunction`이 리플렉션 없이 `[]types.Boxed`를 직접 받고 반환합니다.

## Go에서 바이트코드 호출

`Unmarshal`은 VM 함수를 타입이 지정된 Go 함수로 바꿉니다.

```go
ctx := context.Background()

// addr holds a VM function; see ExampleRegistry_Unmarshal in interp/example_test.go for the full setup.
var inc func(context.Context, int64) (int64, error)
if err := vm.Unmarshal(types.BoxRef(addr), &inc); err != nil {
    log.Fatal(err)
}
n, err := inc(ctx, 41)
if err != nil {
    log.Fatal(err)
}
fmt.Println(n) // 42
```

함수 타입에는 마지막 `error` 결과를 선언하세요. 없으면 호출 실패 시 panic이 발생합니다. 호스트 함수 안에서 호출하면 같은 인터프리터에서 중첩 실행됩니다. 호스트 함수 밖에서 `vm.Run` 실행 중에 호출하면 `interp.ErrInterpreterBusy`를 반환합니다.

변환 표, API별 소유권, 저수준 `HostFunction` 경로는 [호스트 연동](docs/host-integration.md)을 참고하세요.

## 성능

스레디드 인터프리터가 현재 실행 기준입니다. arm64에서는 네이티브 티어가 기본으로 켜지며 `interp.WithThreshold`로 조정하거나 끌 수 있습니다. 현재 측정값과 재현 명령은 [벤치마크](docs/benchmarks.md)가 단일 owner입니다.

## 런타임 도구

### 신뢰할 수 없는 바이트코드 검증

`interp.New`와 `interp.NewPool`은 프로그램의 스냅샷을 검증하고 그 스냅샷만 실행하므로, 이후 `prog`를 바꿔도 검증을 우회할 수 없습니다. 스냅샷은 코드와 상수를 포함하며, 타입 객체(예: 함수 시그니처)는 `prog`와 공유되므로 생성 후 절대 변경하면 안 됩니다([검증](docs/verification.md)과 동일). 인터프리터를 만들지 않고 바이트코드만 검사하려면:

```go
if err := program.Verify(prog); err != nil {
    log.Fatal(err)
}
```

검증기는 잘못된 제어 흐름, 스택 동작, 타입 불일치를 거부합니다.

### 바이트코드 최적화

```go
prog, err := optimize.New(optimize.O2).Optimize(prog)
if err != nil {
    log.Fatal(err)
}
```

바이트코드 최적화기는 실행 전에 프로그램을 다시 씁니다. 검증하지 않으며 잘못된 입력에서 패닉할 수 있으므로 먼저 `program.Verify`를 거치세요. 단계는 상수 폴딩과 데드 코드 제거부터 공통 부분식 제거, 승격, 루프 불변식 이동까지 지원합니다.

### 실행 제어

```go
vm, err := interp.New(prog,
    interp.WithStack(4096),
    interp.WithHeap(512),
    interp.WithFrame(256),
    interp.WithFuel(10_000),
    interp.WithTick(128),
)
if err != nil {
    log.Fatal(err)
}
defer vm.Close()
```

정책 검사는 hook으로 처리합니다. 명령어 단위 중단점과 단계 실행은 `debug.NewDebugger`를 `WithHook(dbg.Hook)`, `WithTick(1)`과 함께 사용합니다([디버깅](docs/debugging.md)).

## 아키텍처

```text
Program → Verify → optimize? → interp.New (verify) → threaded ⇄ native (ARM64)
```

스레디드 인터프리터가 의미론적 기준이자 전체 실행 엔진입니다. ARM64에서 JIT는 프로그램이 컴파일 비용을 회수할 만큼 실행된 뒤 hot function에 네이티브 실행을 추가하고(`WithThreshold`), 지원하지 않는 경로는 스레디드 실행으로 돌아갑니다.

명령어 셋은 WebAssembly를 참고했지만 의도적으로 독자 설계했습니다. 1바이트 opcode와 고정 폭 또는 길이 접두사 피연산자를 사용합니다.

- [아키텍처](docs/architecture.md)
- [명령어 셋](docs/instruction-set.md)
- [JIT 내부 구조](docs/jit-internals.md)
- [메모리 모델](docs/memory-model.md)

## 구현 현황

| 기능 | 상태 |
|---|---|
| 스레디드 인터프리터 | ✅ 사용 가능 |
| 정적 바이트코드 검증기 | ✅ 사용 가능 |
| 바이트코드 최적화기 (`optimize.O1`-`O3`) | ✅ 사용 가능 |
| ARM64 네이티브 티어 (기본 활성, `interp.WithThreshold`) | ✅ 사용 가능 |
| 디버거와 프로파일러 | ✅ 사용 가능 |
| x86-64 네이티브 백엔드 | 🔲 미구현 |

x86-64 어셈블러 백엔드는 아직 없습니다. 현재 우선순위는 [로드맵](docs/roadmap.md)을 참고하세요.

## 문서

| 문서 | 용도 |
|---|---|
| [문서 목록](docs/README.md) | 전체 프로젝트 문서 탐색 |
| [호환성](docs/compatibility.md) | Go, 플랫폼, CGO, 빌드 태그 지원 확인 |
| [호스트 연동](docs/host-integration.md) | 바이트코드와 Go 값 및 함수 연결 |
| [검증](docs/verification.md) | 정적 실행 허용 검사와 한계 이해 |
| [디버깅](docs/debugging.md) | 중단점, 단계 실행, 상태 조회 사용 |
| [테스트](docs/testing.md) | 실행 가능한 명세와 자동화 게이트 이해 |
| [벤치마크](docs/benchmarks.md) | 성능과 할당 측정 재현 |

## 라이선스

[MIT](LICENSE)
