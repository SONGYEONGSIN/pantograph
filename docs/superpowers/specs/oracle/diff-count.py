# diff-count.py — panto diff 의 항목 수를 검증하는 독립 오라클
#
# 이것이 무엇인가
#   Go 구현(internal/diff)과 **완전히 독립적으로** 짠 비교기다. 같은 XML 을
#   계층 LCS(형제 목록마다 서브트리 해시로 정렬, LCS 설계 §3)로 훑어
#   text/attr/elem/inserted/deleted/structure 항목 수를 센다. 설계 문서
#   (2026-08-10-lcs-align-design.md §7)의 기준값(form-a vs form-b = 7/1,
#   deck-a vs deck-b = 13/12)이 이 스크립트가 낸 수다. 그 문서는 "구현이
#   이 수와 다르게 나오면 수를 고치지 말고 차이를 설명하라"고 지시하는데,
#   이 스크립트가 저장소에 없으면 그 지시를 아무도 실행할 수 없다 — 그래서
#   여기 둔다.
#
#   이전 버전(2026-08-09-diff-design.md §10 시절)은 위치 정렬(index 대
#   index)로 비교했다. LCS 정렬이 Go 쪽에 들어오면서 옛 알고리즘을 계속
#   재면 "구현이 다른 수를 낸다"는 거짓 경보가 났을 것이다 — 그래서 이
#   오라클도 같은 알고리즘으로 바꿨다. 두 픽스처 쌍의 total/volatile_only
#   는 안 바뀐다(구조 차이가 없었거나, deck 의 structure 1건이 종류만
#   deleted 1건으로 바뀐다) — LCS 설계 §7 의 예측대로다.
#
# 빌드의 일부가 아니다
#   go.mod 의 어떤 타깃도 이 파일을 참조하지 않는다. 저장소의 "외부 의존
#   없음" 규칙(go.mod 에 require 블록을 만들지 않는다)은 Go 빌드 산출물에
#   대한 규칙이라 이 파일과 무관하다 — 이것은 검증용 산출물이지 배포되는
#   바이너리의 일부가 아니다.
#
# 실행법
#   저장소 루트에서:
#     python3 docs/superpowers/specs/oracle/diff-count.py
#   testdata/real/{form,deck}-{a,b}.{docx,pptx} 를 상대 경로로 읽으므로 다른
#   위치에서 실행하면 파일을 못 찾는다.
#
# 한계 (중요 — 이 오라클이 못 잡는 것)
#   1. 네임스페이스 처리가 Go 쪽과 다르다. 파이썬 xml.etree 는 네임스페이스
#      선언과 속성의 네임스페이스를 xmlscan 과 다르게 다룬다 — 이 스크립트는
#      ElementTree 가 이미 URI 로 확장해 준 태그·속성 이름에서 로컬명만
#      떼어 쓴다(local() 함수, "}" 뒤쪽). 즉 속성의 "어느 네임스페이스인가"
#      정보를 Go 쪽처럼 (NS, Name) 짝으로 보존하지 않고 버린다.
#
#      이 한계가 실제로 문제를 하나 숨겼다: 최종 리뷰가 찾은 Critical
#      결함(internal/diff/compare.go 의 attrMap 이 로컬명만으로 색인해
#      <p:sldId id="256" r:id="rId2"/> 같은 마크업에서 네임스페이스 없는
#      id 와 r:id 의 id 가 충돌 — 뒤엣것이 앞을 덮어써 슬라이드 정체성이
#      비교에서 통째로 빠지는 문제)를 **이 오라클도 똑같이 못 잡는다**.
#      compare_pair() 의 attrib 딕셔너리(xa/ya)도 로컬명 기준이라 Go 의
#      수정 전 attrMap 과 같은 방식으로 충돌한다. subtree_sig() 의 안정
#      속성 튜플도 마찬가지다 — 서브트리 해시조차 그 네임스페이스 구분을
#      못 한다.
#
#      그런데도 §7(옛 §10)의 기준값(deck 13/12)은 Go 구현이 그 결함을 갖고
#      있을 때도, 고친 뒤에도 똑같이 나온다 — 픽스처(deck-a.pptx/
#      deck-b.pptx)의 sldId·sldLayoutId 값이 두 파일에서 동일해서, 애초에
#      충돌한 속성에 차이가 없었기 때문이다(id 는 같은데 어느 쪽 id 를
#      봤는지만 바뀌면 값 비교 결과는 똑같이 "같다"). **두 구현이 같은
#      답을 냈다는 사실이 속성 비교 로직 자체를 검증하지는 않는다** — 그
#      결함을 잡은 것은 합성 노드로 직접 짠 단위 테스트
#      (compare_internal_test.go)였지 이 오라클도, 실제 픽스처 대조도
#      아니었다.
#
#   2. .text/.tail 모델이 다르다. 파이썬 ElementTree 는 텍스트를 요소의
#      .text(첫 자식 앞)와 자식의 .tail(그 자식 뒤)로 나누어 담는다.
#      xmlscan.Node.Text 는 그 요소가 **직접** 품은 문자 데이터를 하나로
#      합친 값이다(자손 제외). compare_pair() 와 subtree_sig() 는 둘 다
#      e.text 만 보고 tail 은 안 본다 — 혼합 콘텐츠(요소 사이사이에 텍스트가
#      섞인 마크업)에서는 두 모델이 서로 다른 답을 낼 수 있다. 현재 픽스처의
#      본문 파트에는 그런 마크업이 없어 드러나지 않는다.
#
#   3. difflib.SequenceMatcher 는 고전적 LCS 가 아니다. Ratcliff-Obershelp
#      패턴 매칭이라(autojunk=False 로 둬도 그렇다) "최장 공통 부분열"을
#      찾는다는 목적은 같지만, 동점(같은 길이의 매칭이 여럿 있을 때 어느
#      것을 고르는가)을 고전적 LCS DP 와 다르게 처리할 수 있다. Go 구현
#      (internal/diff/align.go 의 alignMiddle)은 진짜 LCS DP 를 쓰고,
#      동점이면 항상 a 를 먼저 버리는 규칙으로 고정한다(결정론 — I3).
#      이 오라클은 그 규칙을 재현하지 않는다 — LCS 설계 §7 이 예고한
#      대로다: "프로토타입이 검증한 것은 접근법(형제 단위·서브트리 해시·
#      replace 재귀)이지 특정 알고리즘의 출력이 아니다." 실측으로는 두
#      픽스처 쌍 모두 Go 값과 일치했다(아래 실행 결과) — 동점이 갈릴
#      만큼 애매한 형제 목록이 이 픽스처에는 없었다는 뜻이다. 앞으로
#      새 픽스처가 이 값을 흔들면, 그것이 실제 결함인지 이 동점 처리
#      차이인지부터 가른다(수를 억지로 맞추지 않는다).
#
#   4. 증폭 방어(LCS 설계 §5, maxCells 상한)를 구현하지 않는다. 이
#      오라클은 형제 수 상한을 두지 않는다 — 픽스처의 실측 최대 형제 수가
#      맞물려 있기 때문이다(§5 근거: 400 대 초반이 최대). 그래서
#      `structure` 항목은 이 오라클에서 **항상 0** 이다. Go 쪽 `structure`
#      의 나머지 트리거(한쪽 파트가 노드 0개)도 이 오라클에서는 못
#      일어난다 — `xml.etree.ElementTree.fromstring` 은 잘 짜인 XML 이면
#      항상 정확히 하나의 루트 요소를 돌려주므로 "노드 0개 트리"라는
#      상태 자체가 없다(완전히 빈 문자열이면 ParseError 로 빠져
#      part_content 로 내려간다 — 그건 별개 경로다). 즉 이 오라클로
#      `structure` 를 검증할 수는 없다 — 그 경로는 합성 트리 단위
#      테스트(align_test.go/compare_internal_test.go)의 몫이다.
#
#   5. 텍스트 공백 처리는 설계 문서가 아니라 Go 구현을 참고했다. compare_pair()
#      와 subtree_sig() 는 직접 텍스트를 `.strip()` 없이 raw 로 비교한다(옛
#      오라클의 sig() 는 `.strip()` 을 썼었다). 이 결정은 LCS 설계 문서(§3·
#      §6 어디에도 텍스트 공백 처리를 정하는 문장이 없다)를 따른 게 아니라
#      `internal/diff/compare.go:199` 의 주석("공백을 다듬지 않는다:
#      xml:space="preserve" 인 w:t 의 끝 공백은 내용이다")을 보고 맞춘 것이다
#      — **설계가 침묵한 영역에서 Go 의 동작을 참고했다는 뜻이라, 이 지점은
#      독립 검증이 아니다.** 두 픽스처에서는 strip 유무가 결과를 바꾸지
#      않음을 확인했다(전 필드 동일) — 그래서 값은 안전하지만, "독립적으로
#      짠 오라클"이라는 주장이 이 한 조각에서는 얇아진다는 사실은 숨기지
#      않는다.
import difflib
import zipfile
import xml.etree.ElementTree as ET
def local(t): return t.rsplit('}',1)[-1]
VOL_ELEMS={"creationId"}; VOL_ATTRS={"paraId","textId"}
PAIRS={("fld","id"),("rsid","val"),("rsidRoot","val")}
def keep(tag,attr):
    t,a=local(tag),local(attr)
    if t in VOL_ELEMS: return False
    if a in VOL_ATTRS or a.startswith("rsid"): return False
    return (t,a) not in PAIRS

def subtree_sig(e):
    """서브트리 해시 — 타입 + 직접 텍스트 + 안정 속성 + 자식 해시(재귀).
    Go 의 sign()(align.go)과 대응한다. SHA256 대신 파이썬 튜플의 구조적
    동등성을 그대로 쓴다 — 목적(두 서브트리가 통째로 같은지 판정)은 같다."""
    attrs = tuple(sorted((local(k), v) for k, v in e.attrib.items() if keep(e.tag, k)))
    return (local(e.tag), e.text or "", attrs, tuple(subtree_sig(c) for c in e))

def compare_pair(a, b, c):
    """짝지어진 노드 한 쌍을 비교한다 (LCS 설계 §3 compare). 타입이 다르면
    elem 1건을 내고 그 안은 비교하지 않는다 — Go 의 comparePair 와 같다."""
    if local(a.tag) != local(b.tag):
        c["elem"] += 1
        return
    if (a.text or "") != (b.text or ""):
        c["text"] += 1
    xa = {local(k): v for k, v in a.attrib.items() if keep(a.tag, k)}
    ya = {local(k): v for k, v in b.attrib.items() if keep(b.tag, k)}
    for k in set(xa) | set(ya):
        if xa.get(k) != ya.get(k):
            c["attr"] += 1
    align(list(a), list(b), c)

def align(a_kids, b_kids, c):
    """형제 목록을 정렬한다 (LCS 설계 §3 align). equal 구간은 내려가지
    않는다. replace 구간은 위치로 짝지어 compare_pair 로 재귀하고, 짝
    없는 꼬리는 insert/delete 로 센다(둘 다 서브트리당 1건 — §4)."""
    sm = difflib.SequenceMatcher(None,
                                  [subtree_sig(k) for k in a_kids],
                                  [subtree_sig(k) for k in b_kids],
                                  autojunk=False)
    for tag, i1, i2, j1, j2 in sm.get_opcodes():
        if tag == "equal":
            continue
        elif tag == "delete":
            c["deleted"] += i2 - i1
        elif tag == "insert":
            c["inserted"] += j2 - j1
        elif tag == "replace":
            m = min(i2 - i1, j2 - j1)
            for k in range(m):
                compare_pair(a_kids[i1 + k], b_kids[j1 + k], c)
            c["deleted"] += (i2 - i1) - m
            c["inserted"] += (j2 - j1) - m

def count(x, y):
    """계층 LCS 로 비교해 text/attr/elem/inserted/deleted/structure 항목
    수를 센다. structure 는 이 오라클에서 항상 0 이다 (헤더 한계 4 참고)."""
    try: rx, ry = ET.fromstring(x), ET.fromstring(y)
    except ET.ParseError: return None
    c = {"text":0,"attr":0,"elem":0,"structure":0,"inserted":0,"deleted":0}
    compare_pair(rx, ry, c)
    return c

for a_,b_,plan in [("form-a.docx","form-b.docx",["word/document.xml"]),
                   ("deck-a.pptx","deck-b.pptx",["ppt/slides/slide1.xml","ppt/slides/slide2.xml","ppt/slides/slide3.xml"])]:
    A=zipfile.ZipFile("testdata/real/"+a_); B=zipfile.ZipFile("testdata/real/"+b_)
    tot={"text":0,"attr":0,"elem":0,"structure":0,"inserted":0,"deleted":0}; pc=0; vo=0
    print("%s vs %s" % (a_,b_))
    for n in plan:
        c=count(A.read(n),B.read(n))
        for k in tot: tot[k]+=c[k]
        if any(c.values()): print("   [body] %-28s %s" % (n,{k:v for k,v in c.items() if v}))
    for n in sorted(set(A.namelist())&set(B.namelist())):
        if n in plan: continue
        x,y=A.read(n),B.read(n)
        if x==y: continue
        c=count(x,y)
        if c is None: pc+=1; print("   [other] %-28s 스캔 불가 → part_content" % n); continue
        if not any(c.values()): vo+=1; continue
        for k in tot: tot[k]+=c[k]
        print("   [other] %-28s %s" % (n,{k:v for k,v in c.items() if v}))
    print("   summary: %s part_content=%d total=%d volatile_only=%d"
          % (tot, pc, sum(tot.values())+pc, vo))
