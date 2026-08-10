# tmpl 정렬 구현 계획

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `tmpl extract` 가 구조가 다른 문서에서도 공통부의 템플릿을 뽑고, 표현하지 못하는 것을 스스로 신고하게 한다.

**Architecture:** 정렬기를 `internal/diff` 에서 새 `internal/align` 패키지로 **순수 이동**하고(`diff` 의 동작은 한 톨도 안 바뀐다), 짝짓기를 내는 `align.Match` 를 더한 뒤, `Extract` 의 구조 게이트를 그것으로 갈아끼운다. 표현 못 하는 서브트리가 있으면 **기본은 거절**이고 `--allow-unrepresented` 로만 진행한다.

**Tech Stack:** Go 1.26+, 표준 라이브러리만 (외부 의존 없음)

**설계 문서:** `docs/superpowers/specs/2026-08-11-tmpl-align-design.md` — 이 계획의 모든 결정의 근거다. 계획과 설계가 어긋나 보이면 **설계가 이긴다.**

## Global Constraints

- **외부 의존 금지.** `go.mod` 에 `require` 블록이 생기면 안 된다
- **주석·에러 메시지·커밋 메시지는 한국어.** 커밋 접두사(`feat:`·`fix:`·`test:`·`refactor:`·`docs:`)만 영어
- **TDD 강제.** 모든 태스크는 RED 를 눈으로 본 뒤 구현한다. 테스트가 처음부터 통과하면 그 테스트는 무의미하므로 다시 설계한다
- **`internal/opc`·`internal/xmlscan`·`internal/patch` 를 건드리지 않는다**
- **출력은 결정론적이어야 한다 (I3)** — 맵 순회 순서가 출력에 새면 같은 입력이 다른 바이트를 낸다
- **Go 는 `/opt/homebrew/bin/go`, gofmt 는 `/opt/homebrew/bin/gofmt`** — PATH 에 없을 수 있으니 절대 경로를 쓴다
- `/opt/homebrew/bin/gofmt -l cmd internal` 무출력, `/opt/homebrew/bin/go vet ./...` 무음 필수
- 실제 픽스처: `testdata/real/` 의 `form-a.docx`·`form-b.docx`, `deck-a.pptx`·`deck-b.pptx` — **전부 구조가 같다.** 구조가 다른 쌍은 합성으로 만든다

---

## 파일 구조

| 파일 | 책임 |
|---|---|
| `internal/align/align.go` (신규) | 트리 구성·서브트리 해시·형제 LCS·상한·속성 색인 — `internal/diff/align.go` 에서 옮겨온다 |
| `internal/align/align_test.go` (신규) | 위의 테스트 — `internal/diff/align_test.go` 에서 옮겨온다 |
| `internal/align/match.go` (신규) | `Match` — 두 트리의 짝짓기와 한쪽에만 있는 서브트리 |
| `internal/align/match_test.go` (신규) | `Match` 의 단위 테스트 |
| `internal/diff/align.go` (삭제) | `internal/align` 으로 옮겨간다 |
| `internal/diff/align_test.go` (삭제) | 같이 옮겨간다 |
| `internal/diff/compare.go` (수정) | 옮긴 심볼을 `align.` 로 부른다. `attrMap` 도 옮겨간다 |
| `internal/diff/compare_internal_test.go` (수정) | `attrMap` 테스트를 `align` 으로 넘기고 나머지는 새 이름으로 |
| `internal/tmpl/extract.go` (수정) | `diffStructure` 를 정렬로 대체, `unrepresented` 수집, 게이트 |
| `internal/tmpl/schema.go` (수정) | `Unrepresented` 타입과 `Schema.Unrepresented` 필드 |
| `internal/tmpl/tmpl_test.go` (수정) | T1·T2·T3 테스트 |
| `cmd/panto/cmd_tmpl.go` (수정) | `--allow-unrepresented` 플래그 |
| `cmd/panto/main.go` (수정) | `usage()` 문구 |
| `cmd/panto/main_test.go` (수정) | CLI 게이트 테스트, T5 |
| `README.md` (수정) | 쓰는 법 + 「다음 작업」 4번 |

---

## Task 1: 정렬기를 `internal/align` 으로 옮긴다

**Files:**
- Create: `internal/align/align.go`, `internal/align/align_test.go`
- Delete: `internal/diff/align.go`, `internal/diff/align_test.go`
- Modify: `internal/diff/compare.go`, `internal/diff/compare_internal_test.go`

**Interfaces:**
- Consumes: `xmlscan.Node`·`xmlscan.Tree`·`xmlscan.Attr`, `parts.StableAttrs`
- Produces (전부 `package align`):
  - `type Node struct { xmlscan.Node; Kids []*Node; Size int }` (비공개 필드 `sig string` 유지)
  - `func BuildTree(t *xmlscan.Tree) *Node`
  - `func Sign(n *Node)`
  - `func AttrMap(n xmlscan.Node) map[[2]string]string`
  - `type Op struct { Tag byte; AStart, AEnd, BStart, BEnd int }`
  - `func Siblings(a, b []*Node) ([]Op, bool)`
  - `const MaxCells = 4_000_000`

**배경:** `tmpl` 이 `diff` 를 import 하면 안 된다 — diff 설계 §8 이 그 반대 방향을 금지했고 논거가 대칭이다. 정렬기를 둘 다 볼 수 있는 곳으로 옮긴다.

**이것은 순수 이동이다.** `diff` 의 동작이 한 톨도 바뀌면 안 된다. **D2(`form 7/1` · `deck 13/12`)와 D1·D3 가 안전줄이다.**

**이름 대응표** (그대로 따르라):

| 옮기기 전 (`internal/diff`) | 옮긴 후 (`internal/align`) |
|---|---|
| `node` | `Node` |
| `node.kids` | `Node.Kids` |
| `node.size` | `Node.Size` |
| `node.sig` | `Node.sig` (**비공개 유지** — `align` 밖에서 쓸 일이 없다) |
| `buildTree` | `BuildTree` |
| `sign` | `Sign` |
| `stableAttrTriples` | `stableAttrTriples` (**비공개 유지**) |
| `attrMap` (`compare.go` 에 있음) | `AttrMap` |
| `op` | `Op` |
| `op.tag`·`aStart`·`aEnd`·`bStart`·`bEnd` | `Op.Tag`·`AStart`·`AEnd`·`BStart`·`BEnd` |
| `alignSiblings` | `Siblings` |
| `alignMiddle` | `alignMiddle` (**비공개 유지**) |
| `maxCells` | `MaxCells` |

- [ ] **Step 1: 새 패키지를 만들고 코드를 옮긴다**

`internal/align/align.go` 를 만들고 `internal/diff/align.go` 의 내용 전부와 `internal/diff/compare.go` 의 `attrMap` 함수(주석 포함)를 옮긴다. 패키지 선언은 `package align`.

**패키지 doc 주석을 파일 맨 위에 새로 쓴다** (한국어):

```go
// Package align 은 두 XML 트리를 형제 목록 단위로 정렬한다.
//
// diff 와 tmpl 이 **같은 규칙으로** 짝을 지어야 하기 때문에 여기 있다 —
// 한쪽이 다른 쪽을 import 하면 나중에 어느 쪽이 규칙의 주인인지 알 수 없게
// 된다(diff 설계 §8 의 논거가 대칭으로 성립한다).
//
// 여기에는 재직렬화 함수가 없다. xmlscan 과 같은 이유다 — XML 트리를 바이트로
// 되돌리는 경로가 존재하면 무손실이 깨진다.
package align
```

이름은 위 대응표대로 바꾼다. **주석은 한 글자도 바꾸지 마라** — 그 안의 설계 근거가 이 코드의 값어치다. 다만 주석 안에서 옛 심볼 이름을 부르는 곳(`alignSiblings`, `maxCells`, `attrMap` 등)은 새 이름으로 고친다.

`internal/diff/align.go` 를 삭제한다.

- [ ] **Step 2: `internal/diff` 가 새 이름을 부르게 고친다**

`internal/diff/compare.go` 에서:
- import 에 `"github.com/SONGYEONGSIN/pantograph/internal/align"` 을 더한다
- `attrMap` 함수 정의를 **삭제**한다(이미 옮겼다)
- `buildTree`→`align.BuildTree`, `sign`→`align.Sign`, `*node`→`*align.Node`, `alignSiblings`→`align.Siblings`, `attrMap`→`align.AttrMap`
- `x.kids`→`x.Kids`, `n.size`→`n.Size`
- `o.tag`→`o.Tag`, `o.aStart`→`o.AStart`, `o.aEnd`→`o.AEnd`, `o.bStart`→`o.BStart`, `o.bEnd`→`o.BEnd`
- `maxCells` 를 참조하는 곳이 있으면 `align.MaxCells`

`compare.go` 에서 `parts` import 가 안 쓰이게 됐는지 확인한다 — `attrMap` 이 유일한 사용처였다면 지운다. **`go build` 가 알려준다.**

- [ ] **Step 3: 테스트를 옮긴다**

`internal/diff/align_test.go` **전체**를 `internal/align/align_test.go` 로 옮긴다(`package align`). 그 안의 헬퍼(`scanReal`·`mkSigs`·`opsString`·`itoa`)와 테스트 4개(`TestBuildTreeCoversEveryNodeExactlyOnce`·`TestSignIgnoresVolatileAttrs`·`TestSignDistinguishesTextAndShape`·`TestAlignSiblings`·`TestAlignSiblingsCommonPrefixSuffixIsFree`·`TestAlignSiblingsMidListPureInsertion`·`TestAlignSiblingsCapFallsBackAndSaysSo`)가 전부 간다. 이름을 대응표대로 고친다.

`internal/diff/compare_internal_test.go` 에서 **`TestAttrMapKeepsNamespaceCollidingLocalNames` 만** `internal/align/align_test.go` 로 옮긴다 — 그것이 시험하는 `attrMap` 이 옮겨갔기 때문이다. **나머지 테스트는 `compare.go` 의 함수를 시험하므로 그대로 둔다**(`TestCompareAttrsIgnoresNamespaceDeclarations`·`TestCompareTreesPreservesTrailingWhitespace`·`TestCompareTreesAlignsAcrossDeletionAndFindsDownstreamText`·`TestAlignChildrenAsymmetricReplaceTail`·`TestCapExceededProducesStructureNotSilentPositionalFallback`·`TestComparePairTypeMismatchDisclosesDiscardedWeight`).

남는 테스트들에서 옛 심볼을 부르는 곳을 새 이름으로 고친다 — 특히 `TestCapExceededProducesStructureNotSilentPositionalFallback` 이 `maxCells` 를 참조한다.

- [ ] **Step 4: 빌드와 전체 스위트**

```
/opt/homebrew/bin/go build ./... && /opt/homebrew/bin/gofmt -l cmd internal && /opt/homebrew/bin/go vet ./... && /opt/homebrew/bin/go test ./... -count=1
```

기대: 9개 패키지(신규 `internal/align` 포함) 전부 통과.

**`TestD2FullCounts` 가 form `Total 7 / VolatileOnly 1`, deck `Total 13 / VolatileOnly 12` 로 통과해야 한다.** 하나라도 다르면 이동이 순수하지 않았다는 뜻이니 **기대값을 고치지 말고 무엇이 달라졌는지 찾아라.**

- [ ] **Step 5: 순환 의존이 없는지 확인한다**

```bash
/opt/homebrew/bin/go list -deps ./internal/align | grep pantograph
```

기대: `internal/opc`·`internal/parts`·`internal/xmlscan` 만 나오고 **`internal/diff` 나 `internal/tmpl` 은 없어야 한다.**

- [ ] **Step 6: 커밋**

```bash
git add -A
git commit -m "refactor: 정렬기를 internal/align 으로 옮긴다

tmpl 이 diff 를 import 하면 diff 설계 §8 이 금지한 방향의 대칭이 된다.
둘 다 볼 수 있는 곳으로 옮겨 같은 규칙으로 짝을 짓게 한다.

순수 이동 + export 다 — diff 의 동작은 한 톨도 안 바뀐다. D2 가 안전줄이다."
```

---

## Task 2: `align.Match` — 짝짓기와 미매칭 서브트리

**Files:**
- Create: `internal/align/match.go`, `internal/align/match_test.go`

**Interfaces:**
- Consumes: Task 1 의 `Node`·`Op`·`Siblings`
- Produces:
  - `type Pair struct { A, B *Node }`
  - `func Match(a, b *Node) (pairs []Pair, onlyA, onlyB []*Node)`

**배경:** `diff` 는 짝을 만들며 곧바로 항목을 뱉지만(`alignChildren`), `tmpl` 은 **짝 목록을 받아 나중에 판단**해야 한다 — "이 노드가 **모든** 문서에서 매칭됐는가" 는 문서를 다 본 뒤에야 답할 수 있기 때문이다.

**짝짓기 규칙은 `diff` 의 `alignChildren` 과 같아야 한다** (설계 §5):

| 구간 | tmpl 에서 |
|---|---|
| `'e'` 서브트리가 통째로 같다 | **매칭** |
| `'r'` 양쪽에 남아 위치로 짝지어졌다 | **매칭** — 가변 키는 전부 여기서 나온다 |
| `'i'`·`'d'` 한쪽에만 있다 | 매칭 아님 |

**`'e'` 구간도 내려가서 쌍을 만든다.** `diff` 는 볼 게 없어서 안 내려가지만, `tmpl` 은 "모든 문서에서 매칭됐는가" 를 알아야 한다 — doc1 에서 `'e'` 이고 doc2 에서 `'r'` 인 노드는 **둘 다에서 매칭된 것**이고 키 후보다. `'e'` 서브트리는 양쪽 모양이 같으므로 나란히 내려가면 된다.

- [ ] **Step 1: 실패하는 테스트를 쓴다**

`internal/align/match_test.go` (`package align`):

```go
package align

import (
	"testing"

	"github.com/SONGYEONGSIN/pantograph/internal/xmlscan"
)

// tree 는 (경로, 타입, 텍스트) 목록으로 정렬용 트리를 만든다.
// Span 은 부모·자식 관계를 만드는 재료이므로 실제 값을 준다 — BuildTree 가
// Span 포함으로 트리를 세우기 때문에 0 으로 두면 전부 한 노드로 뭉친다.
func tree(spec ...[3]string) *Node {
	nodes := make([]xmlscan.Node, len(spec))
	for i, s := range spec {
		nodes[i] = xmlscan.Node{Path: s[0], Type: s[1], Text: s[2]}
	}
	// Span 은 뒤에서 앞으로 채운다 — 각 노드는 **경로가 자기 밑에 있는**
	// 뒤쪽 연속 노드들을 품는다. 이 계산은 손으로 검산했다: body/p[1]/t[1],
	// body/p[2]/t[1] 같은 형제 구조에서 BuildTree 가 정확히 두 문단을 만든다.
	for i := len(nodes) - 1; i >= 0; i-- {
		end := i*10 + 10
		for j := i + 1; j < len(nodes); j++ {
			if len(nodes[j].Path) > len(nodes[i].Path) &&
				nodes[j].Path[:len(nodes[i].Path)+1] == nodes[i].Path+"/" {
				end = j*10 + 10
			} else {
				break
			}
		}
		nodes[i].Span = xmlscan.Span{Start: i * 10, End: end}
	}
	n := BuildTree(&xmlscan.Tree{Nodes: nodes})
	Sign(n)
	return n
}

// paths 는 노드 목록의 경로를 뽑는다. 기대값과 대조하기 위한 것이다.
func paths(ns []*Node) []string {
	out := make([]string, len(ns))
	for i, n := range ns {
		out[i] = n.Path
	}
	return out
}

// TestMatchPairsEqualAndReplace 는 'e'(통째로 같음)와 'r'(위치 짝짓기) **둘 다**
// 매칭으로 다루는지 본다.
//
// 'r' 을 빼면 tmpl 의 가변 키가 하나도 안 나온다 — 텍스트가 다른 문단은 서브트리
// 해시가 달라 'e' 가 아니라 'r' 구간에 들어가기 때문이다(설계 §5).
// 'e' 를 안 내려가면 "모든 문서에서 매칭됐는가" 를 답할 수 없다.
func TestMatchPairsEqualAndReplace(t *testing.T) {
	a := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "같음"},
		[3]string{"body/p[2]", "p", ""},
		[3]string{"body/p[2]/t[1]", "t", "기대"},
	)
	b := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "같음"},
		[3]string{"body/p[2]", "p", ""},
		[3]string{"body/p[2]/t[1]", "t", "실제"},
	)
	pairs, onlyA, onlyB := Match(a, b)
	if len(onlyA) != 0 || len(onlyB) != 0 {
		t.Fatalf("구조가 같은데 한쪽에만 있는 것이 나왔다: A=%v B=%v", paths(onlyA), paths(onlyB))
	}
	// 노드 5개가 전부 짝지어져야 한다 — 'e' 서브트리(p[1]) 안쪽도 포함이다.
	if len(pairs) != 5 {
		t.Fatalf("짝이 %d개 (기대 5): %+v", len(pairs), pairs)
	}
	got := map[string]string{}
	for _, p := range pairs {
		got[p.A.Path] = p.B.Path
	}
	for _, want := range []string{"body", "body/p[1]", "body/p[1]/t[1]", "body/p[2]", "body/p[2]/t[1]"} {
		if got[want] != want {
			t.Errorf("%s 가 %q 와 짝지어졌다 (기대 자기 자신)", want, got[want])
		}
	}
}

// TestMatchReportsOnlySideSubtrees 는 한쪽에만 있는 것을 **서브트리 단위로**
// 내는지 본다. 노드마다 내면 문단 하나에 여러 건이 된다.
func TestMatchReportsOnlySideSubtrees(t *testing.T) {
	a := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "하나"},
	)
	b := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "하나"},
		[3]string{"body/p[2]", "p", ""},
		[3]string{"body/p[2]/t[1]", "t", "새로"},
	)
	pairs, onlyA, onlyB := Match(a, b)
	if len(onlyA) != 0 {
		t.Fatalf("기대에만 있는 것이 나왔다: %v", paths(onlyA))
	}
	if len(onlyB) != 1 {
		t.Fatalf("실제에만 있는 서브트리가 %d개 (기대 1 — 문단 하나): %v", len(onlyB), paths(onlyB))
	}
	if onlyB[0].Path != "body/p[2]" {
		t.Fatalf("경로가 %q (기대 body/p[2])", onlyB[0].Path)
	}
	if onlyB[0].Size != 2 {
		t.Fatalf("서브트리 노드 수가 %d (기대 2 — p 와 t)", onlyB[0].Size)
	}
	if len(pairs) != 3 {
		t.Fatalf("짝이 %d개 (기대 3 — body, p[1], t[1]): %v", len(pairs), pairs)
	}
}

// TestMatchStopsAtTypeMismatch 는 타입이 다른 짝의 **안쪽은 짝짓지 않는지** 본다.
// diff 의 comparePair 와 같은 규칙이다 — 서로 다른 요소의 자식을 비교하면
// 뜻이 없다.
func TestMatchStopsAtTypeMismatch(t *testing.T) {
	a := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/p[1]", "p", ""},
		[3]string{"body/p[1]/t[1]", "t", "안쪽"},
	)
	b := tree(
		[3]string{"body", "body", ""},
		[3]string{"body/tbl[1]", "tbl", ""},
		[3]string{"body/tbl[1]/t[1]", "t", "안쪽"},
	)
	pairs, _, _ := Match(a, b)
	// body 와 (p ↔ tbl) 둘만 짝지어져야 한다 — 그 안쪽은 안 본다.
	if len(pairs) != 2 {
		t.Fatalf("짝이 %d개 (기대 2 — 루트와 타입 불일치 쌍까지): %+v", len(pairs), pairs)
	}
}
```

- [ ] **Step 2: 실패를 확인한다**

```
/opt/homebrew/bin/go test ./internal/align/ -run TestMatch -count=1
```

기대: 컴파일 실패 — `undefined: Match`, `undefined: Pair`

- [ ] **Step 3: `internal/align/match.go` 를 만든다**

```go
package align

// Pair 는 정렬이 짝지은 두 노드다. A 는 첫 트리, B 는 둘째 트리의 것이다.
type Pair struct{ A, B *Node }

// Match 는 두 트리를 정렬해 짝지어진 노드 쌍과 한쪽에만 있는 서브트리를 낸다.
//
// **짝짓기 규칙은 diff 의 alignChildren 과 같아야 한다** — 'e'(서브트리가 통째로
// 같다)와 'r'(양쪽에 남아 위치로 짝지어졌다)이 매칭이고, 'i'·'d' 는 한쪽에만
// 있는 것이다. 두 곳이 갈라지면 diff 와 tmpl 이 같은 문서에 다른 답을 낸다 —
// tmpl 설계 T5 가 그것을 테스트로 잠근다.
//
// **'e' 구간도 내려가서 쌍을 만든다.** diff 는 볼 게 없어서 안 내려가지만
// tmpl 은 "이 노드가 **모든** 문서에서 매칭됐는가" 를 알아야 한다 — 어떤
// 문서에서는 'e' 이고 다른 문서에서는 'r' 인 노드는 둘 다에서 매칭된 것이고
// 키 후보다. 'e' 서브트리는 양쪽 모양이 같으므로 나란히 내려가면 된다.
//
// 타입이 다른 짝은 그 안쪽을 짝짓지 않는다 — 서로 다른 요소의 자식을 비교하면
// 뜻이 없다(diff 의 comparePair 와 같은 규칙).
//
// 상한 초과로 정렬을 포기한 형제 목록도 위치로 짝지어진다 — Siblings 가 그때
// 'r' 구간 하나를 내기 때문이다. 호출자가 그 사실을 알아야 하면 Siblings 를
// 직접 부르라.
func Match(a, b *Node) (pairs []Pair, onlyA, onlyB []*Node) {
	if a == nil || b == nil {
		if a != nil {
			onlyA = append(onlyA, a)
		}
		if b != nil {
			onlyB = append(onlyB, b)
		}
		return pairs, onlyA, onlyB
	}

	var walk func(x, y *Node)
	walk = func(x, y *Node) {
		pairs = append(pairs, Pair{A: x, B: y})
		if x.Type != y.Type {
			return
		}
		ops, _ := Siblings(x.Kids, y.Kids)
		for _, o := range ops {
			switch o.Tag {
			case 'e', 'r':
				la, lb := o.AEnd-o.AStart, o.BEnd-o.BStart
				m := la
				if lb < m {
					m = lb
				}
				for k := 0; k < m; k++ {
					walk(x.Kids[o.AStart+k], y.Kids[o.BStart+k])
				}
				// 'e' 는 길이가 같아 꼬리가 없다. 'r' 은 남을 수 있다.
				for i := o.AStart + m; i < o.AEnd; i++ {
					onlyA = append(onlyA, x.Kids[i])
				}
				for j := o.BStart + m; j < o.BEnd; j++ {
					onlyB = append(onlyB, y.Kids[j])
				}
			case 'i':
				for j := o.BStart; j < o.BEnd; j++ {
					onlyB = append(onlyB, y.Kids[j])
				}
			case 'd':
				for i := o.AStart; i < o.AEnd; i++ {
					onlyA = append(onlyA, x.Kids[i])
				}
			}
		}
	}
	walk(a, b)
	return pairs, onlyA, onlyB
}
```

- [ ] **Step 4: 테스트 통과를 확인한다**

```
/opt/homebrew/bin/go test ./internal/align/ -count=1 -v -run TestMatch
```

기대: 3개 전부 PASS

- [ ] **Step 5: 정적 검사와 커밋**

```bash
/opt/homebrew/bin/gofmt -l cmd internal && /opt/homebrew/bin/go vet ./... && /opt/homebrew/bin/go test ./... -count=1
git add internal/align/
git commit -m "feat: align.Match — 짝짓기와 한쪽에만 있는 서브트리

diff 는 짝을 만들며 곧바로 항목을 뱉지만 tmpl 은 짝 목록을 받아 나중에
판단해야 한다 — '이 노드가 모든 문서에서 매칭됐는가' 는 문서를 다 본 뒤에야
답할 수 있다.

'e' 구간도 내려가서 쌍을 만든다. diff 는 볼 게 없어서 안 내려가지만 tmpl 은
어떤 문서에서 'e' 이고 다른 문서에서 'r' 인 노드를 둘 다 매칭으로 봐야 한다."
```

---

## Task 3: `Extract` 를 정렬 기반으로 + `unrepresented` + 기본 거절

**Files:**
- Modify: `internal/tmpl/schema.go` (`Unrepresented` 타입, `Schema` 필드)
- Modify: `internal/tmpl/extract.go` (`Extract` 시그니처, 2단계 교체, `diffStructure` 삭제, `diffMarkup` 시그니처)
- Modify: `internal/tmpl/tmpl_test.go` (T1·T2·T3)

**Interfaces:**
- Consumes: Task 1·2 의 `align.BuildTree`·`align.Sign`·`align.Match`·`align.Node`·`align.Pair`
- Produces:
  - `type Unrepresented struct { Doc, Part, Path, Side string; Nodes int }`
  - `Schema.Unrepresented []Unrepresented` (`json:"unrepresented,omitempty"`)
  - `func Extract(pkgs []*opc.Package, names []string, allowUnrepresented bool) (*opc.Package, *Schema, []patch.Error, error)` — **인자가 하나 늘었다**

**배경:** 이 태스크가 동작을 바꾼다. 설계 §5 의 규칙 셋을 그대로 구현한다.

- [ ] **Step 1: 실패하는 테스트를 쓴다 (T2 — 기본 거절)**

`internal/tmpl/tmpl_test.go` 끝에 붙인다. `testutil.MinimalDocx` 가 문단 목록으로 결정론적 docx 를 만든다.

```go
// TestT2StructuralDifferenceIsRejectedByDefault 는 구조가 다른 문서를 플래그
// 없이 주면 거절하는지 본다.
//
// 정렬이 들어오면 Extract 가 구조 차이에도 **성공**하게 되는데, 그 성공은
// "이 템플릿은 입력 중 일부를 재현하지 못한다"는 단서가 붙은 성공이다.
// 단서는 무시할 수 있지만 실패는 무시할 수 없다 — 그래서 기본은 거절이다
// (설계 §3).
func TestT2StructuralDifferenceIsRejectedByDefault(t *testing.T) {
	a := testutil.MinimalDocx([]string{"첫 줄", "셋째 줄"})
	b := testutil.MinimalDocx([]string{"첫 줄", "새로 낀 줄", "셋째 줄"})
	pa, err := opc.OpenBytes(a)
	if err != nil {
		t.Fatalf("OpenBytes a: %v", err)
	}
	pb, err := opc.OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes b: %v", err)
	}
	_, _, errs, err := tmpl.Extract([]*opc.Package{pa, pb}, []string{"a.docx", "b.docx"}, false)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) != 1 {
		t.Fatalf("거절 항목이 %d개 (기대 1): %+v", len(errs), errs)
	}
	if errs[0].Reason != "unrepresented_structure" {
		t.Fatalf("reason=%q (기대 unrepresented_structure)", errs[0].Reason)
	}
	if errs[0].Detail == "" {
		t.Fatal("detail 이 비었다 — 무엇을 표현 못 하는지 말해야 한다")
	}
}

// TestT3AllowFlagExtractsCommonPartAndReportsRest 는 플래그를 주면 공통부에서
// 키를 뽑고 매칭 안 된 서브트리를 빠짐없이 신고하는지 본다.
func TestT3AllowFlagExtractsCommonPartAndReportsRest(t *testing.T) {
	// base 에 2문단, 다른 문서에 3문단(가운데 하나 삽입) + 마지막 문단 텍스트 변경.
	// 공통부의 가변 키는 "셋째 줄"↔"셋째 줄!" 하나여야 하고,
	// 매칭 안 된 서브트리는 b 에만 있는 문단 하나여야 한다.
	a := testutil.MinimalDocx([]string{"첫 줄", "셋째 줄"})
	b := testutil.MinimalDocx([]string{"첫 줄", "새로 낀 줄", "셋째 줄!"})
	pa, err := opc.OpenBytes(a)
	if err != nil {
		t.Fatalf("OpenBytes a: %v", err)
	}
	pb, err := opc.OpenBytes(b)
	if err != nil {
		t.Fatalf("OpenBytes b: %v", err)
	}
	_, sch, errs, err := tmpl.Extract([]*opc.Package{pa, pb}, []string{"a.docx", "b.docx"}, true)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("플래그를 줬는데 거절됐다: %+v", errs)
	}
	if len(sch.Unrepresented) != 1 {
		t.Fatalf("unrepresented 가 %d건 (기대 1 — b 에만 있는 문단 하나): %+v",
			len(sch.Unrepresented), sch.Unrepresented)
	}
	u := sch.Unrepresented[0]
	if u.Doc != "b.docx" {
		t.Errorf("doc=%q (기대 b.docx)", u.Doc)
	}
	if u.Part != "word/document.xml" {
		t.Errorf("part=%q", u.Part)
	}
	if u.Nodes == 0 {
		t.Error("nodes 가 0 이다 — 서브트리 무게를 말해야 한다")
	}
	// 공통부에서 키가 나와야 한다 — "셋째 줄" 이 "셋째 줄!" 로 바뀐 자리.
	if len(sch.Keys) != 1 {
		t.Fatalf("키가 %d개 (기대 1): %+v", len(sch.Keys), sch.Keys)
	}
	if sch.Keys[0].Samples[0] != "셋째 줄" || sch.Keys[0].Samples[1] != "셋째 줄!" {
		t.Fatalf("샘플이 %v (기대 [셋째 줄 셋째 줄!])", sch.Keys[0].Samples)
	}
}
```

`tmpl_test.go` 의 import 에 `testutil`·`opc` 가 이미 있는지 확인한다(있을 것이다 — 기존 테스트가 쓴다).

- [ ] **Step 2: 실패를 확인한다**

```
/opt/homebrew/bin/go test ./internal/tmpl/ -run 'TestT2|TestT3' -count=1
```

기대: 컴파일 실패 — `too many arguments in call to tmpl.Extract` (인자가 3개인데 4개를 줬다). 그 다음 단계에서 시그니처를 바꾸면 `sch.Unrepresented undefined` 로 바뀐다. **두 실패를 다 보고 보고서에 남겨라.**

- [ ] **Step 3: `schema.go` 에 타입을 더한다**

```go
// Unrepresented 는 템플릿이 표현하지 못하는 서브트리 하나다.
//
// 템플릿은 base 의 바이트에 setText 를 얹은 것이고 patch 의 연산은 setText 와
// replaceRaw 둘뿐이라, "이 문단은 어떤 문서에는 있고 어떤 문서에는 없다" 를
// 담을 수단이 없다(설계 §2). 그래서 그런 서브트리는 키 후보에서 빼고 여기
// 남긴다 — **도구가 자기가 못 하는 것을 스스로 말한다.**
type Unrepresented struct {
	Doc  string `json:"doc"`  // 어느 문서와 비교하다 나왔나
	Part string `json:"part"` // 물리 파트 경로
	Path string `json:"path"` // 그 서브트리의 루트 경로

	// Side 는 어느 쪽에만 있는지다. base 에만 있으면 템플릿에 남아 그 문서를
	// 재현할 때 **지워지지 않고**, 다른 문서에만 있으면 템플릿에 자리가 **없다**.
	// 두 실패 모양이 다르므로 구분해서 싣는다.
	Side string `json:"side"`

	Nodes int `json:"nodes"` // 서브트리의 노드 수 — 버려지는 무게
}
```

`Schema` 에 필드를 더한다 (`Keys` 아래):

```go
	// Unrepresented 는 이 템플릿이 표현하지 못하는 서브트리들이다.
	// **비어 있으면 필드 자체가 나오지 않는다** — 구조가 같은 문서에서 뽑은
	// 스키마는 이 슬라이스 이전과 바이트 동일해야 한다(설계 T1).
	Unrepresented []Unrepresented `json:"unrepresented,omitempty"`
```

- [ ] **Step 4: `extract.go` 를 고친다**

**시그니처**:

```go
func Extract(pkgs []*opc.Package, names []string, allowUnrepresented bool) (*opc.Package, *Schema, []patch.Error, error) {
```

**`diffStructure` 함수를 삭제한다** — 정렬이 대체한다.

**`diffMarkup` 의 시그니처를 바꾼다.** 옛 것은 인덱스로 다른 문서의 노드를 찾았는데(`trees[i].Nodes[idx]`), 정렬 이후에는 인덱스가 대응하지 않는다:

```go
// diffMarkup 은 짝지어진 노드들의 마크업이 같은지 본다.
// nodes[0] 이 base 이고 nodes[i] 가 names[i] 문서의 짝이다.
// 본문은 예전 그대로다 — 정렬은 "어느 노드끼리 비교할지" 만 바꾸지
// "무엇을 차이로 볼지" 는 안 바꾼다(설계 §5 규칙 2).
func diffMarkup(nodes []xmlscan.Node, names []string) *patch.Error {
	bn := nodes[0]
	baseAttrs := parts.StableAttrs(bn)
	for i := 1; i < len(nodes); i++ {
		other := nodes[i]
		// … 기존 본문 그대로. trees[i].Nodes[idx] 를 other 로 바꾸기만 한다 …
	}
	return nil
}
```

**2단계를 갈아끼운다.** 기존 `for _, pt := range basePlan { … }` 루프의 본문을 아래로 바꾼다:

```go
	var keys []Key
	var ops []patch.Op
	var unrep []Unrepresented

	for _, pt := range basePlan {
		baseTree, err := base.Tree(pt.Name)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("%s: %w", names[0], err)
		}
		baseRoot := align.BuildTree(baseTree)
		if baseRoot == nil {
			continue // 노드가 없는 파트 — 볼 것이 없다
		}
		align.Sign(baseRoot)

		// 문서마다 base 와 짝짓는다. Extract 는 base 대 각 문서를 따로 보므로
		// 어떤 노드는 doc1 과는 매칭되고 doc2 와는 안 될 수 있다.
		matched := make([]map[*align.Node]*align.Node, len(docs))
		for i := 1; i < len(docs); i++ {
			tr, err := docs[i].Tree(pt.Name)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("%s: %w", names[i], err)
			}
			root := align.BuildTree(tr)
			align.Sign(root)

			pairs, onlyA, onlyB := align.Match(baseRoot, root)
			m := make(map[*align.Node]*align.Node, len(pairs))
			for _, p := range pairs {
				m[p.A] = p.B
			}
			matched[i] = m

			for _, n := range onlyA {
				unrep = append(unrep, Unrepresented{
					Doc: names[i], Part: pt.Name, Path: n.Path,
					Side: names[0] + " 에만", Nodes: n.Size})
			}
			for _, n := range onlyB {
				unrep = append(unrep, Unrepresented{
					Doc: names[i], Part: pt.Name, Path: n.Path,
					Side: names[i] + " 에만", Nodes: n.Size})
			}
		}

		// base 를 pre-order 로 돌며 후보를 판정한다.
		// **모든 문서에서 매칭된 노드만** 후보다 — 어느 문서의 값을 sample 로
		// 삼을지 정할 수 없으면 키가 될 수 없다(설계 §5 규칙 1).
		var walk func(n *align.Node) *patch.Error
		walk = func(n *align.Node) *patch.Error {
			nodes := make([]xmlscan.Node, len(docs))
			nodes[0] = n.Node
			all := true
			for i := 1; i < len(docs); i++ {
				o, ok := matched[i][n]
				if !ok {
					all = false
					break
				}
				nodes[i] = o.Node
			}
			if all {
				if e := diffMarkup(nodes, names); e != nil {
					return e
				}
				if n.Type == "t" {
					varies := false
					for i := 1; i < len(docs); i++ {
						if nodes[i].Text != n.Text {
							varies = true
							break
						}
					}
					if varies {
						key := "k" + strconv.Itoa(len(keys)+1)
						samples := make([]string, len(docs))
						for i := range nodes {
							samples[i] = nodes[i].Text
						}
						keys = append(keys, Key{Key: key, Part: pt.Name, Path: n.Path, Samples: samples})
						ops = append(ops, patch.Op{Op: "setText", Part: pt.Name,
							Path: n.Path, Text: "{{" + key + "}}"})
					}
				}
			}
			for _, k := range n.Kids {
				if e := walk(k); e != nil {
					return e
				}
			}
			return nil
		}
		if e := walk(baseRoot); e != nil {
			return nil, nil, []patch.Error{*e}, nil
		}
	}

	// 표현 못 하는 것이 있으면 기본은 거절이다 — 단서는 무시할 수 있지만
	// 실패는 무시할 수 없다(설계 §3).
	if len(unrep) > 0 && !allowUnrepresented {
		return nil, nil, []patch.Error{{
			Path:   unrep[0].Part,
			Reason: "unrepresented_structure",
			Detail: fmt.Sprintf("템플릿이 표현하지 못하는 서브트리가 %d개다 (처음: %s 의 %s, %s). "+
				"--allow-unrepresented 를 주면 공통부만 뽑고 나머지를 스키마에 신고한다",
				len(unrep), unrep[0].Doc, unrep[0].Path, unrep[0].Side),
		}}, nil
	}
```

3단계의 마지막 `return` 에 `Unrepresented` 를 싣는다:

```go
	return tp, &Schema{Base: names[0], Hash: pkgs[0].Hash, Keys: keys, Unrepresented: unrep}, nil, nil
```

`extract.go` 의 import 에 `"github.com/SONGYEONGSIN/pantograph/internal/align"` 을 더한다.

- [ ] **Step 5: 호출부를 고친다**

`Extract` 의 인자가 늘었으므로 부르는 곳을 전부 고친다. **호출부는 정확히 18곳이다**(내가 셌다) — `cmd/panto/cmd_tmpl.go` 1곳과 `internal/tmpl/tmpl_test.go` 17곳.

```
grep -c "tmpl.Extract(" cmd/panto/cmd_tmpl.go internal/tmpl/tmpl_test.go
```

**기존 호출은 전부 `false` 를 준다** — 구조가 같은 문서를 쓰므로 동작이 안 바뀐다. `cmd_tmpl.go` 는 Task 4 에서 플래그를 받게 되므로 여기서는 일단 `false` 를 넘겨 컴파일만 통과시킨다.

- [ ] **Step 6: T1(무회귀)을 확인한다**

```
/opt/homebrew/bin/go test ./... -count=1
```

기대: **기존 `tmpl` 테스트가 전부 그대로 통과.** 특히 `TestTemplateReversalBase`·`TestTemplateReversalReal`·`TestTemplateReversalOthersTextLevel`(I4a) 과 실제 픽스처로 키를 뽑는 테스트들.

**하나라도 깨지면 기대값을 고치지 마라** — 현재 픽스처는 전부 구조가 같으므로 정렬이 옛 인덱스 대응과 **같은 답**을 내야 한다. 다르면 정렬이나 매칭 규칙이 틀린 것이다. 특히 `'r'` 구간을 매칭으로 다루는지 확인하라(설계 §5) — 안 그러면 키가 전부 사라진다.

- [ ] **Step 7: T2·T3 통과를 확인하고 커밋**

```bash
/opt/homebrew/bin/go test ./internal/tmpl/ -count=1 -v -run 'TestT2|TestT3'
/opt/homebrew/bin/gofmt -l cmd internal && /opt/homebrew/bin/go vet ./... && /opt/homebrew/bin/go test ./... -count=1
git add internal/tmpl/
git commit -m "feat: Extract 를 정렬 기반으로 — 구조가 달라도 공통부를 뽑는다

diffStructure(경로 순열이 완전히 같아야 함)를 align.Match 로 갈아끼운다.
가변 키 후보는 **모든 문서에서 매칭된** base 노드뿐이고, 매칭 안 된
서브트리는 unrepresented 로 신고한다.

표현 못 하는 것이 있으면 기본은 거절이다 — ok:true 가 '이 템플릿이 입력
전부를 표현한다' 를 계속 뜻하게 하려는 것이다."
```

---

## Task 4: CLI 플래그, T5 정렬 합치, 문서

**Files:**
- Modify: `cmd/panto/cmd_tmpl.go` (`--allow-unrepresented`)
- Modify: `cmd/panto/main.go` (`usage()`)
- Modify: `cmd/panto/main_test.go` (CLI 게이트, T5)
- Modify: `README.md`

**Interfaces:**
- Consumes: Task 3 의 `tmpl.Extract(pkgs, names, allowUnrepresented)`, `Schema.Unrepresented`
- Produces: 없음

- [ ] **Step 1: 실패하는 테스트를 쓴다 (CLI 게이트 + T5)**

`cmd/panto/main_test.go` 끝에 붙인다:

```go
// twoDocxWithInsertion 은 문단 하나가 삽입된 docx 쌍을 만들어 경로를 돌려준다.
func twoDocxWithInsertion(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.docx")
	b := filepath.Join(dir, "b.docx")
	if err := os.WriteFile(a, testutil.MinimalDocx([]string{"첫 줄", "셋째 줄"}), 0o644); err != nil {
		t.Fatalf("a 쓰기: %v", err)
	}
	if err := os.WriteFile(b, testutil.MinimalDocx([]string{"첫 줄", "새로 낀 줄", "셋째 줄"}), 0o644); err != nil {
		t.Fatalf("b 쓰기: %v", err)
	}
	return a, b
}

// TestTmplExtractRejectsUnrepresentedByDefault 는 플래그 없이 구조가 다른
// 문서를 주면 CLI 가 거절하고 출력 파일을 만들지 않는지 본다.
func TestTmplExtractRejectsUnrepresentedByDefault(t *testing.T) {
	a, b := twoDocxWithInsertion(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "t.docx")
	schema := filepath.Join(dir, "s.json")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmpl([]string{"extract", a, b, "-o", out, "--schema", schema})
	})
	if code != exitInput {
		t.Fatalf("exit=%d (기대 %d), stdout=%s", code, exitInput, stdout)
	}
	if !strings.Contains(stdout, "unrepresented_structure") {
		t.Fatalf("stdout 에 unrepresented_structure 가 없다: %s", stdout)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("거절됐는데 템플릿 파일이 생겼다")
	}
	if _, err := os.Stat(schema); !os.IsNotExist(err) {
		t.Fatal("거절됐는데 스키마 파일이 생겼다")
	}
}

// TestTmplExtractAllowFlagWritesUnrepresented 는 플래그를 주면 진행하고
// 스키마에 unrepresented 가 실리는지 본다.
func TestTmplExtractAllowFlagWritesUnrepresented(t *testing.T) {
	a, b := twoDocxWithInsertion(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "t.docx")
	schema := filepath.Join(dir, "s.json")

	var code int
	stdout := captureStdout(t, func() {
		code = cmdTmpl([]string{"extract", a, b, "-o", out, "--schema", schema, "--allow-unrepresented"})
	})
	if code != exitOK {
		t.Fatalf("exit=%d (기대 0), stdout=%s", code, stdout)
	}
	raw, err := os.ReadFile(schema)
	if err != nil {
		t.Fatalf("스키마 읽기: %v", err)
	}
	if !strings.Contains(string(raw), "unrepresented") {
		t.Fatalf("스키마에 unrepresented 가 없다: %s", raw)
	}
}

// TestT5DiffAndTmplAgreeOnAlignment 는 같은 문서 쌍에서 diff 의
// inserted·deleted 경로 집합과 tmpl 의 unrepresented 경로 집합이 일치하는지 본다.
//
// 두 명령이 각자 재귀를 갖고 있어(diff 의 alignChildren, align.Match) 'r' 구간의
// 위치 짝짓기 규칙이 복제돼 있다. 갈라지는 날 같은 문서에 다른 답을 내는데,
// 그것이 바로 이 슬라이스가 없애려던 상태다(설계 T5).
func TestT5DiffAndTmplAgreeOnAlignment(t *testing.T) {
	a, b := twoDocxWithInsertion(t)
	dir := t.TempDir()
	schema := filepath.Join(dir, "s.json")

	diffOut := captureStdout(t, func() {
		if code := cmdDiff([]string{a, b}); code != exitOK {
			t.Fatalf("diff exit=%d", code)
		}
	})
	if code := cmdTmpl([]string{"extract", a, b,
		"-o", filepath.Join(dir, "t.docx"), "--schema", schema, "--allow-unrepresented"}); code != exitOK {
		t.Fatalf("tmpl extract exit=%d", code)
	}

	var rep struct {
		Diffs []struct{ Kind, Part, Path string } `json:"diffs"`
	}
	if err := json.Unmarshal([]byte(diffOut), &rep); err != nil {
		t.Fatalf("diff 출력 파싱: %v", err)
	}
	fromDiff := map[string]bool{}
	for _, d := range rep.Diffs {
		if d.Kind == "inserted" || d.Kind == "deleted" {
			fromDiff[d.Part+"|"+d.Path] = true
		}
	}

	raw, err := os.ReadFile(schema)
	if err != nil {
		t.Fatalf("스키마 읽기: %v", err)
	}
	var sch struct {
		Unrepresented []struct{ Part, Path string } `json:"unrepresented"`
	}
	if err := json.Unmarshal(raw, &sch); err != nil {
		t.Fatalf("스키마 파싱: %v", err)
	}
	fromTmpl := map[string]bool{}
	for _, u := range sch.Unrepresented {
		fromTmpl[u.Part+"|"+u.Path] = true
	}

	if len(fromDiff) == 0 {
		t.Fatal("diff 가 inserted/deleted 를 하나도 안 냈다 — 이 쌍은 구조가 다르다")
	}
	if len(fromDiff) != len(fromTmpl) {
		t.Fatalf("경로 집합 크기가 다르다 — diff %d, tmpl %d\n  diff=%v\n  tmpl=%v",
			len(fromDiff), len(fromTmpl), fromDiff, fromTmpl)
	}
	for k := range fromDiff {
		if !fromTmpl[k] {
			t.Errorf("diff 에만 있는 경로: %s", k)
		}
	}
}
```

**`main_test.go` 는 `"encoding/json"` 을 import 하지 않는다**(확인했다 — `cmd_tmpl.go` 와 `main.go` 만 쓴다). T5 가 JSON 을 파싱하므로 **더해야 한다.**

- [ ] **Step 2: 실패를 확인한다**

```
/opt/homebrew/bin/go test ./cmd/panto/ -run 'TestTmplExtractRejects|TestTmplExtractAllow|TestT5' -count=1
```

기대: `--allow-unrepresented` 를 모르는 인자로 취급해 입력 파일로 오해하거나, `cmdTmpl` 이 `Extract` 를 3인자로 불러 컴파일 실패. **실패 전문을 보고서에 남겨라.**

- [ ] **Step 3: `cmd_tmpl.go` 에 플래그를 더한다**

`cmdTmplExtract` 의 인자 파싱 `switch` 에 분기를 더한다:

```go
		case "--allow-unrepresented":
			allowUnrepresented = true
```

`var inputs []string` 옆에 `var allowUnrepresented bool` 을 선언하고, `tmpl.Extract` 호출에 넘긴다:

```go
	tp, sch, errs, err := tmpl.Extract(pkgs, names, allowUnrepresented)
```

**주석을 남겨라** — 왜 기본이 거절인지:

```go
		// --allow-unrepresented 는 "이 템플릿이 입력 전부를 표현하지는 못한다" 를
		// 사용자가 명시적으로 사는 자리다. 기본이 거절인 이유는 단서는 무시할 수
		// 있지만 실패는 무시할 수 없기 때문이다 (설계 §3).
```

- [ ] **Step 4: `usage()` 를 고친다**

`cmd/panto/main.go` 의 `usage()` 에서 `tmpl extract` 줄을 고친다:

```
  panto tmpl extract <a> <b> [...] -o <tmpl> --schema <schema.json> [--allow-unrepresented]
```

- [ ] **Step 5: 테스트 통과를 확인한다**

```
/opt/homebrew/bin/go test ./cmd/panto/ -count=1 -v -run 'TestTmplExtract|TestT5'
```

기대: 3개 전부 PASS.

**T5 가 실패하면 두 재귀가 갈라졌다는 뜻이다.** `diff` 의 `alignChildren` 과 `align.Match` 의 `'r'` 분기를 나란히 놓고 대조하라 — 어느 쪽이 맞는지는 설계 §5 가 정한다.

- [ ] **Step 6: 손으로 돌려본다**

```bash
/opt/homebrew/bin/go build -o /tmp/panto ./cmd/panto
/tmp/panto tmpl extract testdata/real/form-a.docx testdata/real/form-b.docx -o /tmp/t.docx --schema /tmp/s.json
python3 -c "import json;print(json.load(open('/tmp/s.json')).keys())"
/tmp/panto
```

확인할 것: 실제 픽스처(구조가 같다)에서 **`unrepresented` 키가 스키마에 아예 없다**(`omitempty`). 사용법에 `--allow-unrepresented` 가 보인다.

- [ ] **Step 7: README 를 고친다**

`### 쓰는 법` 코드 블록의 `tmpl extract` 줄을 고친다:

```
panto tmpl extract <a> <b> [...] -o <tmpl> --schema <schema.json> [--allow-unrepresented]
```

그 아래에 문단을 더한다:

```markdown
`tmpl extract`는 문서들의 **구조가 달라도** 공통부에서 템플릿을 뽑는다. 다만 템플릿은 "이 자리의 텍스트가 다르다"만 표현할 수 있고 "이 문단은 어떤 문서엔 있고 어떤 문서엔 없다"는 담지 못한다 — 그런 서브트리가 하나라도 있으면 **기본은 거절**이고, `--allow-unrepresented`를 주면 공통부만 뽑고 나머지를 스키마의 `unrepresented`에 신고한다. `ok:true`는 "이 템플릿이 입력 전부를 표현한다"를 뜻한다.
```

`## 다음 작업` 4번을 고친다 — 이제 끝났다:

```markdown
4. **선택적 블록** — 구조가 다른 문서 간 템플릿 추출은 공통부까지 됐다(`--allow-unrepresented`). "이 문단은 어떤 문서엔 있고 어떤 문서엔 없다"를 담으려면 `patch`에 insert/delete 연산이 필요하다
```

`## 알려진 한계` 에 한 줄을 더한다:

```markdown
- **템플릿은 항상 base의 구조를 낸다.** `--allow-unrepresented`로 뽑은 템플릿을 채우면 base의 문단 구성이 나온다 — 스키마의 `unrepresented`가 무엇이 빠지는지 말한다 (tmpl 정렬 spec §8)
```

- [ ] **Step 8: 전체 검증과 커밋**

```bash
/opt/homebrew/bin/gofmt -l cmd internal && /opt/homebrew/bin/go vet ./... && /opt/homebrew/bin/go test ./... -count=1
git add -A
git commit -m "feat: --allow-unrepresented 플래그와 T5 정렬 합치

플래그가 '이 템플릿이 입력 전부를 표현하지는 못한다' 를 사용자가 명시적으로
사는 자리다.

T5 는 diff 와 tmpl 이 같은 문서 쌍에서 같은 짝짓기를 하는지 잠근다 — 두
재귀가 'r' 구간의 위치 짝짓기를 각자 갖고 있어 갈라질 수 있다."
```

---

## 자체 리뷰

**1. 설계 커버리지**

| 설계 절 | 태스크 |
|---|---|
| §2 공통부에서만 키, 나머지는 `unrepresented` | Task 3 |
| §3 기본 거절 + `--allow-unrepresented` | Task 3 (엔진), Task 4 (CLI) |
| §4 `internal/align` 이동, `attrMap` 포함 | Task 1 |
| §5 "매칭됐다" = `'e'` + `'r'` | Task 2 (`Match`), Task 2 Step 1 의 첫 테스트가 잠근다 |
| §5 규칙 1 모든 문서에서 매칭 | Task 3 (`all` 판정) |
| §5 규칙 2 `diffMarkup` 유지 | Task 3 (시그니처만 바뀐다) |
| §5 규칙 3 `diffPartSet` 불변 | Task 3 (손대지 않는다) |
| §6 스키마 `unrepresented` + `omitempty` | Task 3 (타입), Task 4 Step 6 (실측 확인) |
| §6 `Fill` 불변 | 어느 태스크도 `fill.go` 를 안 건드린다 |
| §7 T1 | Task 3 Step 6 |
| §7 T2·T3 | Task 3 Step 1 |
| §7 T4 | Task 1 Step 4 |
| §7 T5 | Task 4 Step 1 |
| §8·§9 한계·범위 밖 | Task 4 Step 7 (README) |

**빠진 것 하나**: 설계 §8 두 번째 한계 — "어느 문서의 값을 sample 로 쓸지 정할 수 없는 노드는 조용히 키에서 빠지고 `unrepresented` 에도 안 들어간다(문서 3벌 이상에서만 생긴다)". **이 계획에 문서 3벌짜리 테스트가 없다.** 2벌만 시험하면 그 경로가 안 돈다.

**보완**: Task 3 Step 1 에 아래를 더한다.

```go
// TestT3ThreeDocsDropsPartiallyMatchedNodes 는 문서 3벌에서 어떤 문서와는
// 매칭되고 다른 문서와는 안 되는 노드가 **키에서 빠지는지** 본다.
//
// 설계 §5 규칙 1: 어느 문서의 값을 sample 로 삼을지 정할 수 없으면 키가 될 수
// 없다. 이 노드는 unrepresented 에도 안 들어간다 — 매칭 자체는 됐기 때문이다
// (설계 §8). 그 침묵이 의도된 것임을 이 테스트가 고정한다.
func TestT3ThreeDocsDropsPartiallyMatchedNodes(t *testing.T) {
	// a: [X, Y]   b: [X, Y']  c: [X, 삽입, Y'']
	// Y 는 b 와는 'r' 로 매칭되지만 c 와는 어떻게 되는지가 정렬에 달렸다.
	// 무엇이 나오든 **키의 samples 길이는 항상 문서 수와 같아야** 하고,
	// 부분 매칭 노드가 키에 들어가면 안 된다.
	a := testutil.MinimalDocx([]string{"머리", "꼬리"})
	b := testutil.MinimalDocx([]string{"머리", "꼬리B"})
	c := testutil.MinimalDocx([]string{"머리", "삽입", "꼬리C"})
	var pkgs []*opc.Package
	for _, raw := range [][]byte{a, b, c} {
		p, err := opc.OpenBytes(raw)
		if err != nil {
			t.Fatalf("OpenBytes: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	_, sch, errs, err := tmpl.Extract(pkgs, []string{"a.docx", "b.docx", "c.docx"}, true)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(errs) > 0 {
		t.Fatalf("플래그를 줬는데 거절됐다: %+v", errs)
	}
	for _, k := range sch.Keys {
		if len(k.Samples) != 3 {
			t.Fatalf("키 %s 의 samples 가 %d개 (기대 3 — 문서 수만큼): %+v",
				k.Key, len(k.Samples), k)
		}
		for i, s := range k.Samples {
			if s == "" && i > 0 {
				t.Fatalf("키 %s 의 %d번째 sample 이 비었다 — 매칭 안 된 문서가 섞였다", k.Key, i)
			}
		}
	}
}
```

이 테스트는 **정확한 키 개수를 못 박지 않는다** — 3벌 정렬 결과가 어떻게 나오는지 내가 실행해 보지 않았기 때문이다. 대신 **불변식**(samples 길이가 항상 문서 수와 같고 빈 sample 이 없다)을 건다. 구현자는 실제로 몇 개가 나오는지 관찰해 보고서에 적고, **그 수가 나오는 이유를 설명할 수 있어야 한다.**

**2. 플레이스홀더 점검**

"TBD"·"적절히"·"비슷하게" 없음. Task 3 Step 4 의 `diffMarkup` 본문을 "기존 본문 그대로" 라고 쓴 곳이 있는데, 그것은 플레이스홀더가 아니라 **바꾸지 말라는 지시**다 — 시그니처만 바뀌고 판정 로직은 그대로여야 한다(설계 §5 규칙 2).

**3. 타입 일관성**

- `align.Node{xmlscan.Node; Kids []*Node; Size int}` — Task 1 정의, Task 2·3 사용 ✅
- `align.Pair{A, B *Node}` — Task 2 정의, Task 3 사용 ✅
- `align.Match(a, b *Node) ([]Pair, []*Node, []*Node)` — Task 2 정의, Task 3 사용 ✅
- `align.Siblings(a, b []*Node) ([]Op, bool)` — Task 1 정의, Task 2 사용 ✅
- `align.Op{Tag byte; AStart, AEnd, BStart, BEnd int}` — Task 1 정의, Task 2 사용 ✅
- `tmpl.Unrepresented{Doc, Part, Path, Side string; Nodes int}` — Task 3 정의, Task 3·4 테스트가 필드명으로 참조 ✅
- `tmpl.Extract(pkgs, names, allowUnrepresented)` — Task 3 정의, Task 3·4 테스트와 Task 4 CLI 사용 ✅
- `diffMarkup(nodes []xmlscan.Node, names []string)` — Task 3 안에서만 정의·사용 ✅
