# minivm

[![CI](https://github.com/siyul-park/minivm/actions/workflows/ci.yml/badge.svg)](https://github.com/siyul-park/minivm/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/siyul-park/minivm/branch/main/graph/badge.svg)](https://codecov.io/gh/siyul-park/minivm)
[![Go Reference](https://pkg.go.dev/badge/github.com/siyul-park/minivm.svg)](https://pkg.go.dev/github.com/siyul-park/minivm)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

[English](README.md) · **한국어**

## Go를 위한 작고 내장하기 쉬운 바이트코드 VM

성능, 자원, 호스트 연동에 대한 통제력을 유지하면서 Go 애플리케이션 안에서
동적 로직을 실행합니다.

- **제한된 실행** — 스택, 힙, 호출 깊이, fuel, hook, context를 제어합니다.
- **직접적인 호스트 연동** — 타입이 지정된 리플렉션 없는 함수로 Go를 호출합니다.
- **적응형 성능** — 스레디드 인터프리터로 시작해 핫 ARM64 함수와 루프를
  네이티브 코드로 승격합니다.

```bash
go get github.com/siyul-park/minivm
```

> Go 1.26.2 이상이 필요합니다. VM 코어는 Go 표준 라이브러리만 사용합니다.

## 빠른 시작

`6 × 7`을 계산하는 바이트코드 프로그램을 실행합니다.

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

minivm의 실행 모델은 명확합니다. 바이트코드를 입력하고, 통제된 런타임에서
실행한 뒤, 타입이 지정된 값을 꺼냅니다.

## minivm을 선택하는 이유

| 기능 | 제공하는 가치 |
|---|---|
| 내장형 런타임 | 일급 함수, 로컬, 글로벌, 클로저, ref, 문자열, 배열, 구조체, 맵, 코루틴, 구조화된 에러 |
| 호스트 연동 | 타입이 지정된 `HostFunction`과 일반 Go 값용 `Marshal`, `Unmarshal` |
| 자원 제어 | 스택, 힙, 프레임, fuel, context, hook, 디버거 제어 |
| 빠른 기본 실행 | 핵심 워크로드에서 낮은 정상 상태 할당을 유지하는 클로저 기반 스레디드 디스패치 |
| 핫 경로 가속 | 지원되는 함수와 루프를 위한 적응형 ARM64 트레이스 JIT |
| 안전한 실행 허용 | 실행 전 정적 바이트코드 검증 |

### 활용 분야

- 호스트 정책 아래에서 사용자 동작을 실행하는 **스크립팅 엔진**
- 재배포 없이 런타임 결정을 바꾸는 **룰 엔진**
- 작고 독립적인 실행 계층이 필요한 **DSL 런타임**
- 확장 로직을 호스트 애플리케이션과 분리하는 **플러그인 시스템**

## 바이트코드에서 Go 호출

타입이 지정된 호스트 함수로 Go 동작을 노출합니다.

```go
lookup := interp.NewHostFunction(
    &types.FunctionType{
        Params:  []types.Type{types.TypeI32},
        Returns: []types.Type{types.TypeI32},
    },
    func(vm *interp.Interpreter, params []types.Boxed) ([]types.Boxed, error) {
        price := db.GetPrice(int(params[0].I32()))
        return []types.Boxed{types.BoxI32(price)}, nil
    },
)
```

파라미터와 결과는 타입이 지정된 `[]types.Boxed`로 유지됩니다. 직접 호출
경로에는 리플렉션이나 `interface{}` 박싱이 필요하지 않습니다.

마샬링, 호스트 객체, 수명 규칙은 [호스트 연동](docs/host-integration.md)을
참고하세요.

## 성능

minivm은 JIT 컴파일 전에도 실용적인 성능을 제공하고, 반복 실행으로 네이티브
트레이스의 이점이 커지는 경로를 추가로 가속하도록 설계했습니다.

Apple M4 Pro, `darwin/arm64`, Go 1.26.2에서 측정한 대표 중앙값입니다
(`ns/op`, 낮을수록 좋음).

| 런타임 | 반복 피보나치 | 재귀 피보나치 | 분기 트리 |
|---|---:|---:|---:|
| native Go | 8.444 | 19,129,096 | 77.55 |
| wazero | 47.98 | 44,150,405 | 156.3 |
| **minivm/default** | **69.9** | **47,048,123** | **222.4** |
| minivm/threaded | 718.5 | 487,293,996 | 949.4 |

`minivm/default`는 적응형 ARM64 트레이스 JIT 정책을 사용합니다. 성능은
워크로드마다 다릅니다. 지원하지 않는 경로는 스레디드 인터프리터에서 계속
실행되며, 아직 트레이싱의 이점을 얻지 못하는 워크로드도 있습니다.

전체 비교표, 메모리 결과, 측정 범위, 재현 명령은
[벤치마크](docs/benchmarks.md)를 참고하세요.

## 런타임 도구

### 신뢰할 수 없는 바이트코드 검증

```go
if err := program.Verify(prog); err != nil {
    log.Fatal(err)
}
```

검증기는 실행 전에 잘못된 제어 흐름, 스택 동작, 타입 불일치를 거부합니다.
`run` CLI는 불러온 프로그램을 기본적으로 검증합니다.

### 실행 전 최적화

```go
prog, err := optimize.New(optimize.O2).Optimize(prog)
```

최적화 단계는 로컬 상수 폴딩과 중복 제거부터 데드 코드 제거, 블록 간 전역 값
번호화까지 지원합니다.

### 실행 제어

```go
vm := interp.New(prog,
    interp.WithStack(4096),
    interp.WithHeap(512),
    interp.WithFrame(256),
    interp.WithFuel(10_000),
    interp.WithThreshold(4096),
    interp.WithTick(128),
)
```

정책 검사는 hook을 사용하고, 명령어 단위 중단점과 단계 실행은
`NewDebugger`와 `WithDebugger`를 사용합니다.

## 아키텍처

```text
Program -> verifier / optimizer -> threaded interpreter -> ARM64 trace JIT
                                   |                    |
                                   +-- 항상 실행 가능 --+-- 핫 경로만
```

스레디드 인터프리터가 완전한 실행 엔진입니다. 트레이스 JIT는 적응형 가속
계층으로, 지원되는 핫 경로는 네이티브 ARM64 코드로 컴파일하고 지원하지 않거나
콜드 상태인 모든 경로는 인터프리터에서 계속 실행합니다.

명령어 셋은 WebAssembly를 참고했지만 의도적으로 독자 설계했습니다. 1바이트
opcode와 고정 폭 또는 길이 접두사 피연산자를 사용합니다.

- [아키텍처](docs/architecture.md)
- [명령어 셋](docs/instruction-set.md)
- [JIT 내부 구조](docs/jit-internals.md)
- [메모리 모델](docs/memory-model.md)

## 구현 현황

| 기능 | 상태 |
|---|---|
| 스레디드 인터프리터 | ✅ 사용 가능 |
| 정적 바이트코드 검증기 | ✅ 사용 가능 |
| AOT 최적화 (`O1`-`O3`) | ✅ 사용 가능 |
| ARM64 트레이스 JIT | ✅ 사용 가능 |
| 디버거와 프로파일러 | ✅ 사용 가능 |
| x86-64 JIT | 🔲 계획 중 |

x86-64 어셈블러 패키지는 현재 코드를 내보내지 않는 placeholder입니다. 현재
우선순위는 [로드맵](docs/roadmap.md)을 참고하세요.

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
