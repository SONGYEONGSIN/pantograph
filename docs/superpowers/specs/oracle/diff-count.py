# diff-count.py — panto diff 의 항목 수를 검증하는 독립 오라클
#
# 이것이 무엇인가
#   Go 구현(internal/diff)과 **완전히 독립적으로** 짠 비교기다. 같은 XML 을
#   위치 정렬(index 대 index)로 훑어 text/attr/elem/structure 항목 수를 센다.
#   설계 문서(2026-08-09-diff-design.md) §10 "전체 항목 수(독립 측정)" 표의
#   기준값(form-a vs form-b = 7/1, deck-a vs deck-b = 13/12)이 이 스크립트가
#   낸 수다. §10 은 "구현이 이 수와 다르게 나오면 수를 고치지 말고 차이를
#   설명하라"고 지시하는데, 이 스크립트가 저장소에 없으면 그 지시를 아무도
#   실행할 수 없다 — 그래서 여기 둔다.
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
#      sig() 의 attrib 딕셔너리도 로컬명 기준이라 Go 의 수정 전 attrMap 과
#      같은 방식으로 충돌한다.
#
#      그런데도 §10 의 기준값(deck 13/12)은 Go 구현이 그 결함을 갖고 있을
#      때도, 고친 뒤에도 똑같이 나온다 — 픽스처(deck-a.pptx/deck-b.pptx)의
#      sldId·sldLayoutId 값이 두 파일에서 동일해서, 애초에 충돌한 속성에
#      차이가 없었기 때문이다(id 는 같은데 어느 쪽 id 를 봤는지만 바뀌면
#      값 비교 결과는 똑같이 "같다"). **두 구현이 같은 답을 냈다는 사실이
#      속성 비교 로직 자체를 검증하지는 않는다** — 그 결함을 잡은 것은
#      합성 노드로 직접 짠 단위 테스트(compare_internal_test.go)였지 이
#      오라클도, 실제 픽스처 대조도 아니었다.
#
#   2. .text/.tail 모델이 다르다. 파이썬 ElementTree 는 텍스트를 요소의
#      .text(첫 자식 앞)와 자식의 .tail(그 자식 뒤)로 나누어 담는다.
#      xmlscan.Node.Text 는 그 요소가 **직접** 품은 문자 데이터를 하나로
#      합친 값이다(자손 제외). 이 스크립트의 sig() 는 e.text 만 보고
#      tail 은 안 본다 — 혼합 콘텐츠(요소 사이사이에 텍스트가 섞인 마크업)
#      에서는 두 모델이 서로 다른 답을 낼 수 있다. 현재 픽스처의 본문
#      파트에는 그런 마크업이 없어 드러나지 않는다.
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
def sig(e):
    return (local(e.tag),(e.text or "").strip(),
            tuple(sorted((local(k),v) for k,v in e.attrib.items() if keep(e.tag,k))))
def walk(e):
    yield sig(e)
    for c in e: yield from walk(c)

def count(x, y):
    """위치 정렬로 비교 — text/attr/elem/structure 항목 수를 센다."""
    try: xs, ys = list(walk(ET.fromstring(x))), list(walk(ET.fromstring(y)))
    except ET.ParseError: return None
    c = {"text":0,"attr":0,"elem":0,"structure":0}
    for i in range(min(len(xs),len(ys))):
        (xt,xtx,xa),(yt,ytx,ya) = xs[i], ys[i]
        if xt != yt:
            c["elem"] += 1; continue
        if xtx != ytx: c["text"] += 1
        da = dict(xa); db = dict(ya)
        c["attr"] += sum(1 for k in set(da)|set(db) if da.get(k) != db.get(k))
    if len(xs) != len(ys): c["structure"] += 1
    return c

for a_,b_,plan in [("form-a.docx","form-b.docx",["word/document.xml"]),
                   ("deck-a.pptx","deck-b.pptx",["ppt/slides/slide1.xml","ppt/slides/slide2.xml","ppt/slides/slide3.xml"])]:
    A=zipfile.ZipFile("testdata/real/"+a_); B=zipfile.ZipFile("testdata/real/"+b_)
    tot={"text":0,"attr":0,"elem":0,"structure":0}; pc=0; vo=0
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
