package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SONGYEONGSIN/pantograph/internal/opc"
	"github.com/SONGYEONGSIN/pantograph/internal/patch"
	"github.com/SONGYEONGSIN/pantograph/internal/tmpl"
)

func cmdTmpl(args []string) int {
	if len(args) == 0 {
		return die(exitInput, "사용법: panto tmpl extract|fill …")
	}
	switch args[0] {
	case "extract":
		return cmdTmplExtract(args[1:])
	case "fill":
		return cmdTmplFill(args[1:])
	default:
		return die(exitInput, "알 수 없는 하위 명령: %s", args[0])
	}
}

func cmdTmplExtract(args []string) int {
	var inputs []string
	var out, schemaPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			i++
			if i >= len(args) {
				return die(exitInput, "-o 뒤에 출력 경로가 필요하다")
			}
			out = args[i]
		case "--schema":
			i++
			if i >= len(args) {
				return die(exitInput, "--schema 뒤에 출력 경로가 필요하다")
			}
			schemaPath = args[i]
		default:
			inputs = append(inputs, args[i])
		}
	}
	if len(inputs) < 2 || out == "" || schemaPath == "" {
		return die(exitInput, "사용법: panto tmpl extract <a.docx> <b.docx> [...] -o <tmpl.docx> --schema <schema.json>")
	}

	pkgs := make([]*opc.Package, len(inputs))
	names := make([]string, len(inputs))
	for i, in := range inputs {
		p, code := openInput(in)
		if code != exitOK {
			return code
		}
		pkgs[i] = p
		names[i] = filepath.Base(in)
	}

	// Extract 는 입력 전부의 본문 파트를 파트 인식으로 순회한다(docx 는
	// word/document.xml 하나, pptx 는 슬라이드마다). 어느 문서가 문제인지는
	// 에러에서 되짚을 수 없으므로 입력 목록 전체를 경로로 단다 — Detail 이
	// 문제의 엔트리 이름을 갖고 있다.
	tp, sch, errs, err := tmpl.Extract(pkgs, names)
	if err != nil {
		return fail(strings.Join(inputs, ", "), err)
	}
	if len(errs) > 0 {
		if err := emit(patch.Result{OK: false, Errors: errs}); err != nil {
			return die(exitInternal, "%v", err)
		}
		return exitInput
	}

	// tp 는 inputs[0] 에서 만들어진다 — 재조립이 거절되면 그 문서가 원인이다.
	if err := writeAtomic(out, tp.Write); err != nil {
		return fail(inputs[0], err)
	}
	sb, err := json.MarshalIndent(sch, "", "  ")
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := writeAtomic(schemaPath, func(w io.Writer) error {
		_, err := w.Write(append(sb, '\n'))
		return err
	}); err != nil {
		return die(exitInternal, "%v", err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}

func cmdTmplFill(args []string) int {
	var in, dataPath, out, schemaPath string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-d":
			i++
			if i >= len(args) {
				return die(exitInput, "-d 뒤에 데이터 파일 경로가 필요하다")
			}
			dataPath = args[i]
		case "-o":
			i++
			if i >= len(args) {
				return die(exitInput, "-o 뒤에 출력 경로가 필요하다")
			}
			out = args[i]
		case "--schema":
			i++
			if i >= len(args) {
				return die(exitInput, "--schema 뒤에 스키마 경로가 필요하다")
			}
			schemaPath = args[i]
		default:
			if in != "" {
				return die(exitInput, "입력 파일이 둘 이상이다: %s, %s", in, args[i])
			}
			in = args[i]
		}
	}
	if in == "" || dataPath == "" || out == "" || schemaPath == "" {
		return die(exitInput, "사용법: panto tmpl fill <tmpl.docx> --schema <schema.json> -d <data.json> -o <out.docx>")
	}

	tp, code := openInput(in)
	if code != exitOK {
		return code
	}
	sb, err := os.ReadFile(schemaPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var sch tmpl.Schema
	if err := decodeStrict(sb, &sch); err != nil {
		return die(exitInput, "스키마 JSON 파싱 실패: %v", err)
	}
	// keys 가 비어있으면 Fill 이 만드는 ops 도 비어있어 patch.Apply 가
	// (nil, nil) 을 돌려준다 — missing_key/template_drift 검사를 하나도
	// 거치지 않고 원본 그대로의 사본을 "ok": true 로 내보내게 된다.
	// "keys" 필드가 없는 JSON(예: 다른 스키마, 잘못된 경로)도 여기 걸린다.
	if len(sch.Keys) == 0 {
		return die(exitInput, "스키마에 키가 없다: %s", schemaPath)
	}
	db, err := os.ReadFile(dataPath)
	if err != nil {
		return die(exitInternal, "%v", err)
	}
	var data map[string]string
	if err := decodeStrict(db, &data); err != nil {
		return die(exitInput, "데이터 JSON 파싱 실패: %v", err)
	}

	// Fill 은 템플릿의 본문 파트를 파트 인식으로 푼다(docx 는 word/document.xml
	// 하나, pptx 는 슬라이드마다) — 미지원 압축 방식이면 여기서 UnsupportedError
	// 가 난다.
	errs, err := tmpl.Fill(tp, &sch, data)
	if err != nil {
		return fail(in, err)
	}
	if len(errs) > 0 {
		if err := emit(patch.Result{OK: false, Errors: errs}); err != nil {
			return die(exitInternal, "%v", err)
		}
		return exitInput
	}

	if err := writeAtomic(out, tp.Write); err != nil {
		return fail(in, err)
	}
	if err := emit(patch.Result{OK: true}); err != nil {
		return die(exitInternal, "%v", err)
	}
	return exitOK
}
